//go:build linux
// +build linux

package runtime_signals

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -tags linux -target amd64,arm64 runtimeMonitor ./bpf_src/runtime_monitor.c -- -I../bpf_common

// eBPF components - implements EBPFState interface
type ebpfState struct {
	objs   *runtimeMonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// IsLoaded returns true if eBPF programs are loaded
func (s *ebpfState) IsLoaded() bool {
	return s.objs != nil
}

// LinkCount returns the number of active eBPF links
func (s *ebpfState) LinkCount() int {
	return len(s.links)
}

// startEBPF initializes eBPF monitoring with comprehensive OTEL instrumentation
func (c *Collector) startEBPF() error {
	ctx, span := c.tracer.Start(context.Background(), "namespace.ebpf.start",
		trace.WithAttributes(
			attribute.String("collector.name", c.name),
		),
	)
	defer span.End()

	c.logger.Info("Starting eBPF monitoring",
		zap.String("collector.name", c.name),
		zap.String("trace.id", span.SpanContext().TraceID().String()),
	)

	// Record eBPF load attempt
	if c.ebpfLoadsTotal != nil {
		c.ebpfLoadsTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
			),
		)
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		if c.ebpfLoadErrors != nil {
			c.ebpfLoadErrors.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
					attribute.String("error.type", "memlock_removal"),
				),
			)
		}
		span.RecordError(err)
		c.logger.Error("Failed to remove memlock",
			zap.Error(err),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load eBPF objects
	objs := &runtimeMonitorObjects{}
	if err := loadRuntimeMonitorObjects(objs, nil); err != nil {
		if c.ebpfLoadErrors != nil {
			c.ebpfLoadErrors.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
					attribute.String("error.type", "object_loading"),
				),
			)
		}
		span.RecordError(err)
		c.logger.Error("Failed to load eBPF objects",
			zap.Error(err),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	// Create eBPF state
	state := &ebpfState{
		objs:  objs,
		links: make([]link.Link, 0),
	}

	// Attach to process exec tracepoint
	if c.ebpfAttachTotal != nil {
		c.ebpfAttachTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
				attribute.String("tracepoint", "sched_process_exec"),
			),
		)
	}
	l1, err := link.Tracepoint("sched", "sched_process_exec", objs.TraceProcessExec, nil)
	if err != nil {
		if c.ebpfAttachErrors != nil {
			c.ebpfAttachErrors.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
					attribute.String("tracepoint", "sched_process_exec"),
					attribute.String("error.type", "attach_failure"),
				),
			)
		}
		objs.Close()
		span.RecordError(err)
		c.logger.Error("Failed to attach process exec tracepoint",
			zap.Error(err),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
		return fmt.Errorf("attaching process exec tracepoint: %w", err)
	}
	state.links = append(state.links, l1)

	// Attach to process exit tracepoint
	if c.ebpfAttachTotal != nil {
		c.ebpfAttachTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
				attribute.String("tracepoint", "sched_process_exit"),
			),
		)
	}
	l2, err := link.Tracepoint("sched", "sched_process_exit", objs.TraceProcessExit, nil)
	if err != nil {
		if c.ebpfAttachErrors != nil {
			c.ebpfAttachErrors.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
					attribute.String("tracepoint", "sched_process_exit"),
					attribute.String("error.type", "attach_failure"),
				),
			)
		}
		state.cleanup()
		span.RecordError(err)
		c.logger.Error("Failed to attach process exit tracepoint",
			zap.Error(err),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
		return fmt.Errorf("attaching process exit tracepoint: %w", err)
	}
	state.links = append(state.links, l2)

	// Attach to signal generation tracepoint
	if c.ebpfAttachTotal != nil {
		c.ebpfAttachTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
				attribute.String("tracepoint", "signal_generate"),
			),
		)
	}
	l3, err := link.Tracepoint("signal", "signal_generate", objs.TraceSignalGenerate, nil)
	if err != nil {
		if c.ebpfAttachErrors != nil {
			c.ebpfAttachErrors.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
					attribute.String("tracepoint", "signal_generate"),
					attribute.String("error.type", "attach_failure"),
				),
			)
		}
		state.cleanup()
		span.RecordError(err)
		c.logger.Error("Failed to attach signal generate tracepoint",
			zap.Error(err),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
		return fmt.Errorf("attaching signal generate tracepoint: %w", err)
	}
	state.links = append(state.links, l3)

	// Attach to signal delivery tracepoint
	if c.ebpfAttachTotal != nil {
		c.ebpfAttachTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
				attribute.String("tracepoint", "signal_deliver"),
			),
		)
	}
	l4, err := link.Tracepoint("signal", "signal_deliver", objs.TraceSignalDeliver, nil)
	if err != nil {
		if c.ebpfAttachErrors != nil {
			c.ebpfAttachErrors.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
					attribute.String("tracepoint", "signal_deliver"),
					attribute.String("error.type", "attach_failure"),
				),
			)
		}
		state.cleanup()
		span.RecordError(err)
		c.logger.Error("Failed to attach signal deliver tracepoint",
			zap.Error(err),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
		return fmt.Errorf("attaching signal deliver tracepoint: %w", err)
	}
	state.links = append(state.links, l4)

	// Attach to OOM killer kprobe
	if c.ebpfAttachTotal != nil {
		c.ebpfAttachTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
				attribute.String("kprobe", "oom_kill_process"),
			),
		)
	}
	l5, err := link.Kprobe("oom_kill_process", objs.TraceOomKill, nil)
	if err != nil {
		// OOM killer hook is optional (may not exist on all kernels)
		c.logger.Warn("Failed to attach OOM killer kprobe (optional)",
			zap.Error(err),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
	} else {
		state.links = append(state.links, l5)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		if c.ebpfLoadErrors != nil {
			c.ebpfLoadErrors.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
					attribute.String("error.type", "ringbuf_creation"),
				),
			)
		}
		state.cleanup()
		span.RecordError(err)
		c.logger.Error("Failed to create ring buffer reader",
			zap.Error(err),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	state.reader = reader

	// Store state and start reading events
	c.ebpfState = state
	go c.readEBPFEvents()

	span.SetAttributes(attribute.Bool("ebpf.started", true))
	c.logger.Info("eBPF monitoring started successfully",
		zap.String("collector.name", c.name),
		zap.Int("attached_links", len(state.links)),
		zap.String("trace.id", span.SpanContext().TraceID().String()),
	)

	return nil
}

