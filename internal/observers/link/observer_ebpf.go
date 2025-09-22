//go:build linux
// +build linux

package link

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/internal/observers/link/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// linkEBPF contains all eBPF state
type linkEBPF struct {
	objs   *bpf.LinkMonitorObjects
	links  []link.Link
	reader *ringbuf.Reader

	// Metrics
	eventsProcessed metric.Int64Counter
	eventsDropped   metric.Int64Counter
	processingTime  metric.Float64Histogram

	logger *zap.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// startEBPF initializes and attaches eBPF programs with CO-RE support
func (o *Observer) startEBPF() error {
	o.logger.Info("Starting link observer with CO-RE eBPF support")
	return o.loadEBPF()
}

// loadEBPF loads the CO-RE eBPF programs
func (o *Observer) loadEBPF() error {
	o.logger.Info("Loading CO-RE eBPF programs for Link observer")

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("removing memlock: %w", err)
	}

	// Load eBPF objects
	var objs bpf.LinkMonitorObjects
	opts := &ebpf.CollectionOptions{
		Programs: ebpf.ProgramOptions{
			LogSize: 64 * 1024 * 1024, // 64MB for verifier logs
		},
	}

	err := bpf.LoadLinkMonitorObjects(&objs, opts)
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			o.logger.Error("BPF verifier error",
				zap.String("error", ve.Error()))
			return fmt.Errorf("BPF verifier rejected program: %w", err)
		}
		return fmt.Errorf("loading BPF objects: %w", err)
	}

	o.ebpfState = &linkEBPF{
		objs:            &objs,
		links:           make([]link.Link, 0),
		eventsProcessed: o.eventsProcessed,
		eventsDropped:   o.eventsDropped,
		processingTime:  o.processingTime,
		logger:          o.logger,
	}

	// Attach probes
	if err := o.attachLinkProbes(); err != nil {
		objs.Close()
		return fmt.Errorf("attaching probes: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.LinkEvents)
	if err != nil {
		o.closeEBPF()
		return fmt.Errorf("creating ringbuf reader: %w", err)
	}

	ebpfState := o.ebpfState.(*linkEBPF)
	ebpfState.reader = reader

	// Start event processor
	ctx, cancel := context.WithCancel(context.Background())
	ebpfState.cancel = cancel

	ebpfState.wg.Add(1)
	go o.processEBPFEvents(ctx)

	o.logger.Info("CO-RE eBPF programs loaded successfully for Link observer")
	return nil
}

// attachLinkProbes attaches the eBPF probes
func (o *Observer) attachLinkProbes() error {
	ebpfState := o.ebpfState.(*linkEBPF)

	// Attach TCP SYN probe
	l, err := link.Kprobe("tcp_v4_connect", ebpfState.objs.TraceTcpSyn, nil)
	if err != nil {
		o.logger.Warn("Failed to attach tcp_v4_connect probe", zap.Error(err))
	} else {
		ebpfState.links = append(ebpfState.links, l)
	}

	// Attach TCP established probe (connection complete)
	l, err = link.Kprobe("tcp_finish_connect", ebpfState.objs.TraceTcpEstablished, nil)
	if err != nil {
		o.logger.Warn("Failed to attach tcp_finish_connect probe", zap.Error(err))
	} else {
		ebpfState.links = append(ebpfState.links, l)
	}

	// Attach TCP reset probe (when RST is sent)
	l, err = link.Kprobe("tcp_send_active_reset", ebpfState.objs.TraceTcpReset, nil)
	if err != nil {
		o.logger.Warn("Failed to attach tcp_send_active_reset probe", zap.Error(err))
	} else {
		ebpfState.links = append(ebpfState.links, l)
	}

	// Attach TCP retransmit timer probe (for timeout detection)
	l, err = link.Kprobe("tcp_retransmit_timer", ebpfState.objs.TraceTcpTimeout, nil)
	if err != nil {
		o.logger.Warn("Failed to attach tcp_retransmit_timer probe", zap.Error(err))
	} else {
		ebpfState.links = append(ebpfState.links, l)
	}

	// Attach cleanup probe for stale SYNs (runs on every tcp_close)
	l, err = link.Kprobe("tcp_close", ebpfState.objs.CleanupStaleSyns, nil)
	if err != nil {
		o.logger.Warn("Failed to attach tcp_close probe", zap.Error(err))
	} else {
		ebpfState.links = append(ebpfState.links, l)
	}

	return nil
}

// processEBPFEvents reads events from the ring buffer
func (o *Observer) processEBPFEvents(ctx context.Context) {
	defer o.ebpfState.(*linkEBPF).wg.Done()

	o.logger.Info("Started reading link failure events from ring buffer")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		record, err := o.ebpfState.(*linkEBPF).reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			o.logger.Warn("Error reading from ring buffer", zap.Error(err))
			o.RecordError(err)
			continue
		}

		// Process the raw event
		if err := o.processRawLinkEvent(ctx, record.RawSample); err != nil {
			o.logger.Warn("Failed to process link event", zap.Error(err))
			o.RecordError(err)
		}
	}
}

