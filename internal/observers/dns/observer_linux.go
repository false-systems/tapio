//go:build linux
// +build linux

package dns

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// loadDNSSpec loads the generated eBPF specification
func loadDNSSpec() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("eBPF DNS monitoring not yet implemented")
}

// dnsEBPF contains eBPF-specific state
type dnsEBPF struct {
	collection *ebpf.Collection
	links      []link.Link
	reader     *ringbuf.Reader

	logger *zap.Logger
	cancel context.CancelFunc
}

// startPlatform starts eBPF-based DNS problem detection
func (o *Observer) startPlatform() error {
	if !o.config.EnableEBPF {
		o.logger.Info("eBPF disabled, using fallback mode")
		return o.startFallback()
	}

	o.logger.Info("Starting eBPF-based DNS problem detection")

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("removing memlock: %w", err)
	}

	// Load eBPF programs (will be generated from C code)
	spec, err := loadDNSSpec()
	if err != nil {
		return fmt.Errorf("loading BPF spec: %w", err)
	}

	// Load collection
	opts := &ebpf.CollectionOptions{
		Programs: ebpf.ProgramOptions{
			LogLevel: ebpf.LogLevelInfo,
			LogSize:  64 * 1024 * 1024,
		},
	}

	coll, err := ebpf.NewCollectionWithOptions(spec, opts)
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			o.logger.Error("BPF verifier error",
				zap.String("error", ve.Error()),
				zap.String("log", ve.Log))
		}
		return fmt.Errorf("loading BPF collection: %w", err)
	}

	o.ebpfState = &dnsEBPF{
		collection: coll,
		links:      make([]link.Link, 0),
		logger:     o.logger,
	}

	// Configure thresholds in BPF maps
	if err := o.configureEBPF(); err != nil {
		coll.Close()
		return fmt.Errorf("configuring eBPF: %w", err)
	}

	// Attach probes
	if err := o.attachProbes(); err != nil {
		coll.Close()
		return fmt.Errorf("attaching probes: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(coll.Maps["dns_events"])
	if err != nil {
		o.stopPlatform()
		return fmt.Errorf("creating ringbuf reader: %w", err)
	}

	ebpfState := o.ebpfState.(*dnsEBPF)
	ebpfState.reader = reader

	// Start event processor
	ctx, cancel := context.WithCancel(context.Background())
	ebpfState.cancel = cancel

	o.lifecycleManager.Start("ebpf-reader", func() {
		o.processDNSProblems(ctx)
	})

	// Start timeout checker
	o.lifecycleManager.Start("timeout-checker", func() {
		o.checkDNSTimeouts(ctx)
	})

	o.logger.Info("eBPF DNS problem detection started")
	return nil
}

// configureEBPF sets thresholds in BPF maps
func (o *Observer) configureEBPF() error {
	ebpfState := o.ebpfState.(*dnsEBPF)

	configMap := ebpfState.collection.Maps["config"]
	if configMap == nil {
		return fmt.Errorf("config map not found")
	}

	// Set slow query threshold
	key := uint32(0) // CONFIG_SLOW_THRESHOLD_NS
	value := uint64(o.config.SlowQueryThresholdMs) * 1_000_000
	if err := configMap.Update(key, value, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("setting slow threshold: %w", err)
	}

	// Set timeout
	key = uint32(1) // CONFIG_TIMEOUT_NS
	value = uint64(o.config.TimeoutMs) * 1_000_000
	if err := configMap.Update(key, value, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("setting timeout: %w", err)
	}

	return nil
}

// attachProbes attaches eBPF programs to kernel functions
func (o *Observer) attachProbes() error {
	ebpfState := o.ebpfState.(*dnsEBPF)

	// Attach to udp_sendmsg for DNS queries
	prog := ebpfState.collection.Programs["trace_udp_sendmsg"]
	if prog == nil {
		return fmt.Errorf("trace_udp_sendmsg program not found")
	}

	l, err := link.Kprobe("udp_sendmsg", prog, nil)
	if err != nil {
		return fmt.Errorf("attaching udp_sendmsg kprobe: %w", err)
	}
	ebpfState.links = append(ebpfState.links, l)

	// Attach to udp_recvmsg entry to save socket info
	prog = ebpfState.collection.Programs["trace_udp_recvmsg_enter"]
	if prog == nil {
		return fmt.Errorf("trace_udp_recvmsg_enter program not found")
	}

	l, err = link.Kprobe("udp_recvmsg", prog, nil)
	if err != nil {
		return fmt.Errorf("attaching udp_recvmsg kprobe: %w", err)
	}
	ebpfState.links = append(ebpfState.links, l)

	// Attach to udp_recvmsg return for DNS responses
	prog = ebpfState.collection.Programs["trace_udp_recvmsg_ret"]
	if prog == nil {
		return fmt.Errorf("trace_udp_recvmsg_ret program not found")
	}

	l, err = link.Kretprobe("udp_recvmsg", prog, nil)
	if err != nil {
		return fmt.Errorf("attaching udp_recvmsg kretprobe: %w", err)
	}
	ebpfState.links = append(ebpfState.links, l)

	o.logger.Debug("Attached DNS probes", zap.Int("count", len(ebpfState.links)))
	return nil
}

// processDNSProblems reads and processes DNS problem events from eBPF
func (o *Observer) processDNSProblems(ctx context.Context) {
	ebpfState := o.ebpfState.(*dnsEBPF)

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
			o.logger.Warn("Error reading from ringbuf", zap.Error(err))
			continue
		}

		// Parse event
		if len(record.RawSample) < int(unsafe.Sizeof(DNSEvent{})) {
			o.logger.Warn("Invalid event size", zap.Int("size", len(record.RawSample)))
			continue
		}

		event := (*DNSEvent)(unsafe.Pointer(&record.RawSample[0]))

		// Check if this is a repeated problem
		isRepeated := o.trackProblem(event)

		// Convert to domain event
		domainEvent := o.convertToDomainEvent(event, isRepeated)

		// Send event
		if o.EventChannelManager.SendEvent(domainEvent) {
			o.BaseObserver.RecordEvent()
			o.updateMetrics(ctx, event)
		} else {
			o.BaseObserver.RecordDrop()
		}
	}
}

