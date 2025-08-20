//go:build linux
// +build linux

package kernel

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/kernel/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 kernelMonitor ./bpf_src/kernel_monitor.c -- -I../bpf_common

// eBPF components - Linux-specific
type ebpfComponents struct {
	objs   *bpf.KernelmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes eBPF monitoring - Linux only
func (c *Collector) startEBPF() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.logger.Warn("Failed to remove memory limit", zap.Error(err))
	}

	// Load pre-compiled eBPF programs
	objs := &bpf.KernelmonitorObjects{}
	if err := bpf.LoadKernelmonitorObjects(objs, nil); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_load_failed"),
			))
		}
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	state := &ebpfComponents{
		objs:  objs,
		links: make([]link.Link, 0),
	}

	// Attach to process events
	processLink, err := link.Tracepoint("syscalls", "sys_enter_execve", objs.TraceExec, nil)
	if err != nil {
		objs.Close()
		return fmt.Errorf("attaching execve tracepoint: %w", err)
	}
	state.links = append(state.links, processLink)

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		processLink.Close()
		objs.Close()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	state.reader = reader

	c.ebpfState = state
	c.logger.Info("eBPF programs loaded and attached successfully")
	return nil
}

// stopEBPF stops eBPF monitoring - Linux only
func (c *Collector) stopEBPF() {
	if c.ebpfState == nil {
		return
	}

	state, ok := c.ebpfState.(*ebpfComponents)
	if !ok {
		return
	}

	// Close reader first
	if state.reader != nil {
		state.reader.Close()
	}

	// Close all links
	for _, l := range state.links {
		if l != nil {
			l.Close()
		}
	}

	// Close eBPF objects
	if state.objs != nil {
		state.objs.Close()
	}

	c.ebpfState = nil
	c.logger.Info("eBPF programs stopped")
}

// processEvents reads and processes eBPF events
func (c *Collector) processEvents() {
	if c.ebpfState == nil {
		return
	}

	state, ok := c.ebpfState.(*ebpfComponents)
	if !ok || state.reader == nil {
		return
	}

	c.logger.Info("Starting eBPF event processing")

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Stopping eBPF event processing")
			return
		default:
			record, err := state.reader.Read()
			if err != nil {
				if ringbuf.IsClosed(err) {
					return
				}
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
						attribute.String("error_type", "ring_buffer_read"),
					))
				}
				c.logger.Error("Failed to read from ring buffer", zap.Error(err))
				continue
			}

			// Process the raw event
			c.processRawEvent(record.RawSample)
		}
	}
}

// processRawEvent processes a single raw eBPF event
func (c *Collector) processRawEvent(data []byte) {
	start := time.Now()

	// Parse kernel event based on size
	if len(data) < int(unsafe.Sizeof(KernelEvent{})) {
		if c.droppedEvents != nil {
			c.droppedEvents.Add(c.ctx, 1, metric.WithAttributes(
				attribute.String("reason", "invalid_size"),
			))
		}
		return
	}

	// Convert raw bytes to KernelEvent
	event := (*KernelEvent)(unsafe.Pointer(&data[0]))

	// Create domain event
	domainEvent := domain.RawEvent{
		Timestamp:   time.Unix(0, int64(event.Timestamp)),
		CollectorID: c.name,
		Type:        getEventType(event.EventType),
		Data: KernelEventData{
			PID:       event.PID,
			PPID:      event.PPID,
			UID:       event.UID,
			GID:       event.GID,
			CgroupID:  event.CgroupID,
			EventType: event.EventType,
			Comm:      bytesToString(event.Comm[:]),
		},
	}

	// Send event to channel
	select {
	case c.events <- domainEvent:
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(c.ctx, 1, metric.WithAttributes(
				attribute.String("event_type", domainEvent.Type),
			))
		}
	default:
		// Channel full, drop event
		if c.droppedEvents != nil {
			c.droppedEvents.Add(c.ctx, 1, metric.WithAttributes(
				attribute.String("reason", "channel_full"),
			))
		}
	}

	// Record processing time
	if c.processingTime != nil {
		duration := time.Since(start).Milliseconds()
		c.processingTime.Record(c.ctx, float64(duration), metric.WithAttributes(
			attribute.String("event_type", domainEvent.Type),
		))
	}

	// Update buffer usage
	if c.bufferUsage != nil {
		usage := int64(len(c.events))
		c.bufferUsage.Record(c.ctx, usage, metric.WithAttributes(
			attribute.String("collector", c.name),
		))
	}
}

// getEventType converts numeric event type to string
func getEventType(eventType uint8) string {
	switch eventType {
	case 1:
		return "process_exec"
	case 2:
		return "process_exit"
	case 3:
		return "file_open"
	case 4:
		return "network_connect"
	default:
		return fmt.Sprintf("unknown_%d", eventType)
	}
}

// bytesToString converts byte array to string
func bytesToString(data []byte) string {
	n := bytes.IndexByte(data, 0)
	if n == -1 {
		n = len(data)
	}
	return string(data[:n])
}

// parseKernelEvent safely parses kernel event from buffer
func parseKernelEvent(buffer []byte) (*KernelEvent, error) {
	if len(buffer) < int(unsafe.Sizeof(KernelEvent{})) {
		return nil, fmt.Errorf("buffer too small: %d bytes", len(buffer))
	}

	var event KernelEvent
	reader := bytes.NewReader(buffer)
	if err := binary.Read(reader, binary.LittleEndian, &event); err != nil {
		return nil, fmt.Errorf("failed to parse event: %w", err)
	}

	return &event, nil
}
