//go:build linux
// +build linux

package dns

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/observers/dns/bpf"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// DNS event from BPF - must match C struct exactly
type dnsEventCore struct {
	Timestamp  uint64
	PID        uint32
	TID        uint32
	UID        uint32
	GID        uint32
	CgroupID   uint64
	EventType  uint8
	Protocol   uint8
	IPVersion  uint8
	Rcode      uint8
	SrcAddr    uint32
	DstAddr    uint32
	SrcPort    uint16
	DstPort    uint16
	DNSID      uint16
	QueryType  uint16
	LatencyNs  uint32
	Comm       [16]byte
	QueryName  [256]byte
	PacketSize uint32
	Answers    uint8
	IsError    uint8
	Pad        [2]uint8
}

// Overflow stats from BPF
type overflowStats struct {
	RingbufDrops   uint64
	RateLimitDrops uint64
	SamplingDrops  uint64
}

// CO-RE eBPF implementation
type coreEBPF struct {
	collection *ebpf.Collection
	links      []link.Link
	reader     *ringbuf.Reader

	// Metrics
	eventsProcessed  metric.Int64Counter
	eventsDropped    metric.Int64Counter
	processingTime   metric.Float64Histogram
	latencyHistogram metric.Float64Histogram

	logger *zap.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Load CO-RE eBPF programs
func (c *Collector) loadCoreEBPF() error {
	c.logger.Info("Loading CO-RE eBPF programs for DNS observer")

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("removing memlock: %w", err)
	}

	// Load eBPF spec
	spec, err := bpf.LoadDnsmonitor_core()
	if err != nil {
		return fmt.Errorf("loading BPF spec: %w", err)
	}

	// Verify BTF is available
	if spec.Types == nil {
		return fmt.Errorf("BTF information not available - CO-RE requires BTF-enabled kernel")
	}

	// Load collection with CO-RE options
	opts := &ebpf.CollectionOptions{
		Programs: ebpf.ProgramOptions{
			LogLevel: ebpf.LogLevelInfo,
			LogSize:  64 * 1024 * 1024, // 64MB for verifier logs
		},
	}

	coll, err := ebpf.NewCollectionWithOptions(spec, opts)
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			// Log verifier error details
			c.logger.Error("BPF verifier error",
				zap.String("error", ve.Error()),
				zap.String("log", ve.Log))
			return fmt.Errorf("BPF verifier rejected program: %w", err)
		}
		return fmt.Errorf("loading BPF collection: %w", err)
	}

	c.ebpfState = &coreEBPF{
		collection:      coll,
		links:           make([]link.Link, 0),
		eventsProcessed: c.eventsProcessed,
		eventsDropped:   c.eventsDropped,
		processingTime:  c.processingTime,
		logger:          c.logger,
	}

	// Attach kprobes
	if err := c.attachCoreProbes(); err != nil {
		coll.Close()
		return fmt.Errorf("attaching probes: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(coll.Maps["dns_events"])
	if err != nil {
		c.closeCoreEBPF()
		return fmt.Errorf("creating ringbuf reader: %w", err)
	}

	ebpfState := c.ebpfState.(*coreEBPF)
	ebpfState.reader = reader

	// Start event processor
	ctx, cancel := context.WithCancel(context.Background())
	ebpfState.cancel = cancel

	ebpfState.wg.Add(1)
	go c.processCoreEvents(ctx)

	// Start metrics collector
	ebpfState.wg.Add(1)
	go c.collectCoreMetrics(ctx)

	c.logger.Info("CO-RE eBPF programs loaded successfully")
	return nil
}

// Attach CO-RE kprobes
func (c *Collector) attachCoreProbes() error {
	ebpfState := c.ebpfState.(*coreEBPF)

	// Attach UDP send probe
	prog := ebpfState.collection.Programs["trace_udp_sendmsg"]
	if prog == nil {
		return fmt.Errorf("trace_udp_sendmsg program not found")
	}

	l, err := link.Kprobe("udp_sendmsg", prog, nil)
	if err != nil {
		return fmt.Errorf("attaching udp_sendmsg kprobe: %w", err)
	}
	ebpfState.links = append(ebpfState.links, l)

	// Attach UDP receive probe
	prog = ebpfState.collection.Programs["trace_udp_recvmsg"]
	if prog == nil {
		return fmt.Errorf("trace_udp_recvmsg program not found")
	}

	l, err = link.Kprobe("udp_recvmsg", prog, nil)
	if err != nil {
		return fmt.Errorf("attaching udp_recvmsg kprobe: %w", err)
	}
	ebpfState.links = append(ebpfState.links, l)

	c.logger.Debug("Attached CO-RE kprobes",
		zap.Int("count", len(ebpfState.links)))

	return nil
}

// Process events from ring buffer
func (c *Collector) processCoreEvents(ctx context.Context) {
	defer c.ebpfState.(*coreEBPF).wg.Done()

	ebpfState := c.ebpfState.(*coreEBPF)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		record, err := ebpfState.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			c.logger.Warn("Error reading from ringbuf",
				zap.Error(err))
			continue
		}

		// Parse event
		if len(record.RawSample) < int(unsafe.Sizeof(dnsEventCore{})) {
			c.logger.Warn("Invalid event size",
				zap.Int("size", len(record.RawSample)))
			continue
		}

		event := (*dnsEventCore)(unsafe.Pointer(&record.RawSample[0]))

		// Convert to domain event
		domainEvent := c.convertCoreToDomainEvent(event)

		// Send to channel
		select {
		case c.EventChannelManager.GetChannel() <- domainEvent:
			c.RecordEvent()
			if c.eventsProcessed != nil {
				c.eventsProcessed.Add(ctx, 1,
					metric.WithAttributes(
						attribute.String("type", "dns"),
						attribute.String("event", getEventTypeName(event.EventType))))
			}
		default:
			c.RecordDrop()
			if c.eventsDropped != nil {
				c.eventsDropped.Add(ctx, 1,
					metric.WithAttributes(
						attribute.String("reason", "channel_full")))
			}
		}
	}
}

