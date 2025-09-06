//go:build linux
// +build linux

package processsignals

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// ebpfObjects contains all eBPF objects loaded from the compiled program
type ebpfObjects struct {
	RuntimeEvents       *ebpf.Map
	TraceProcessExec    link.Link
	TraceProcessExit    link.Link
	TraceSignalGenerate link.Link
	TraceSignalDeliver  link.Link
	TraceOomKill        link.Link
}

// initializeEBPF loads and attaches the eBPF programs
func (o *Observer) initializeEBPF(ctx context.Context) error {
	ctx, span := o.tracer.Start(ctx, "runtime.initialize_ebpf")
	defer span.End()

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		o.logger.Warn("Failed to remove memlock limit", zap.Error(err))
	}

	// Record eBPF load attempt
	if o.ebpfLoadsTotal != nil {
		o.ebpfLoadsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("observer", o.name),
		))
	}

	// Load compiled eBPF objects
	objs := &runtimeMonitorObjects{}
	opts := &ebpf.CollectionOptions{}

	if err := loadRuntimeMonitorObjects(objs, opts); err != nil {
		o.logger.Error("Failed to load eBPF objects", zap.Error(err))
		if o.ebpfLoadErrors != nil {
			o.ebpfLoadErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("observer", o.name),
				attribute.String("error", err.Error()),
			))
		}
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Attach tracepoints
	state := &ebpfObjects{
		RuntimeEvents: objs.Events,
	}

	// Attach process exec tracepoint
	if o.ebpfAttachTotal != nil {
		o.ebpfAttachTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("program", "trace_process_exec"),
		))
	}

	execLink, err := link.Tracepoint("sched", "sched_process_exec", objs.TraceProcessExec)
	if err != nil {
		o.logger.Error("Failed to attach process exec tracepoint", zap.Error(err))
		if o.ebpfAttachErrors != nil {
			o.ebpfAttachErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("program", "trace_process_exec"),
			))
		}
		return fmt.Errorf("failed to attach process exec tracepoint: %w", err)
	}
	state.TraceProcessExec = execLink

	// Attach process exit tracepoint
	if o.ebpfAttachTotal != nil {
		o.ebpfAttachTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("program", "trace_process_exit"),
		))
	}

	exitLink, err := link.Tracepoint("sched", "sched_process_exit", objs.TraceProcessExit)
	if err != nil {
		o.logger.Error("Failed to attach process exit tracepoint", zap.Error(err))
		if o.ebpfAttachErrors != nil {
			o.ebpfAttachErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("program", "trace_process_exit"),
			))
		}
		execLink.Close()
		return fmt.Errorf("failed to attach process exit tracepoint: %w", err)
	}
	state.TraceProcessExit = exitLink

	// Attach signal generation tracepoint
	if o.ebpfAttachTotal != nil {
		o.ebpfAttachTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("program", "trace_signal_generate"),
		))
	}

	signalGenLink, err := link.Tracepoint("signal", "signal_generate", objs.TraceSignalGenerate)
	if err != nil {
		o.logger.Error("Failed to attach signal generate tracepoint", zap.Error(err))
		if o.ebpfAttachErrors != nil {
			o.ebpfAttachErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("program", "trace_signal_generate"),
			))
		}
		// Continue without signal tracking - still valuable
	} else {
		state.TraceSignalGenerate = signalGenLink
	}

	// Attach signal deliver tracepoint
	if o.ebpfAttachTotal != nil {
		o.ebpfAttachTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("program", "trace_signal_deliver"),
		))
	}

	signalDelLink, err := link.Tracepoint("signal", "signal_deliver", objs.TraceSignalDeliver)
	if err != nil {
		o.logger.Error("Failed to attach signal deliver tracepoint", zap.Error(err))
		if o.ebpfAttachErrors != nil {
			o.ebpfAttachErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("program", "trace_signal_deliver"),
			))
		}
		// Continue without signal tracking
	} else {
		state.TraceSignalDeliver = signalDelLink
	}

	// Attach OOM kill kprobe
	if o.ebpfAttachTotal != nil {
		o.ebpfAttachTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("program", "trace_oom_kill"),
		))
	}

	oomLink, err := link.Kprobe("oom_kill_process", objs.TraceOomKill)
	if err != nil {
		o.logger.Warn("Failed to attach OOM kill probe", zap.Error(err))
		if o.ebpfAttachErrors != nil {
			o.ebpfAttachErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("program", "trace_oom_kill"),
			))
		}
		// Continue without OOM tracking
	} else {
		state.TraceOomKill = oomLink
	}

	o.ebpfState = state
	o.logger.Info("eBPF programs loaded and attached successfully")

	// Start event reader
	o.LifecycleManager.Start("ebpf-reader", func() {
		o.readEBPFEvents(ctx, objs.Events)
	})

	return nil
}