// stopEBPF cleans up eBPF resources
func (c *Collector) stopEBPF() {
	if state, ok := c.ebpfState.(*ebpfState); ok && state != nil {
		state.cleanup()
		c.ebpfState = nil
	}
}

// cleanup releases all eBPF resources
func (s *ebpfState) cleanup() {
	if s.reader != nil {
		s.reader.Close()
	}
	for _, l := range s.links {
		l.Close()
	}
	if s.objs != nil {
		s.objs.Close()
	}
}

// readEBPFEvents reads events from eBPF ring buffer with comprehensive OTEL tracing
func (c *Collector) readEBPFEvents() {
	ctx, span := c.tracer.Start(context.Background(), "cni.ebpf.read_events",
		trace.WithAttributes(
			attribute.String("collector.name", c.name),
		),
	)
	defer span.End()

	c.logger.Info("Starting eBPF event reading loop",
		zap.String("collector.name", c.name),
		zap.String("trace.id", span.SpanContext().TraceID().String()),
	)

	state, ok := c.ebpfState.(*ebpfState)
	if !ok || state == nil {
		c.logger.Error("Invalid eBPF state",
			zap.String("collector.name", c.name),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
		span.RecordError(fmt.Errorf("invalid eBPF state"))
		return
	}

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("eBPF event reading loop stopped due to context cancellation",
				zap.String("collector.name", c.name),
				zap.String("trace.id", span.SpanContext().TraceID().String()),
			)
			return
		default:
		}

		start := time.Now()
		record, err := state.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				c.logger.Info("Ring buffer closed, stopping event reading",
					zap.String("collector.name", c.name),
					zap.String("trace.id", span.SpanContext().TraceID().String()),
				)
				return
			}
			c.logger.Debug("Error reading from ring buffer",
				zap.Error(err),
				zap.String("collector.name", c.name),
				zap.String("trace.id", span.SpanContext().TraceID().String()),
			)
			continue
		}

		// Parse the event with error handling
		var event runtimeEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			c.logger.Debug("Failed to parse eBPF event",
				zap.Error(err),
				zap.String("collector.name", c.name),
				zap.String("trace.id", span.SpanContext().TraceID().String()),
			)
			continue
		}

		// Process the runtime event
		c.processRuntimeEvent(ctx, &event)

		// Record processing latency
		if c.processingTime != nil {
			duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
			c.processingTime.Record(ctx, duration,
				metric.WithAttributes(
					attribute.String("operation", "ebpf_event_processing"),
					attribute.String("event.type", runtimeEventTypeToString(event.EventType)),
				),
			)
		}
	}
}

