//go:build linux
// +build linux

package dns

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// dnsEBPF contains eBPF-specific state
type dnsEBPF struct {
	program *DNSeBPFProgram
	events  <-chan *DNSEvent
	logger  *zap.Logger
	cancel  context.CancelFunc
}

// startPlatform starts eBPF-based DNS problem detection
func (o *Observer) startPlatform() error {
	ctx, span := o.tracer.Start(context.Background(), "dns.start_ebpf")
	defer span.End()
	if !o.config.EnableEBPF {
		o.logger.Info("eBPF disabled, using fallback mode")
		span.SetAttributes(attribute.Bool("ebpf_enabled", false))
		return o.startFallback()
	}
	span.SetAttributes(attribute.Bool("ebpf_enabled", true))

	o.logger.Info("Starting eBPF-based DNS problem detection")

	// Create and configure eBPF program
	program := NewDNSeBPFProgram(o.logger)

	// Load eBPF programs
	_, loadSpan := o.tracer.Start(ctx, "dns.load_ebpf")
	if err := program.Load(); err != nil {
		loadSpan.RecordError(err)
		loadSpan.SetStatus(codes.Error, "Failed to load eBPF")
		loadSpan.End()
		return fmt.Errorf("loading eBPF program: %w", err)
	}
	loadSpan.SetStatus(codes.Ok, "eBPF loaded")
	loadSpan.End()

	// Attach tracepoints
	_, attachSpan := o.tracer.Start(ctx, "dns.attach_ebpf")
	if err := program.Attach(); err != nil {
		attachSpan.RecordError(err)
		attachSpan.SetStatus(codes.Error, "Failed to attach eBPF")
		attachSpan.End()
		program.Close()
		return fmt.Errorf("attaching tracepoints: %w", err)
	}
	attachSpan.SetStatus(codes.Ok, "eBPF attached")
	attachSpan.End()

	// Start reading events
	events, err := program.ReadEvents()
	if err != nil {
		program.Close()
		return fmt.Errorf("starting event reader: %w", err)
	}

	o.ebpfState = &dnsEBPF{
		program: program,
		events:  events,
		logger:  o.logger,
		cancel:  func() {}, // Not needed anymore, using lifecycle context
	}

	// Start event processor using lifecycle manager's context
	o.lifecycleManager.Start("ebpf-reader", func() {
		o.processDNSProblems(o.lifecycleManager.Context())
	})

	o.logger.Info("eBPF DNS problem detection started")
	return nil
}

// processDNSProblems reads and processes DNS problem events from eBPF
func (o *Observer) processDNSProblems(ctx context.Context) {
	ebpfState := o.ebpfState.(*dnsEBPF)

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ebpfState.events:
			if !ok {
				o.logger.Debug("eBPF event channel closed")
				return
			}

			// Start a span for event processing
			ctx, span := o.tracer.Start(ctx, "dns.process_event",
				trace.WithAttributes(
					attribute.String("problem_type", event.ProblemType.String()),
					attribute.String("query_name", event.GetQueryName()),
					attribute.Int64("latency_ms", event.GetLatencyMs())))

			// Check if this is a repeated problem
			isRepeated := o.trackProblem(event)
			span.SetAttributes(attribute.Bool("is_repeated", isRepeated))

			// Convert to domain event
			domainEvent := o.convertToDomainEvent(event, isRepeated)

			// Send event
			if o.EventChannelManager.SendEvent(domainEvent) {
				o.BaseObserver.RecordEvent()
				o.updateMetrics(ctx, event)
				span.SetStatus(codes.Ok, "Event sent successfully")
			} else {
				o.BaseObserver.RecordDrop()
				span.SetStatus(codes.Error, "Event dropped - channel full")
			}
			span.End()
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
				PID:     int32(event.PID),
				TID:     int32(event.TID),
				UID:     int32(event.UID),
				GID:     int32(event.GID),
				Command: event.GetComm(),
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer":     o.name,
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
	case DNSProblemNXDomain:
		if o.nxdomains != nil {
			o.nxdomains.Add(ctx, 1)
		}
	case DNSProblemServfail:
		if o.servfails != nil {
			o.servfails.Add(ctx, 1)
		}
	}

	// Record latency
	if o.queryLatency != nil && event.LatencyNs > 0 {
		o.queryLatency.Record(ctx, float64(event.GetLatencyMs()))
	}

	// Update stats
	o.mu.Lock()
	o.stats.TotalProblems++
	switch event.ProblemType {
	case DNSProblemSlow:
		o.stats.SlowQueries++
	case DNSProblemTimeout:
		o.stats.Timeouts++
	case DNSProblemNXDomain:
		o.stats.NXDomains++
	case DNSProblemServfail:
		o.stats.ServerFailures++
	}
	o.stats.LastProblemTime = time.Now()
	o.mu.Unlock()
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

	// Close eBPF program
	if ebpfState.program != nil {
		ebpfState.program.Close()
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
		return fmt.Sprintf("Query took %dms", event.GetLatencyMs())
	case DNSProblemTimeout:
		return fmt.Sprintf("No response after %d retries", event.Retries)
	case DNSProblemNXDomain:
		return "Domain does not exist"
	case DNSProblemServfail:
		return "DNS server failure"
	case DNSProblemRefused:
		return "Query refused by server"
	case DNSProblemTruncated:
		return "Response truncated, TCP fallback needed"
	default:
		return "Unknown DNS problem"
	}
}
