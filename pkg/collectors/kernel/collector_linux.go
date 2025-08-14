//go:build linux
// +build linux

package kernel

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/kernel/bpf"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 kernelMonitor ./bpf_src/kernel_monitor.c -- -I../bpf_common

// eBPF components - Linux-specific
type ebpfState struct {
	objs   *bpf.KernelmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes eBPF monitoring - Linux only
func (c *ModularCollector) startEBPF() error {
	ctx, span := c.tracer.Start(context.Background(), "kernel.ebpf.start",
		trace.WithAttributes(
			attribute.String("collector.name", c.name),
		),
	)
	defer span.End()

	// Check if eBPF is supported
	if !bpf.IsSupported() {
		c.logger.Warn("eBPF not supported on this platform")
		return nil
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to remove memory limit")
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("error_type", "memlock_removal_failed"),
				),
			)
		}
		return fmt.Errorf("removing memory limit: %w", err)
	}

	// Load pre-compiled eBPF programs
	objs := bpf.KernelmonitorObjects{}
	if err := bpf.LoadKernelmonitorObjects(&objs, nil); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to load eBPF objects")
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("error_type", "ebpf_load_failed"),
				),
			)
		}
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	c.ebpfState = &ebpfState{objs: &objs}

	// Attach eBPF programs to tracepoints
	// Monitor process events
	processLink, err := link.Tracepoint("syscalls", "sys_enter_execve", objs.TraceExec, nil)
	if err != nil {
		objs.Close()
		return fmt.Errorf("attaching execve tracepoint: %w", err)
	}

	// Monitor file operations
	fileLink, err := link.Tracepoint("syscalls", "sys_enter_openat", objs.TraceOpenat, nil)
	if err != nil {
		processLink.Close()
		objs.Close()
		return fmt.Errorf("attaching openat tracepoint: %w", err)
	}

	c.ebpfState.(*ebpfState).reader, err = ringbuf.NewReader(objs.Events)
	if err != nil {
		processLink.Close()
		fileLink.Close()
		objs.Close()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}

	c.ebpfState.(*ebpfState).links = []link.Link{processLink, fileLink}

	span.SetAttributes(
		attribute.Int("link_count", len(c.ebpfState.(*ebpfState).links)),
	)

	c.logger.Info("eBPF monitoring started successfully",
		zap.String("collector", c.name),
		zap.Int("links", len(c.ebpfState.(*ebpfState).links)),
	)

	return nil
}

// stopEBPF cleans up eBPF resources - Linux only
func (c *ModularCollector) stopEBPF() {
	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState.(*ebpfState)

	// Close reader
	if state.reader != nil {
		state.reader.Close()
	}

	// Close all links
	for _, link := range state.links {
		if err := link.Close(); err != nil {
			c.logger.Error("Failed to close eBPF link", zap.Error(err))
		}
	}

	// Close eBPF objects
	if state.objs != nil {
		state.objs.Close()
	}

	c.logger.Info("eBPF monitoring stopped", zap.String("collector", c.name))
}

// readEBPFEvents processes eBPF ring buffer events - Linux only
func (c *ModularCollector) readEBPFEvents() {
	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState.(*ebpfState)
	if state.reader == nil {
		return
	}

	ctx := c.ctx
	for {
		select {
		case <-ctx.Done():
			return
		default:
			record, err := state.reader.Read()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1,
						metric.WithAttributes(
							attribute.String("error_type", "ringbuf_read_failed"),
						),
					)
				}
				c.logger.Error("Failed to read from ring buffer", zap.Error(err))
				continue
			}

			// Parse the event
			if len(record.RawSample) < int(unsafe.Sizeof(KernelEvent{})) {
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1,
						metric.WithAttributes(
							attribute.String("error_type", "invalid_event_size"),
						),
					)
				}
				continue
			}

			var event KernelEvent
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1,
						metric.WithAttributes(
							attribute.String("error_type", "event_parse_failed"),
						),
					)
				}
				c.logger.Error("Failed to parse kernel event", zap.Error(err))
				continue
			}

			// Convert to unified event
			unifiedEvent := c.convertToUnifiedEvent(event)

			// Send to event channel
			select {
			case c.events <- unifiedEvent:
				if c.eventsProcessed != nil {
					c.eventsProcessed.Add(ctx, 1,
						metric.WithAttributes(
							attribute.String("event_type", unifiedEvent.Type),
						),
					)
				}
			case <-ctx.Done():
				return
			default:
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1,
						metric.WithAttributes(
							attribute.String("error_type", "channel_full"),
						),
					)
				}
			}
		}
	}
}

// convertToUnifiedEvent converts eBPF event to unified event format - Linux only
func (c *ModularCollector) convertToUnifiedEvent(event KernelEvent) collectors.RawEvent {
	// Convert timestamp to time.Time format expected by RawEvent
	timestamp := time.Unix(0, int64(event.Timestamp))
	
	// Create metadata map with proper string conversion
	metadata := map[string]string{
		"collector":  c.name,
		"pid":        fmt.Sprintf("%d", event.PID),
		"tid":        fmt.Sprintf("%d", event.TID),
		"event_type": fmt.Sprintf("%d", event.EventType),
		"size":       fmt.Sprintf("%d", event.Size),
		"cgroup_id":  fmt.Sprintf("%d", event.CgroupID),
	}
	
	// Convert comm from byte array to string
	comm := ""
	for i, b := range event.Comm {
		if b == 0 {
			comm = string(event.Comm[:i])
			break
		}
	}
	if comm != "" {
		metadata["comm"] = comm
	}
	
	// Convert pod UID from byte array to string
	podUID := ""
	for i, b := range event.PodUID {
		if b == 0 {
			podUID = string(event.PodUID[:i])
			break
		}
	}
	if podUID != "" {
		metadata["pod_uid"] = podUID
	}
	
	// Determine event type
	eventType := "kernel_event"
	switch event.EventType {
	case 1:
		eventType = "process_exec"
	case 2:
		eventType = "file_open"
	case 3:
		eventType = "network_connect"
	}
	
	return collectors.RawEvent{
		Timestamp: timestamp,
		Type:      eventType,
		Data:      nil, // Raw event data would go here if needed
		Metadata:  metadata,
		TraceID:   collectors.GenerateTraceID(),
		SpanID:    collectors.GenerateSpanID(),
	}
}