// processRuntimeEvent processes a runtime event from eBPF
func (c *Collector) processRuntimeEvent(ctx context.Context, event *runtimeEvent) {
	// Convert to domain event
	runtimeEvt := &RuntimeSignalEvent{
		Timestamp: event.Timestamp,
		EventType: runtimeEventTypeToString(event.EventType),
		PID:       event.PID,
		TGID:      event.TGID,
		PPID:      event.PPID,
		Command:   nullTerminatedString(event.Comm[:]),
	}

	// Handle different event types
	switch event.EventType {
	case EventTypeProcessExec:
		runtimeEvt.UID = event.ExecInfo.UID
		runtimeEvt.GID = event.ExecInfo.GID
	case EventTypeProcessExit:
		runtimeEvt.ExitInfo = DecodeExitCode(event.ExitCode)
	case EventTypeSignalGenerate, EventTypeSignalDeliver:
		runtimeEvt.SignalInfo = &SignalInfo{
			Number:      int(event.Signal),
			Name:        GetSignalName(int(event.Signal)),
			Description: GetSignalDescription(int(event.Signal)),
			IsFatal:     IsSignalFatal(int(event.Signal)),
		}
		runtimeEvt.SenderPID = event.SenderPID
	case EventTypeOOMKill:
		runtimeEvt.IsOOMKill = true
		runtimeEvt.SignalInfo = &SignalInfo{
			Number:      SIGKILL,
			Name:        "SIGKILL",
			Description: "OOM Killer",
			IsFatal:     true,
		}
		runtimeEvt.SenderPID = event.SenderPID
	}

	// Create domain event
	eventData := map[string]string{
		"event_type": runtimeEvt.EventType,
		"pid":        fmt.Sprintf("%d", runtimeEvt.PID),
		"tgid":       fmt.Sprintf("%d", runtimeEvt.TGID),
		"ppid":       fmt.Sprintf("%d", runtimeEvt.PPID),
		"command":    runtimeEvt.Command,
	}

	if runtimeEvt.ExitInfo != nil {
		eventData["exit_code"] = fmt.Sprintf("%d", runtimeEvt.ExitInfo.Code)
		eventData["exit_signal"] = fmt.Sprintf("%d", runtimeEvt.ExitInfo.Signal)
		eventData["exit_description"] = runtimeEvt.ExitInfo.Description
	}

	if runtimeEvt.SignalInfo != nil {
		eventData["signal_number"] = fmt.Sprintf("%d", runtimeEvt.SignalInfo.Number)
		eventData["signal_name"] = runtimeEvt.SignalInfo.Name
		eventData["signal_description"] = runtimeEvt.SignalInfo.Description
		if event.SenderPID > 0 {
			eventData["sender_pid"] = fmt.Sprintf("%d", event.SenderPID)
		}
	}

	domainEvent := c.createEvent(runtimeEvt.EventType, eventData)

	// Update buffer usage gauge
	if c.bufferUsage != nil {
		c.bufferUsage.Record(ctx, int64(len(c.events)),
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
			),
		)
	}

	// Try to send event with metrics tracking
	select {
	case c.events <- domainEvent:
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
					attribute.String("event.type", runtimeEvt.EventType),
				),
			)
		}
	case <-c.ctx.Done():
		c.logger.Info("Context cancelled during event processing",
			zap.String("collector.name", c.name),
		)
		return
	default:
		// Buffer full, drop event and record metric
		if c.droppedEvents != nil {
			c.droppedEvents.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
					attribute.String("reason", "buffer_full"),
				),
			)
		}
		c.logger.Warn("Dropped event due to full buffer",
			zap.String("collector.name", c.name),
			zap.String("event.type", runtimeEvt.EventType),
		)
	}
}

// Helper to convert runtime event type to string
func runtimeEventTypeToString(t uint32) string {
	switch t {
	case EventTypeProcessExec:
		return "process_exec"
	case EventTypeProcessExit:
		return "process_exit"
	case EventTypeSignalGenerate:
		return "signal_sent"
	case EventTypeSignalDeliver:
		return "signal_received"
	case EventTypeOOMKill:
		return "oom_kill"
	case EventTypeCoreDump:
		return "core_dump"
	default:
		return fmt.Sprintf("unknown_%d", t)
	}
}

// Helper to convert null-terminated byte array to string
func nullTerminatedString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