// convertToDomainEvent converts DNS problem event to domain event
func (o *Observer) convertToDomainEvent(event *DNSEvent, isRepeated bool) *domain.CollectorEvent {
	timestamp := time.Unix(0, int64(event.Timestamp))

	// Determine severity based on problem type
	severity := domain.EventSeverityWarning
	if event.ProblemType == DNSProblemTimeout || isRepeated {
		severity = domain.EventSeverityError
	}

	// Build DNS data
	dnsData := &domain.DNSData{
		QueryName:    event.GetQueryName(),
		QueryType:    getQueryTypeName(event.QueryType),
		Duration:     time.Duration(event.LatencyNs),
		ResponseCode: int(event.ResponseCode),
		Error:        true,
		ErrorMessage: getProblemDescription(event),
	}

	// Add server IP if available
	if event.ServerIP[0] != 0 {
		dnsData.ServerIP = formatIP(event.ServerIP[:])
	}

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("dns-problem-%d-%d", event.PID, event.Timestamp),
		Timestamp: timestamp,
		Type:      domain.EventTypeDNS,
		Source:    o.name,
		Severity:  severity,
		EventData: domain.EventDataContainer{
			DNS: dnsData,
			Process: &domain.ProcessData{
				PID:      int32(event.PID),
				TID:      int32(event.TID),
				UID:      int32(event.UID),
				GID:      int32(event.GID),
				Command:  event.GetComm(),
				CgroupID: event.CgroupID,
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer":     "dns",
				"version":      "1.0.0",
				"problem_type": event.ProblemType.String(),
				"repeated":     fmt.Sprintf("%v", isRepeated),
			},
		},
	}
}

// updateMetrics updates DNS problem metrics
func (o *Observer) updateMetrics(ctx context.Context, event *DNSEvent) {
	// Update problem counter
	if o.problemsDetected != nil {
		o.problemsDetected.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("type", event.ProblemType.String())))
	}

	// Update specific problem type counters
	switch event.ProblemType {
	case DNSProblemSlow:
		if o.slowQueries != nil {
			o.slowQueries.Add(ctx, 1)
		}
	case DNSProblemTimeout:
		if o.timeouts != nil {
			o.timeouts.Add(ctx, 1)
		}
	case DNSProblemNXDOMAIN:
		if o.nxdomains != nil {
			o.nxdomains.Add(ctx, 1)
		}
	case DNSProblemSERVFAIL:
		if o.servfails != nil {
			o.servfails.Add(ctx, 1)
		}
	}

	// Record latency
	if o.queryLatency != nil && event.LatencyNs > 0 {
		o.queryLatency.Record(ctx, event.GetLatencyMs())
	}

	// Update stats
	o.mu.Lock()
	o.stats.TotalProblems++
	switch event.ProblemType {
	case DNSProblemSlow:
		o.stats.SlowQueries++
	case DNSProblemTimeout:
		o.stats.Timeouts++
	case DNSProblemNXDOMAIN:
		o.stats.NXDomains++
	case DNSProblemSERVFAIL:
		o.stats.ServerFailures++
	}
	o.stats.LastProblemTime = time.Now()
	o.mu.Unlock()
}