// processRawLinkEvent processes a raw eBPF event
func (o *Observer) processRawLinkEvent(ctx context.Context, data []byte) error {
	start := time.Now()
	defer func() {
		if o.processingTime != nil {
			duration := time.Since(start).Milliseconds()
			o.processingTime.Record(ctx, float64(duration), metric.WithAttributes(
				attribute.String("observer", o.name),
			))
		}
	}()

	// Parse the event
	var event LinkEvent
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &event); err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// Track the failure
	srcIP := formatIP(event.SrcIP)
	dstIP := formatIP(event.DstIP)
	o.trackFailure(srcIP, dstIP, event.EventType)

	// Update metrics
	o.updateLinkMetrics(ctx, &event)

	// Convert to domain event
	domainEvent := o.convertToDomainEvent(&event)
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
			attribute.String("event_type", GetEventTypeName(event.EventType)),
		))
	}

	return nil
}

// updateLinkMetrics updates metrics based on link event
func (o *Observer) updateLinkMetrics(ctx context.Context, event *LinkEvent) {
	switch event.EventType {
	case EventSYNTimeout:
		if o.synTimeouts != nil {
			o.synTimeouts.Add(ctx, 1, metric.WithAttributes(
				attribute.String("src_ip", formatIP(event.SrcIP)),
				attribute.String("dst_ip", formatIP(event.DstIP)),
			))
		}
	case EventConnectionRST:
		if o.connectionRSTs != nil {
			o.connectionRSTs.Add(ctx, 1, metric.WithAttributes(
				attribute.String("src_ip", formatIP(event.SrcIP)),
				attribute.String("dst_ip", formatIP(event.DstIP)),
			))
		}
	}

	if o.linkFailures != nil {
		o.linkFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String("failure_type", GetEventTypeName(event.EventType)),
		))
	}
}

// convertToDomainEvent converts raw eBPF event to domain event
func (o *Observer) convertToDomainEvent(event *LinkEvent) *domain.CollectorEvent {
	// Convert comm to string
	comm := string(bytes.TrimRight(event.Comm[:], "\x00"))

	// Determine severity based on event type
	severity := domain.EventSeverityWarning
	if event.EventType == EventConnectionRST {
		severity = domain.EventSeverityError
	}

	// Create network data
	networkData := &domain.NetworkData{
		SrcIP:    formatIP(event.SrcIP),
		DstIP:    formatIP(event.DstIP),
		SrcPort:  int32(event.SrcPort),
		DstPort:  int32(event.DstPort),
		Protocol: "TCP",
	}

	// Create domain event
	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("link-%d-%d-%d", event.EventType, event.PID, event.Timestamp),
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
				"observer":     o.name,
				"version":      "1.0.0",
				"failure_type": GetEventTypeName(event.EventType),
				"protocol":     fmt.Sprintf("%d", event.Protocol),
			},
		},
	}
}

// closeEBPF cleans up eBPF resources
func (o *Observer) closeEBPF() {
	if o.ebpfState != nil {
		ebpfState := o.ebpfState.(*linkEBPF)

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