// cleanupEBPF cleans up eBPF resources
func (o *Observer) cleanupEBPF() {
	if state, ok := o.ebpfState.(*ebpfObjects); ok {
		if state.TraceProcessExec != nil {
			state.TraceProcessExec.Close()
		}
		if state.TraceProcessExit != nil {
			state.TraceProcessExit.Close()
		}
		if state.TraceSignalGenerate != nil {
			state.TraceSignalGenerate.Close()
		}
		if state.TraceSignalDeliver != nil {
			state.TraceSignalDeliver.Close()
		}
		if state.TraceOomKill != nil {
			state.TraceOomKill.Close()
		}
		if state.RuntimeEvents != nil {
			state.RuntimeEvents.Close()
		}
	}
	o.ebpfState = nil
}

// readEBPFEvents reads events from the eBPF perf buffer
func (o *Observer) readEBPFEvents(ctx context.Context, eventsMap *ebpf.Map) {
	// Create perf reader
	reader, err := perf.NewReader(eventsMap, 4096)
	if err != nil {
		o.logger.Error("Failed to create perf reader", zap.Error(err))
		o.BaseObserver.RecordError(err)
		return
	}
	defer reader.Close()

	o.logger.Info("Started reading eBPF events")

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.LifecycleManager.StopChannel():
			return
		default:
		}

		record, err := reader.Read()
		if err != nil {
			if err == perf.ErrClosed {
				return
			}
			o.logger.Warn("Error reading from perf buffer", zap.Error(err))
			o.BaseObserver.RecordError(err)
			continue
		}

		// Process the raw event
		if err := o.processRawEvent(ctx, record.RawSample); err != nil {
			o.logger.Warn("Failed to process raw event", zap.Error(err))
			o.BaseObserver.RecordError(err)
		}
	}
}

// processRawEvent processes a raw eBPF event
func (o *Observer) processRawEvent(ctx context.Context, data []byte) error {
	ctx, span := o.tracer.Start(ctx, "runtime.process_raw_event")
	defer span.End()

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
	if len(data) < int(unsafe.Sizeof(runtimeEvent{})) {
		return fmt.Errorf("invalid event size: %d", len(data))
	}

	// Parse the event
	var event runtimeEvent
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &event); err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// Convert to domain event
	domainEvent := o.convertToDomainEvent(ctx, &event)
	if domainEvent == nil {
		return nil // Event filtered out
	}

	// Track the signal if it's a signal event
	if event.EventType == EventTypeSignalGenerate || event.EventType == EventTypeSignalDeliver {
		o.signalTracker.TrackSignal(event.PID, event.Signal, event.SenderPID)

		if o.signalsByType != nil {
			o.signalsByType.Add(ctx, 1, metric.WithAttributes(
				attribute.String("signal", GetSignalName(int(event.Signal))),
			))
		}
	}

	// Track process lifecycle
	switch event.EventType {
	case EventTypeProcessExec:
		if o.processExecs != nil {
			o.processExecs.Add(ctx, 1)
		}
	case EventTypeProcessExit:
		if o.processExits != nil {
			o.processExits.Add(ctx, 1)
		}
		// Check if this death was correlated with a signal
		if o.signalTracker.IsDeathCorrelated(event.PID) {
			if o.deathsCorrelated != nil {
				o.deathsCorrelated.Add(ctx, 1)
			}
		}
	case EventTypeOOMKill:
		if o.oomKills != nil {
			o.oomKills.Add(ctx, 1)
		}
	}

	// Send event
	if err := o.EventChannelManager.SendEvent(domainEvent); err != nil {
		o.logger.Warn("Failed to send event", zap.Error(err))
		o.BaseObserver.RecordError(err)
		return err
	}

	// Update metrics
	o.BaseObserver.RecordEvent()
	if o.eventsProcessed != nil {
		o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
			attribute.String("event_type", domainEvent.EventType.String()),
		))
	}

	return nil
}

