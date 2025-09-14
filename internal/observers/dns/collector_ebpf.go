//go:build linux
// +build linux

package dns

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/internal/observers/dns/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// eBPF generation is handled by bpf/generate.go

// BPFDNSEvent matches the C struct dns_event exactly for proper memory alignment
type BPFDNSEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	UID       uint32
	GID       uint32
	CgroupID  uint64

	EventType uint8
	Protocol  uint8
	IPVersion uint8
	Pad1      uint8

	// Source address (IPv4 or IPv6)
	SrcAddr [16]byte // Union of IPv4 (4 bytes) and IPv6 (16 bytes)

	// Destination address (IPv4 or IPv6)
	DstAddr [16]byte // Union of IPv4 (4 bytes) and IPv6 (16 bytes)

	SrcPort uint16
	DstPort uint16

	// DNS info
	DNSID    uint16
	DNSFlags uint16
	Opcode   uint8
	Rcode    uint8
	QType    uint16

	DataLen   uint32
	LatencyNs uint32

	QueryName [128]byte // MAX_DNS_NAME_LEN from C
	Data      [512]byte // MAX_DNS_DATA from C
}

// eBPF components for Linux
type ebpfState struct {
	objs   *bpf.DnsmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes and starts eBPF monitoring on Linux
func (c *Observer) startEBPF() error {
	ctx := c.LifecycleManager.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Check for mock mode
	if os.Getenv("TAPIO_MOCK_MODE") == "true" {
		c.mockMode = true
		c.logger.Info("DNS observer running in MOCK MODE")
		go c.generateMockEvents()
		return nil
	}

	c.logger.Debug("Starting eBPF DNS monitoring")

	// Remove memory lock limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.logger.Warn("Failed to remove memlock limit", zap.Error(err))
	}

	// Load pre-compiled eBPF objects
	objs := &bpf.DnsmonitorObjects{}
	if err := bpf.LoadDnsmonitorObjects(objs, nil); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_load_failed")))
		}
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.DnsEvents)
	if err != nil {
		objs.Close()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}

	// Attach tracepoint programs
	var attachedLinks []link.Link

	// Attach DNS tracepoints for recvfrom (DNS responses)
	recvfromLink, err := link.Tracepoint("syscalls", "sys_exit_recvfrom", objs.TraceDnsRecvfrom, nil)
	if err != nil {
		reader.Close()
		objs.Close()
		return fmt.Errorf("failed to attach recvfrom tracepoint: %w", err)
	}
	attachedLinks = append(attachedLinks, recvfromLink)

	// Attach DNS tracepoints for sendto (DNS queries)
	sendtoLink, err := link.Tracepoint("syscalls", "sys_exit_sendto", objs.TraceDnsSendto, nil)
	if err != nil {
		for _, l := range attachedLinks {
			l.Close()
		}
		reader.Close()
		objs.Close()
		return fmt.Errorf("failed to attach sendto tracepoint: %w", err)
	}
	attachedLinks = append(attachedLinks, sendtoLink)

	// Store eBPF state as interface{}
	c.ebpfState = &ebpfState{
		objs:   objs,
		links:  attachedLinks,
		reader: reader,
	}

	c.logger.Info("DNS eBPF monitoring started successfully",
		zap.Int("tracepoints_attached", len(attachedLinks)))

	return nil
}

// stopEBPF cleans up eBPF resources on Linux
func (c *Observer) stopEBPF() {
	if c.ebpfState == nil {
		return
	}

	state, ok := c.ebpfState.(*ebpfState)
	if !ok {
		c.logger.Error("Invalid eBPF state type")
		return
	}

	c.logger.Debug("Stopping eBPF DNS monitoring")

	// Close reader
	if state.reader != nil {
		if err := state.reader.Close(); err != nil {
			c.logger.Warn("Failed to close ring buffer reader", zap.Error(err))
		}
	}

	// Detach links
	for _, l := range state.links {
		if err := l.Close(); err != nil {
			c.logger.Warn("Failed to close eBPF link", zap.Error(err))
		}
	}

	// Close eBPF objects
	if err := state.objs.Close(); err != nil {
		c.logger.Warn("Failed to close eBPF objects", zap.Error(err))
	}

	c.ebpfState = nil
	c.logger.Info("DNS eBPF monitoring stopped")
}

// readEBPFEvents reads and processes events from eBPF on Linux
func (c *Observer) readEBPFEvents() {
	if c.mockMode {
		c.generateMockEvents()
		return
	}

	state, ok := c.ebpfState.(*ebpfState)
	if !ok {
		c.logger.Error("Invalid eBPF state type for event processing")
		return
	}

	c.logger.Debug("Starting eBPF event processing loop")

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			c.logger.Debug("DNS event processing stopped")
			return
		default:
			// Read event from ring buffer
			record, err := state.reader.Read()
			if err != nil {
				if err == ringbuf.ErrClosed {
					return
				}
				c.logger.Warn("Failed to read from ring buffer", zap.Error(err))
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.LifecycleManager.Context(), 1,
						metric.WithAttributes(attribute.String("error", "ringbuf_read")))
				}
				continue
			}

			// Process the DNS event
			if err := c.processBPFEvent(c.LifecycleManager.Context(), record.RawSample); err != nil {
				c.logger.Warn("Failed to process DNS event", zap.Error(err))
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.LifecycleManager.Context(), 1,
						metric.WithAttributes(attribute.String("error", "event_processing")))
				}
			}
		}
	}
}

