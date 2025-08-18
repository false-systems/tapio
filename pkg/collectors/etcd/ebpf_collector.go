//go:build linux
// +build linux

package etcd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang etcdMonitor ./bpf_src/etcd_monitor.c -- -I../bpf_common

// etcdEvent represents a raw event from eBPF
type etcdEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	EventType uint8
	_         [3]byte // padding
	SrcIP     uint32
	DstIP     uint32
	SrcPort   uint16
	DstPort   uint16
	DataLen   uint32
	Data      [256]byte
}

// eBPF components
type ebpfState struct {
	objs   *etcdMonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes eBPF monitoring
func (c *Collector) startEBPF() error {
	start := time.Now()
	ctx, span := c.tracer.Start(c.ctx, "etcd.start_ebpf")
	defer span.End()

	span.SetAttributes(
		attribute.String("ebpf.target", "etcd"),
		attribute.StringSlice("ebpf.syscalls", []string{"write", "fsync"}),
	)

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_memlock"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to remove memlock")
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load eBPF objects
	objs := &etcdMonitorObjects{}
	if err := loadEtcdMonitorObjects(objs, nil); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_load"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "loading eBPF objects failed")
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	// Create eBPF state
	state := &ebpfState{
		objs:  objs,
		links: make([]link.Link, 0),
	}

	// Attach to write syscalls (etcd WAL writes)
	l1, err := link.Tracepoint("syscalls", "sys_enter_write", objs.TraceSysEnterWrite, nil)
	if err != nil {
		objs.Close()
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_attach"),
				attribute.String("syscall", "write"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "attaching write tracepoint failed")
		return fmt.Errorf("attaching write tracepoint: %w", err)
	}
	state.links = append(state.links, l1)
	span.AddEvent("Attached write tracepoint")

	// Attach to fsync syscalls (etcd WAL syncs)
	l2, err := link.Tracepoint("syscalls", "sys_enter_fsync", objs.TraceSysEnterFsync, nil)
	if err != nil {
		state.cleanup()
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_attach"),
				attribute.String("syscall", "fsync"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "attaching fsync tracepoint failed")
		return fmt.Errorf("attaching fsync tracepoint: %w", err)
	}
	state.links = append(state.links, l2)
	span.AddEvent("Attached fsync tracepoint")

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		state.cleanup()
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_ringbuf"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "creating ring buffer reader failed")
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	state.reader = reader
	span.AddEvent("Created ring buffer reader")

	// Store state and start reading events
	c.ebpfState = state
	go c.readEBPFEvents()

	// Record setup duration
	duration := time.Since(start)
	if c.processingTime != nil {
		c.processingTime.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("operation", "ebpf_setup"),
		))
	}

	span.SetAttributes(
		attribute.Float64("setup_duration_seconds", duration.Seconds()),
		attribute.Int("tracepoints_attached", len(state.links)),
	)

	c.logger.Info("eBPF monitoring initialized",
		zap.Duration("setup_duration", duration),
		zap.Int("tracepoints", len(state.links)))

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

// readEBPFEvents reads events from eBPF ring buffer
func (c *Collector) readEBPFEvents() {
	ctx, span := c.tracer.Start(c.ctx, "etcd.read_ebpf_events")
	defer span.End()

	state, ok := c.ebpfState.(*ebpfState)
	if !ok || state == nil {
		span.AddEvent("eBPF state not available")
		return
	}

	var eventsProcessed uint64
	var eventsDropped uint64

	defer func() {
		span.SetAttributes(
			attribute.Int64("events_processed", int64(eventsProcessed)),
			attribute.Int64("events_dropped", int64(eventsDropped)),
		)
		c.logger.Info("eBPF event reader stopped",
			zap.Uint64("events_processed", eventsProcessed),
			zap.Uint64("events_dropped", eventsDropped))
	}()

	for {
		record, err := state.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				span.AddEvent("Ring buffer closed")
				return
			}
			// Record error but continue
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "ebpf_read"),
				))
			}
			span.AddEvent("Ring buffer read error",
				trace.WithAttributes(attribute.String("error", err.Error())))
			continue
		}

		start := time.Now()

		// Parse the raw event
		var event etcdEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "ebpf_parse"),
				))
			}
			continue
		}

		// Track syscall events as part of events processed
		// (syscall tracking is now counted as regular events)

		// Create strongly-typed eBPF event data
		eventData := EBPFEventData{
			Timestamp: event.Timestamp,
			PID:       event.PID,
			TID:       event.TID,
			Type:      event.EventType, // Raw type, no interpretation
			DataLen:   event.DataLen,
		}

		// Add network info if present
		if event.SrcIP != 0 || event.DstIP != 0 {
			eventData.SrcIP = fmt.Sprintf("%d.%d.%d.%d",
				byte(event.SrcIP), byte(event.SrcIP>>8),
				byte(event.SrcIP>>16), byte(event.SrcIP>>24))
			eventData.DstIP = fmt.Sprintf("%d.%d.%d.%d",
				byte(event.DstIP), byte(event.DstIP>>8),
				byte(event.DstIP>>16), byte(event.DstIP>>24))
			eventData.SrcPort = event.SrcPort
			eventData.DstPort = event.DstPort
		}

		// Include raw data if present
		if event.DataLen > 0 && event.DataLen <= 256 {
			eventData.RawData = event.Data[:event.DataLen]
		}

		rawEvent := c.createEventWithContext(ctx, "syscall", eventData)

		select {
		case c.events <- rawEvent:
			eventsProcessed++
			// Record event metric
			if c.eventsProcessed != nil {
				c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
					attribute.String("event_type", "ebpf_syscall"),
					attribute.String("syscall_type", fmt.Sprintf("%d", event.EventType)),
					attribute.Int("pid", int(event.PID)),
				))
			}

		case <-c.ctx.Done():
			span.AddEvent("Context cancelled")
			return
		default:
			// Buffer full, drop event
			eventsDropped++
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "ebpf_buffer_full"),
				))
			}
		}

		// Record processing time if we've processed significant events
		if eventsProcessed%1000 == 0 && eventsProcessed > 0 {
			processingTime := time.Since(start)
			if c.processingTime != nil {
				c.processingTime.Record(ctx, processingTime.Seconds()*1000, metric.WithAttributes(
					attribute.String("operation", "ebpf_event_processing"),
				))
			}
		}
	}
}