// Collect metrics from BPF maps
func (c *Collector) collectCoreMetrics(ctx context.Context) {
	defer c.ebpfState.(*coreEBPF).wg.Done()

	ebpfState := c.ebpfState.(*coreEBPF)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.readCoreOverflowStats()
		}
	}
}

// Read overflow statistics from BPF
func (c *Collector) readCoreOverflowStats() {
	ebpfState := c.ebpfState.(*coreEBPF)

	var stats overflowStats
	key := uint32(0)

	// Read overflow stats
	if err := ebpfState.collection.Maps["dns_overflow"].Lookup(key, &stats); err == nil {
		if c.eventsDropped != nil {
			ctx := context.Background()

			c.eventsDropped.Add(ctx, int64(stats.RingbufDrops),
				metric.WithAttributes(attribute.String("reason", "ringbuf_full")))

			c.eventsDropped.Add(ctx, int64(stats.RateLimitDrops),
				metric.WithAttributes(attribute.String("reason", "rate_limit")))

			c.eventsDropped.Add(ctx, int64(stats.SamplingDrops),
				metric.WithAttributes(attribute.String("reason", "sampling")))
		}

		// Reset counters after reading
		stats = overflowStats{}
		ebpfState.collection.Maps["dns_overflow"].Update(key, &stats, ebpf.UpdateAny)
	}
}

