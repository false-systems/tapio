//go:build linux
// +build linux

package status

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/internal/observers/status/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// statusEvent from BPF - must match the C struct `struct status_event` in status_monitor.c exactly
// C struct is __attribute__((packed)) = 60 bytes, Go struct has padding = 64 bytes
type statusEvent struct {
	Timestamp    uint64   // __u64 timestamp
	PID          uint32   // __u32 pid
	TID          uint32   // __u32 tid
	ServiceHash  uint32   // __u32 service_hash
	EndpointHash uint32   // __u32 endpoint_hash
	LatencyUS    uint32   // __u32 latency_us
	StatusCode   uint16   // __u16 status_code
	ErrorType    uint16   // __u16 error_type
	Protocol     uint16   // __u16 protocol
	Port         uint16   // __u16 port
	SrcIP        uint32   // __u32 src_ip
	DstIP        uint32   // __u32 dst_ip
	Comm         [16]byte // char comm[16]
	// No padding fields - Go will add 4 bytes padding to align to 8-byte boundary
}

// statusEBPF contains all eBPF state
type statusEBPF struct {
	objs   *bpf.StatusMonitorObjects
	links  []link.Link
	reader *ringbuf.Reader

	// Maps for direct access
	connTracker *ebpf.Map

	// Metrics
	eventsProcessed metric.Int64Counter
	eventsDropped   metric.Int64Counter
	processingTime  metric.Float64Histogram
	httpErrors      metric.Int64Counter
	grpcErrors      metric.Int64Counter
	timeouts        metric.Int64Counter
	errorRate       metric.Float64ObservableGauge

	logger *zap.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// startEBPF initializes and attaches eBPF programs with CO-RE support
func (o *Observer) startEBPF() error {
	o.logger.Info("Starting status observer with CO-RE eBPF support")
	return o.loadEBPF()
}

// loadEBPF loads the CO-RE eBPF programs
func (o *Observer) loadEBPF() error {
	o.logger.Info("Loading CO-RE eBPF programs for Status observer")

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("removing memlock: %w", err)
	}

	// Load eBPF objects
	var objs bpf.StatusMonitorObjects
	opts := &ebpf.CollectionOptions{
		Programs: ebpf.ProgramOptions{
			LogSize: 64 * 1024 * 1024, // 64MB for verifier logs
		},
	}

	err := bpf.LoadStatusMonitorObjects(&objs, opts)
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			o.logger.Error("BPF verifier error",
				zap.String("error", ve.Error()))
			return fmt.Errorf("BPF verifier rejected program: %w", err)
		}
		return fmt.Errorf("loading BPF objects: %w", err)
	}

	o.ebpfState = &statusEBPF{
		objs:            &objs,
		links:           make([]link.Link, 0),
		connTracker:     objs.ConnTracker,
		eventsProcessed: o.eventsProcessed,
		eventsDropped:   o.eventsDropped,
		processingTime:  o.processingTime,
		httpErrors:      o.httpErrors,
		grpcErrors:      o.grpcErrors,
		timeouts:        o.timeouts,
		errorRate:       o.errorRate,
		logger:          o.logger,
	}

	// Attach probes
	if err := o.attachStatusProbes(); err != nil {
		objs.Close()
		return fmt.Errorf("attaching probes: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.StatusEvents)
	if err != nil {
		o.closeEBPF()
		return fmt.Errorf("creating ringbuf reader: %w", err)
	}

	ebpfState := o.ebpfState.(*statusEBPF)
	ebpfState.reader = reader

	// Start event processor
	ctx, cancel := context.WithCancel(context.Background())
	ebpfState.cancel = cancel

	ebpfState.wg.Add(1)
	go o.processEBPFEvents(ctx)

	// Start aggregation worker
	ebpfState.wg.Add(1)
	go o.aggregateMetrics(ctx)

	o.logger.Info("CO-RE eBPF programs loaded successfully for Status observer")
	return nil
}

// attachStatusProbes attaches the eBPF probes
func (o *Observer) attachStatusProbes() error {
	ebpfState := o.ebpfState.(*statusEBPF)

	// Attach TCP connect probe
	l, err := link.Kprobe("tcp_v4_connect", ebpfState.objs.TraceTcpConnect, nil)
	if err != nil {
		o.logger.Warn("Failed to attach tcp_v4_connect probe", zap.Error(err))
	} else {
		ebpfState.links = append(ebpfState.links, l)
	}

	// Attach TCP close probe
	l, err = link.Kprobe("tcp_close", ebpfState.objs.TraceTcpClose, nil)
	if err != nil {
		o.logger.Warn("Failed to attach tcp_close probe", zap.Error(err))
	} else {
		ebpfState.links = append(ebpfState.links, l)
	}

	// Note: HTTP response probes would be attached to user-space functions
	// For example, uprobe on libcurl or Go's http.Client.Do
	// This requires more complex setup and is application-specific

	return nil
}

// processEBPFEvents reads events from the ring buffer
func (o *Observer) processEBPFEvents(ctx context.Context) {
	defer o.ebpfState.(*statusEBPF).wg.Done()

	o.logger.Info("Started reading status events from ring buffer")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		record, err := o.ebpfState.(*statusEBPF).reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			o.logger.Warn("Error reading from ring buffer", zap.Error(err))
			o.RecordError(err)
			continue
		}

		// Process the raw event
		if err := o.processRawStatusEvent(ctx, record.RawSample); err != nil {
			o.logger.Warn("Failed to process status event", zap.Error(err))
			o.RecordError(err)
		}
	}
}