// processBPFEvent processes a single DNS event from eBPF
func (c *Observer) processBPFEvent(ctx context.Context, data []byte) error {
	if len(data) < int(unsafe.Sizeof(BPFDNSEvent{})) {
		return fmt.Errorf("DNS event data too small: got %d bytes", len(data))
	}

	// Parse the BPF event
	var bpfEvent BPFDNSEvent
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, binary.LittleEndian, &bpfEvent); err != nil {
		return fmt.Errorf("failed to parse BPF DNS event: %w", err)
	}

	// Convert to ObserverEvent
	observerEvent, err := c.convertBPFEventToObserverEvent(&bpfEvent)
	if err != nil {
		return fmt.Errorf("failed to convert BPF event: %w", err)
	}

	// Send event
	if c.EventChannelManager.SendEvent(observerEvent) {
		c.BaseObserver.RecordEvent()
	} else {
		c.BaseObserver.RecordDrop()
	}
	c.logger.Debug("Processed DNS event",
		zap.String("query", observerEvent.EventData.DNS.QueryName),
		zap.String("type", observerEvent.EventData.DNS.QueryType))

	return nil
}

// convertBPFEventToObserverEvent converts a BPF DNS event to a ObserverEvent
func (c *Observer) convertBPFEventToObserverEvent(bpfEvent *BPFDNSEvent) (*domain.CollectorEvent, error) {
	// Extract query name
	queryName := strings.TrimRight(string(bpfEvent.QueryName[:]), "\x00")
	if queryName == "" {
		queryName = "unknown"
	}

	// Parse addresses
	var srcIP, dstIP string
	if bpfEvent.IPVersion == 4 {
		srcIP = net.IP(bpfEvent.SrcAddr[:4]).String()
		dstIP = net.IP(bpfEvent.DstAddr[:4]).String()
	} else {
		srcIP = net.IP(bpfEvent.SrcAddr[:16]).String()
		dstIP = net.IP(bpfEvent.DstAddr[:16]).String()
	}

	// Determine query type
	queryType := "UNKNOWN"
	switch bpfEvent.QType {
	case 1:
		queryType = "A"
	case 28:
		queryType = "AAAA"
	case 5:
		queryType = "CNAME"
	case 15:
		queryType = "MX"
	case 2:
		queryType = "NS"
	case 16:
		queryType = "TXT"
	}

	// Determine protocol
	protocol := "UDP"
	if bpfEvent.Protocol == 6 { // IPPROTO_TCP
		protocol = "TCP"
	}

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("dns-%d-%d", bpfEvent.PID, bpfEvent.Timestamp),
		Type:      domain.EventTypeDNS,
		Timestamp: time.Unix(0, int64(bpfEvent.Timestamp)),
		Source:    c.config.Name,
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			DNS: &domain.DNSData{
				QueryName:    queryName,
				QueryType:    queryType,
				ResponseCode: int(bpfEvent.Rcode),
				Duration:     time.Duration(bpfEvent.LatencyNs),
				ClientIP:     srcIP,
				ServerIP:     dstIP,
				ClientPort:   bpfEvent.SrcPort,
				ServerPort:   bpfEvent.DstPort,
				Error:        bpfEvent.Rcode != 0,
			},
			Process: &domain.ProcessData{
				PID: int32(bpfEvent.PID),
				TID: int32(bpfEvent.TID),
				UID: int32(bpfEvent.UID),
				GID: int32(bpfEvent.GID),
			},
		},
		Metadata: domain.EventMetadata{
			Tags: []string{
				fmt.Sprintf("protocol:%s", protocol),
				fmt.Sprintf("qtype:%s", queryType),
				fmt.Sprintf("rcode:%d", bpfEvent.Rcode),
			},
			Labels: map[string]string{
				"observer":      c.config.Name,
				"query_name":    queryName,
				"query_type":    queryType,
				"protocol":      protocol,
				"client_ip":     srcIP,
				"server_ip":     dstIP,
				"response_code": fmt.Sprintf("%d", bpfEvent.Rcode),
			},
		},
	}, nil
}

// generateMockEvents generates mock DNS events for testing on Linux
func (c *Observer) generateMockEvents() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	mockQueries := []string{
		"google.com", "github.com", "stackoverflow.com",
		"kubernetes.io", "golang.org", "docker.com",
	}
	queryIndex := 0

	c.logger.Info("Starting mock DNS event generation")

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			now := time.Now()
			queryName := mockQueries[queryIndex%len(mockQueries)]
			queryIndex++

			mockEvent := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("dns-mock-%d", now.UnixNano()),
				Type:      domain.EventTypeDNS,
				Timestamp: now,
				Source:    c.config.Name,
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					DNS: &domain.DNSData{
						QueryName:    queryName,
						QueryType:    "A",
						ResponseCode: 0,
						Duration:     time.Duration(20+queryIndex%80) * time.Millisecond,
						ClientIP:     "127.0.0.1",
						ServerIP:     "8.8.8.8",
						ClientPort:   uint16(32000 + queryIndex%1000),
						ServerPort:   53,
						Error:        false,
					},
					Process: &domain.ProcessData{
						PID: int32(1000 + queryIndex%1000),
						TID: int32(1000 + queryIndex%1000),
						UID: 1000,
						GID: 1000,
					},
				},
				Metadata: domain.EventMetadata{
					Tags: []string{
						"protocol:UDP", "qtype:A", "rcode:0", "mock:true",
					},
					Labels: map[string]string{
						"observer":      c.config.Name,
						"query_name":    queryName,
						"query_type":    "A",
						"protocol":      "UDP",
						"mock_event":    "true",
						"response_code": "0",
					},
				},
			}

			// Send mock event
			if c.EventChannelManager.SendEvent(mockEvent) {
				c.BaseObserver.RecordEvent()
			} else {
				c.BaseObserver.RecordDrop()
			}
			c.logger.Debug("Mock DNS event generated", zap.String("query", queryName))
		}
	}
}