// checkDNSTimeouts scans for queries that never got responses
func (o *Observer) checkDNSTimeouts(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.scanForTimeouts()
		}
	}
}

// scanForTimeouts checks active queries for timeouts
func (o *Observer) scanForTimeouts() {
	ebpfState := o.ebpfState.(*dnsEBPF)
	activeQueries := ebpfState.collection.Maps["active_queries"]
	if activeQueries == nil {
		return
	}

	now := time.Now().UnixNano()
	timeoutNs := uint64(o.config.TimeoutMs) * 1_000_000

	var key uint64
	var state DNSEvent // Using our Go struct to read the BPF state

	iter := activeQueries.Iterate()
	for iter.Next(&key, &state) {
		// Check if query has timed out
		elapsed := uint64(now) - state.Timestamp
		if elapsed > timeoutNs {
			// Create timeout event
			event := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("dns-timeout-%d-%d", key>>32, now),
				Timestamp: time.Now(),
				Type:      domain.EventTypeDNS,
				Source:    o.name,
				Severity:  domain.EventSeverityError,
				EventData: domain.EventDataContainer{
					DNS: &domain.DNSData{
						QueryName:    state.GetQueryName(),
						Duration:     time.Duration(elapsed),
						Error:        true,
						ErrorMessage: fmt.Sprintf("DNS query timeout after %dms", elapsed/1_000_000),
					},
					Process: &domain.ProcessData{
						PID:     int32(key >> 32),
						Command: state.GetComm(),
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer":     "dns",
						"version":      "1.0.0",
						"problem_type": "timeout",
					},
				},
			}

			// Send timeout event
			if o.EventChannelManager.SendEvent(event) {
				o.BaseObserver.RecordEvent()
				o.mu.Lock()
				o.stats.Timeouts++
				o.mu.Unlock()
			}

			// Clean up timed-out query
			activeQueries.Delete(key)
		}
	}
}

// stopPlatform stops eBPF programs
func (o *Observer) stopPlatform() {
	if o.ebpfState == nil {
		return
	}

	ebpfState := o.ebpfState.(*dnsEBPF)

	// Cancel context
	if ebpfState.cancel != nil {
		ebpfState.cancel()
	}

	// Close reader
	if ebpfState.reader != nil {
		ebpfState.reader.Close()
	}

	// Detach probes
	for _, l := range ebpfState.links {
		l.Close()
	}

	// Close collection
	if ebpfState.collection != nil {
		ebpfState.collection.Close()
	}

	o.logger.Info("eBPF DNS problem detection stopped")
}

// Helper functions
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

func formatIP(ip []byte) string {
	// Check for IPv6 marker
	if ip[0] == 0xFF && ip[1] == 0xFF {
		return "IPv6"
	}

	// Check if IPv4 (only first 4 bytes non-zero)
	isIPv4 := true
	for i := 4; i < 16; i++ {
		if ip[i] != 0 {
			isIPv4 = false
			break
		}
	}

	if isIPv4 && ip[0] != 0 {
		return fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3])
	}

	// Unknown or empty
	return ""
}

func getProblemDescription(event *DNSEvent) string {
	switch event.ProblemType {
	case DNSProblemSlow:
		return fmt.Sprintf("Query took %.2fms", event.GetLatencyMs())
	case DNSProblemTimeout:
		return fmt.Sprintf("No response after %d retries", event.Retries)
	case DNSProblemNXDOMAIN:
		return "Domain does not exist"
	case DNSProblemSERVFAIL:
		return "DNS server failure"
	case DNSProblemRefused:
		return "Query refused by server"
	case DNSProblemTruncated:
		return "Response truncated, TCP fallback needed"
	default:
		return "Unknown DNS problem"
	}
}
