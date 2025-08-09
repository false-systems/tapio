//go:build linux
// +build linux

package cni

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

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 cniMonitor ./bpf_src/cni_monitor.c -- -I../bpf_common

// cniEvent represents a network event from eBPF
type cniEvent struct {
	Timestamp uint64
	PID       uint32
	Netns     uint32
	EventType uint32
	Comm      [16]byte
	Data      [64]byte
}

// eBPF components
type ebpfState struct {
	objs   *cniMonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes eBPF monitoring with comprehensive OTEL instrumentation
func (c *Collector) startEBPF() error {
	ctx, span := c.tracer.Start(context.Background(), "cni.ebpf.start",
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
	c.ebpfLoadsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("collector.name", c.name),
		),
	)

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.ebpfLoadErrors.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
				attribute.String("error.type", "memlock_removal"),
			),
		)
		span.RecordError(err)
		c.logger.Error("Failed to remove memlock",
			zap.Error(err),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load eBPF objects
	objs := &cniMonitorObjects{}
	if err := loadCniMonitorObjects(objs, nil); err != nil {
		c.ebpfLoadErrors.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
				attribute.String("error.type", "object_loading"),
			),
		)
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

	// Attach to network namespace operations with metrics
	c.ebpfAttachTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("collector.name", c.name),
			attribute.String("tracepoint", "sys_enter_setns"),
		),
	)
	l1, err := link.Tracepoint("syscalls", "sys_enter_setns", objs.TraceSysEnterSetns, nil)
	if err != nil {
		c.ebpfAttachErrors.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
				attribute.String("tracepoint", "sys_enter_setns"),
				attribute.String("error.type", "attach_failure"),
			),
		)
		objs.Close()
		span.RecordError(err)
		c.logger.Error("Failed to attach setns tracepoint",
			zap.Error(err),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
		return fmt.Errorf("attaching setns tracepoint: %w", err)
	}
	state.links = append(state.links, l1)

	// Attach to unshare (new network namespace)
	c.ebpfAttachTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("collector.name", c.name),
			attribute.String("tracepoint", "sys_enter_unshare"),
		),
	)
	l2, err := link.Tracepoint("syscalls", "sys_enter_unshare", objs.TraceSysEnterUnshare, nil)
	if err != nil {
		c.ebpfAttachErrors.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
				attribute.String("tracepoint", "sys_enter_unshare"),
				attribute.String("error.type", "attach_failure"),
			),
		)
		state.cleanup()
		span.RecordError(err)
		c.logger.Error("Failed to attach unshare tracepoint",
			zap.Error(err),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
		return fmt.Errorf("attaching unshare tracepoint: %w", err)
	}
	state.links = append(state.links, l2)

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		c.ebpfLoadErrors.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
				attribute.String("error.type", "ringbuf_creation"),
			),
		)
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
		var event cniEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			c.logger.Debug("Failed to parse eBPF event",
				zap.Error(err),
				zap.String("collector.name", c.name),
				zap.String("trace.id", span.SpanContext().TraceID().String()),
			)
			continue
		}

		// Create event data with type conversion
		eventData := map[string]string{
			"timestamp": fmt.Sprintf("%d", event.Timestamp),
			"pid":       fmt.Sprintf("%d", event.PID),
			"netns":     fmt.Sprintf("%d", event.Netns),
			"type":      eventTypeToString(event.EventType),
			"comm":      nullTerminatedString(event.Comm[:]),
			"data":      nullTerminatedString(event.Data[:]),
		}

		// Create event with tracing context
		rawEvent := c.createEvent("network_namespace", eventData)

		// Try to send event with metrics tracking
		select {
		case c.events <- rawEvent:
			c.eventsProcessed.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
					attribute.String("event.type", eventTypeToString(event.EventType)),
				),
			)
		case <-c.ctx.Done():
			c.logger.Info("Context cancelled during event processing",
				zap.String("collector.name", c.name),
				zap.String("trace.id", span.SpanContext().TraceID().String()),
			)
			return
		default:
			// Buffer full, drop event and record metric
			c.eventsDropped.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
					attribute.String("reason", "buffer_full"),
				),
			)
			c.logger.Warn("Dropped event due to full buffer",
				zap.String("collector.name", c.name),
				zap.String("event.type", eventTypeToString(event.EventType)),
				zap.String("trace.id", span.SpanContext().TraceID().String()),
			)
		}

		// Record processing latency
		duration := time.Since(start).Seconds()
		c.processingLatency.Record(ctx, duration,
			metric.WithAttributes(
				attribute.String("operation", "ebpf_event_processing"),
				attribute.String("event.type", eventTypeToString(event.EventType)),
			),
		)
	}
}

// Helper to convert event type to string
func eventTypeToString(t uint32) string {
	switch t {
	case 1:
		return "netns_enter"
	case 2:
		return "netns_create"
	case 3:
		return "netns_exit"
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