// Convert BPF event to domain event
func (c *Collector) convertCoreToDomainEvent(event *dnsEventCore) *domain.CollectorEvent {
	// Convert timestamp
	timestamp := time.Unix(0, int64(event.Timestamp))

	// Convert comm to string
	comm := string(event.Comm[:])
	for i, b := range event.Comm {
		if b == 0 {
			comm = string(event.Comm[:i])
			break
		}
	}

	// Convert query name
	queryName := string(event.QueryName[:])
	for i, b := range event.QueryName {
		if b == 0 {
			queryName = string(event.QueryName[:i])
			break
		}
	}

	// Format IPs
	srcIP := formatIPv4(event.SrcAddr)
	dstIP := formatIPv4(event.DstAddr)

	// Create domain event
	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("dns-%d-%d", event.PID, event.Timestamp),
		Timestamp: timestamp,
		Type:      domain.EventTypeNetwork,
		Source:    "dns-observer",
		Severity:  getDNSSeverity(event),
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				Protocol:  getProtocolName(event.Protocol),
				SrcIP:     srcIP,
				DstIP:     dstIP,
				SrcPort:   int(event.SrcPort),
				DstPort:   int(event.DstPort),
				Direction: getDirection(event.EventType),
				Bytes:     int64(event.PacketSize),
			},
			Process: &domain.ProcessData{
				PID:      int32(event.PID),
				TID:      int32(event.TID),
				UID:      int32(event.UID),
				GID:      int32(event.GID),
				Command:  comm,
				CgroupID: event.CgroupID,
			},
			Custom: map[string]string{
				"dns_query":  queryName,
				"dns_id":     fmt.Sprintf("%d", event.DNSID),
				"query_type": getQueryTypeName(event.QueryType),
				"rcode":      getRcodeName(event.Rcode),
				"answers":    fmt.Sprintf("%d", event.Answers),
				"latency_ms": fmt.Sprintf("%.2f", float64(event.LatencyNs)/1000000.0),
				"event_type": getEventTypeName(event.EventType),
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "dns",
				"core":     "true",
				"version":  "1.0",
			},
		},
	}
}

// Helper functions
func formatIPv4(addr uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(addr), byte(addr>>8), byte(addr>>16), byte(addr>>24))
}

func getEventTypeName(eventType uint8) string {
	switch eventType {
	case 1:
		return "query"
	case 2:
		return "response"
	case 3:
		return "timeout"
	case 4:
		return "error"
	default:
		return "unknown"
	}
}

func getProtocolName(proto uint8) string {
	switch proto {
	case 6:
		return "tcp"
	case 17:
		return "udp"
	default:
		return fmt.Sprintf("proto_%d", proto)
	}
}

func getDirection(eventType uint8) string {
	if eventType == 1 { // Query
		return "outbound"
	}
	return "inbound"
}

func getDNSSeverity(event *dnsEventCore) domain.EventSeverity {
	if event.IsError > 0 || event.Rcode != 0 {
		return domain.EventSeverityWarning
	}
	if event.LatencyNs > 1000000000 { // > 1 second
		return domain.EventSeverityWarning
	}
	return domain.EventSeverityInfo
}

func getQueryTypeName(qtype uint16) string {
	switch qtype {
	case 1:
		return "A"
	case 28:
		return "AAAA"
	case 5:
		return "CNAME"
	case 15:
		return "MX"
	case 16:
		return "TXT"
	case 33:
		return "SRV"
	default:
		return fmt.Sprintf("TYPE%d", qtype)
	}
}

func getRcodeName(rcode uint8) string {
	switch rcode {
	case 0:
		return "NOERROR"
	case 2:
		return "SERVFAIL"
	case 3:
		return "NXDOMAIN"
	case 5:
		return "REFUSED"
	default:
		return fmt.Sprintf("RCODE%d", rcode)
	}
}

// Close CO-RE eBPF
func (c *Collector) closeCoreEBPF() {
	if c.ebpfState == nil {
		return
	}

	ebpfState := c.ebpfState.(*coreEBPF)

	// Cancel context
	if ebpfState.cancel != nil {
		ebpfState.cancel()
	}

	// Close reader
	if ebpfState.reader != nil {
		ebpfState.reader.Close()
	}

	// Wait for goroutines
	ebpfState.wg.Wait()

	// Detach probes
	for _, l := range ebpfState.links {
		l.Close()
	}

	// Close collection
	if ebpfState.collection != nil {
		ebpfState.collection.Close()
	}

	c.logger.Info("CO-RE eBPF programs closed")
}
