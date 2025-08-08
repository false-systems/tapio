package process

import (
	"context"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
)

// ProcessEvent represents a process event from eBPF
type ProcessEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	EventType uint32
	Size      uint64
	Comm      [16]byte
	CgroupID  uint64
	PodUID    [36]byte
	Data      [200]byte
}

// Collector implements process monitoring
type Collector struct {
	logger *zap.Logger
	events chan collectors.RawEvent
	ctx    context.Context
	cancel context.CancelFunc
	reader *ringbuf.Reader
	links  []link.Link
}

// NewProcessCollector creates a new process collector
func NewProcessCollector(logger *zap.Logger) *Collector {
	return &Collector{
		logger: logger,
		events: make(chan collectors.RawEvent, 2000),
	}
}

// Start starts process monitoring
func (c *Collector) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Load process eBPF programs
	// Note: Process monitor eBPF objects need to be generated first
	c.logger.Info("loading process eBPF programs")

	// For now, skip eBPF loading until compilation issues are resolved
	// The programs will be loaded once the C compilation is fixed

	c.logger.Info("Process collector started")

	// Start event processing
	go c.processEvents()

	return nil
}

// Stop stops process monitoring
func (c *Collector) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	for _, l := range c.links {
		l.Close()
	}

	if c.reader != nil {
		c.reader.Close()
	}

	if c.events != nil {
		close(c.events)
	}

	c.logger.Info("Process collector stopped")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// processEvents processes process events
func (c *Collector) processEvents() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		if c.reader == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		record, err := c.reader.Read()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			continue
		}

		// Parse process event
		event, err := c.parseProcessEvent(record.RawSample)
		if err != nil {
			c.logger.Error("Failed to parse process event", zap.Error(err))
			continue
		}

		// Convert to RawEvent
		metadata := map[string]string{
			"collector": "kernel-process",
			"pid":       fmt.Sprintf("%d", event.PID),
			"tid":       fmt.Sprintf("%d", event.TID),
			"comm":      c.nullTerminatedString(event.Comm[:]),
			"size":      fmt.Sprintf("%d", event.Size),
			"cgroup_id": fmt.Sprintf("%d", event.CgroupID),
			"pod_uid":   c.nullTerminatedString(event.PodUID[:]),
		}

		rawEvent := collectors.RawEvent{
			Timestamp: time.Unix(0, int64(event.Timestamp)),
			Type:      c.eventTypeToString(event.EventType),
			Data:      record.RawSample,
			Metadata:  metadata,
			TraceID:   collectors.GenerateTraceID(),
			SpanID:    collectors.GenerateSpanID(),
		}

		select {
		case c.events <- rawEvent:
		case <-c.ctx.Done():
			return
		default:
			// Drop event if buffer full
		}
	}
}

// parseProcessEvent parses a ProcessEvent from raw bytes
func (c *Collector) parseProcessEvent(rawBytes []byte) (*ProcessEvent, error) {
	expectedSize := int(unsafe.Sizeof(ProcessEvent{}))

	if len(rawBytes) < expectedSize {
		return nil, fmt.Errorf("buffer too small: got %d bytes, expected at least %d", len(rawBytes), expectedSize)
	}

	event := *(*ProcessEvent)(unsafe.Pointer(&rawBytes[0]))

	if event.EventType > 20 { // Basic sanity check
		return nil, fmt.Errorf("invalid process event type: %d", event.EventType)
	}

	return &event, nil
}

// eventTypeToString converts process event type to string
func (c *Collector) eventTypeToString(eventType uint32) string {
	switch eventType {
	case 1:
		return "memory_alloc"
	case 2:
		return "memory_free"
	case 3:
		return "process_exec"
	case 17:
		return "process_exit"
	case 18:
		return "process_fork"
	default:
		return "process_unknown"
	}
}

// nullTerminatedString converts null-terminated byte array to string
func (c *Collector) nullTerminatedString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