// convertToDomainEvent converts raw eBPF event to domain event
func (o *Observer) convertToDomainEvent(ctx context.Context, event *runtimeEvent) *domain.CollectorEvent {
	// Convert comm to string
	comm := string(bytes.TrimRight(event.Comm[:], "\x00"))
	parentComm := string(bytes.TrimRight(event.ParentComm[:], "\x00"))

	// Create runtime signal event
	runtimeSignalEvent := &RuntimeSignalEvent{
		Timestamp: event.Timestamp,
		PID:       event.PID,
		TGID:      event.TGID,
		PPID:      event.PPID,
		Command:   comm,
		UID:       event.ExecInfo.UID,
		GID:       event.ExecInfo.GID,
	}

	// Set event type and decode based on type
	var eventTypeStr string
	var collectorEventType domain.CollectorEventType

	switch event.EventType {
	case EventTypeProcessExec:
		eventTypeStr = "process_exec"
		collectorEventType = domain.EventTypeKernelProcess
		runtimeSignalEvent.EventType = eventTypeStr

	case EventTypeProcessExit:
		eventTypeStr = "process_exit"
		collectorEventType = domain.EventTypeKernelProcess
		runtimeSignalEvent.EventType = eventTypeStr
		runtimeSignalEvent.ExitInfo = DecodeExitCode(event.ExitCode)

	case EventTypeSignalGenerate:
		eventTypeStr = "signal_sent"
		collectorEventType = domain.EventTypeKernelProcess
		runtimeSignalEvent.EventType = eventTypeStr
		runtimeSignalEvent.SenderPID = event.SenderPID
		runtimeSignalEvent.SignalInfo = &SignalInfo{
			Number:      int(event.Signal),
			Name:        GetSignalName(int(event.Signal)),
			Description: GetSignalDescription(int(event.Signal)),
			IsFatal:     IsSignalFatal(int(event.Signal)),
		}

	case EventTypeSignalDeliver:
		eventTypeStr = "signal_delivered"
		collectorEventType = domain.EventTypeKernelProcess
		runtimeSignalEvent.EventType = eventTypeStr
		runtimeSignalEvent.SignalInfo = &SignalInfo{
			Number:      int(event.Signal),
			Name:        GetSignalName(int(event.Signal)),
			Description: GetSignalDescription(int(event.Signal)),
			IsFatal:     IsSignalFatal(int(event.Signal)),
		}

	case EventTypeOOMKill:
		eventTypeStr = "oom_kill"
		collectorEventType = domain.EventTypeContainerOOM
		runtimeSignalEvent.EventType = eventTypeStr
		runtimeSignalEvent.IsOOMKill = true

	default:
		return nil // Unknown event type
	}

	// Marshal to JSON
	eventData, err := json.Marshal(runtimeSignalEvent)
	if err != nil {
		o.logger.Warn("Failed to marshal event", zap.Error(err))
		return nil
	}

	// Extract K8s metadata if available
	var k8sData *domain.KubernetesData
	if event.CgroupID != 0 {
		// Try to extract pod info from cgroup
		if o.k8sExtractionTotal != nil {
			o.k8sExtractionTotal.Add(ctx, 1)
		}
		// This would require cgroup to pod mapping
		// For now, we'll leave it empty
	}

	// Create process data
	processData := &domain.ProcessData{
		PID:         int32(event.PID),
		PPID:        int32(event.PPID),
		Command:     comm,
		CommandLine: comm,
	}

	// Create domain event
	return &domain.CollectorEvent{
		EventID:        fmt.Sprintf("runtime-%s-%d-%d", eventTypeStr, event.PID, event.Timestamp),
		Timestamp:      time.Unix(0, int64(event.Timestamp)),
		EventType:      collectorEventType,
		CollectorName:  o.name,
		ProcessData:    processData,
		KubernetesData: k8sData,
		RawData:        eventData,
		TraceID:        span.SpanContext().TraceID().String(),
		SpanID:         span.SpanContext().SpanID().String(),
	}
}

// processEvents handles non-eBPF event processing
func (o *Observer) processEvents(ctx context.Context) {
	// This can be used for additional event sources if needed
	// For now, eBPF is our primary source

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.LifecycleManager.StopChannel():
			return
		case <-ticker.C:
			// Periodic health check
			if o.ebpfState == nil && o.config.EnableEBPF {
				o.logger.Warn("eBPF not initialized, operating in degraded mode")
				o.BaseObserver.SetHealthy(false)
			}
		}
	}
}