// processRawStatusEvent processes a raw eBPF event
func (o *Observer) processRawStatusEvent(ctx context.Context, data []byte) error {
	start := time.Now()
	defer func() {
		if o.processingTime != nil {
			duration := time.Since(start).Milliseconds()
			o.processingTime.Record(ctx, float64(duration), metric.WithAttributes(
				attribute.String("observer", o.name),
			))
		}
	}()

	// Validate minimum size
	if len(data) < int(unsafe.Sizeof(statusEvent{})) {
		return fmt.Errorf("invalid event size: %d", len(data))
	}

	// Parse the event
	var event statusEvent
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &event); err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// Update metrics based on event
	o.updateStatusMetrics(ctx, &event)

	// Convert to domain event
	domainEvent := o.convertToDomainEvent(ctx, &event)
	if domainEvent == nil {
		return nil // Event filtered out
	}

	// Send event through channel
	if !o.EventChannelManager.SendEvent(domainEvent) {
		o.logger.Warn("Failed to send event - channel full")
		o.RecordDrop()
		return fmt.Errorf("event channel full")
	}

	// Update processed counter
	o.RecordEvent()
	if o.eventsProcessed != nil {
		o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
			attribute.String("event_type", string(domainEvent.Type)),
		))
	}

	return nil
}

// updateStatusMetrics updates metrics based on status event
func (o *Observer) updateStatusMetrics(ctx context.Context, event *statusEvent) {
	// Track HTTP errors
	if event.StatusCode >= 500 && o.httpErrors != nil {
		o.httpErrors.Add(ctx, 1, metric.WithAttributes(
			attribute.Int("status_code", int(event.StatusCode)),
			attribute.String("service", o.hashDecoder.GetService(event.ServiceHash)),
		))
	}

	// Track timeouts
	if event.ErrorType == STATUS_ERROR_TIMEOUT && o.timeouts != nil {
		o.timeouts.Add(ctx, 1, metric.WithAttributes(
			attribute.String("service", o.hashDecoder.GetService(event.ServiceHash)),
		))
	}

	// Track latency
	if event.LatencyUS > 0 && o.latency != nil {
		o.latency.Record(ctx, float64(event.LatencyUS)/1000.0, metric.WithAttributes(
			attribute.String("service", o.hashDecoder.GetService(event.ServiceHash)),
			attribute.String("endpoint", o.hashDecoder.GetEndpoint(event.EndpointHash)),
		))
	}

	// Update aggregator
	if o.aggregator != nil {
		o.aggregator.Add(&StatusEvent{
			ServiceHash:  event.ServiceHash,
			EndpointHash: event.EndpointHash,
			StatusCode:   event.StatusCode,
			ErrorType:    ErrorType(event.ErrorType),
			Timestamp:    event.Timestamp,
			Latency:      event.LatencyUS,
			PID:          event.PID,
		})
	}
}

// convertToDomainEvent converts raw eBPF event to domain event
func (o *Observer) convertToDomainEvent(ctx context.Context, event *statusEvent) *domain.CollectorEvent {
	// Convert comm to string
	comm := string(bytes.TrimRight(event.Comm[:], "\x00"))

	// Determine event severity based on error type
	severity := domain.EventSeverityInfo
	if event.ErrorType == STATUS_ERROR_5XX || event.ErrorType == STATUS_ERROR_TIMEOUT {
		severity = domain.EventSeverityError
	} else if event.ErrorType == STATUS_ERROR_4XX {
		severity = domain.EventSeverityWarning
	}

	// Create network data
	networkData := &domain.NetworkData{
		SrcIP:    fmt.Sprintf("%d.%d.%d.%d", byte(event.SrcIP), byte(event.SrcIP>>8), byte(event.SrcIP>>16), byte(event.SrcIP>>24)),
		DstIP:    fmt.Sprintf("%d.%d.%d.%d", byte(event.DstIP), byte(event.DstIP>>8), byte(event.DstIP>>16), byte(event.DstIP>>24)),
		DstPort:  int32(event.Port),
		Protocol: getProtocolName(event.Protocol),
	}

	// Create domain event
	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("status-%d-%d-%d", event.ServiceHash, event.PID, event.Timestamp),
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Type:      domain.EventTypeNetworkConnection,
		Source:    o.name,
		Severity:  severity,
		EventData: domain.EventDataContainer{
			Network: networkData,
			Process: &domain.ProcessData{
				PID:     int32(event.PID),
				Command: comm,
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer":      "status",
				"version":       "1.0.0",
				"latency_us":    fmt.Sprintf("%d", event.LatencyUS),
				"status_code":   fmt.Sprintf("%d", event.StatusCode),
				"error_type":    getErrorTypeName(event.ErrorType),
				"service_hash":  fmt.Sprintf("%d", event.ServiceHash),
				"endpoint_hash": fmt.Sprintf("%d", event.EndpointHash),
			},
		},
	}
}

// aggregateMetrics periodically aggregates and publishes metrics
func (o *Observer) aggregateMetrics(ctx context.Context) {
	defer o.ebpfState.(*statusEBPF).wg.Done()

	ticker := time.NewTicker(o.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			aggregates := o.aggregator.Flush()
			o.updateErrorRates(aggregates)
		}
	}
}

// closeEBPF cleans up eBPF resources
func (o *Observer) closeEBPF() {
	if o.ebpfState != nil {
		ebpfState := o.ebpfState.(*statusEBPF)

		// Cancel context and wait for goroutines
		if ebpfState.cancel != nil {
			ebpfState.cancel()
		}
		ebpfState.wg.Wait()

		// Close reader
		if ebpfState.reader != nil {
			ebpfState.reader.Close()
		}

		// Close links
		for _, l := range ebpfState.links {
			if l != nil {
				l.Close()
			}
		}

		// Close eBPF objects
		if ebpfState.objs != nil {
			ebpfState.objs.Close()
		}

		o.ebpfState = nil
	}
}

// stopEBPF stops the eBPF programs
func (o *Observer) stopEBPF() error {
	o.closeEBPF()
	return nil
}

// getProtocolName returns the protocol name for a protocol number
func getProtocolName(proto uint16) string {
	switch proto {
	case 1:
		return "HTTP"
	case 2:
		return "gRPC"
	case 3:
		return "TCP"
	default:
		return "Unknown"
	}
}

// getErrorTypeName returns the error type name
func getErrorTypeName(errorType uint16) string {
	switch errorType {
	case STATUS_OK:
		return "OK"
	case STATUS_ERROR_TIMEOUT:
		return "Timeout"
	case STATUS_ERROR_REFUSED:
		return "Refused"
	case STATUS_ERROR_RESET:
		return "Reset"
	case STATUS_ERROR_5XX:
		return "5XX"
	case STATUS_ERROR_4XX:
		return "4XX"
	case STATUS_ERROR_SLOW:
		return "Slow"
	case STATUS_ERROR_PARTIAL:
		return "Partial"
	default:
		return "Unknown"
	}
}

// Error type constants (must match BPF code)
const (
	STATUS_OK            = 0
	STATUS_ERROR_TIMEOUT = 1
	STATUS_ERROR_REFUSED = 2
	STATUS_ERROR_RESET   = 3
	STATUS_ERROR_5XX     = 4
	STATUS_ERROR_4XX     = 5
	STATUS_ERROR_SLOW    = 6
	STATUS_ERROR_PARTIAL = 7
)
