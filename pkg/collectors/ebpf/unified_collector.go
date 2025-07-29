package ebpf

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
	"github.com/yairfalse/tapio/pkg/collectors"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -I./bpf -I./bpf/headers" -target amd64,arm64 -type event unified ./bpf/unified.c -- -I./bpf -I./bpf/headers

// Event types from BPF program
const (
	EventNetwork = 1
	EventSyscall = 2
	EventMemory  = 3
	EventOOM     = 4
)

// UnifiedCollector uses a single CO-RE eBPF program for all collection
type UnifiedCollector struct {
	config  collectors.CollectorConfig
	events  chan collectors.RawEvent
	objs    unifiedObjects
	links   []link.Link
	reader  *ringbuf.Reader
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	healthy bool
	mu      sync.RWMutex
	metrics CollectorMetrics
}

// CollectorMetrics tracks performance
type CollectorMetrics struct {
	EventsReceived uint64
	EventsDropped  uint64
	BytesProcessed uint64
}

// NewUnifiedCollector creates a new unified eBPF collector with CO-RE
func NewUnifiedCollector(config collectors.CollectorConfig) (*UnifiedCollector, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("failed to remove memlock: %w", err)
	}

	return &UnifiedCollector{
		config:  config,
		events:  make(chan collectors.RawEvent, config.BufferSize),
		healthy: true,
	}, nil
}

// Name returns the collector name
func (c *UnifiedCollector) Name() string {
	return "ebpf-unified"
}

// Start begins collection with the unified eBPF program
func (c *UnifiedCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		return errors.New("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Load eBPF objects
	if err := loadUnifiedObjects(&c.objs, nil); err != nil {
		return fmt.Errorf("loading objects: %w", err)
	}

	// Attach programs
	if err := c.attachPrograms(); err != nil {
		c.objs.Close()
		return fmt.Errorf("attaching programs: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(c.objs.Events)
	if err != nil {
		c.cleanup()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	c.reader = reader

	// Start event processing
	c.wg.Add(1)
	go c.processEvents()

	// Start health monitor
	c.wg.Add(1)
	go c.healthMonitor()

	return nil
}

// Stop gracefully shuts down the collector
func (c *UnifiedCollector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	// Wait for goroutines
	c.wg.Wait()

	// Cleanup
	c.cleanup()

	close(c.events)
	return nil
}

// Events returns the event channel
func (c *UnifiedCollector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns true if the collector is functioning properly
func (c *UnifiedCollector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// GetMetrics returns collector metrics
func (c *UnifiedCollector) GetMetrics() CollectorMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metrics
}

// attachPrograms attaches all BPF programs
func (c *UnifiedCollector) attachPrograms() error {
	// Attach memory tracking
	kmalloc, err := link.AttachTracepoint(link.TracepointOptions{
		Group:   "kmem",
		Name:    "kmalloc",
		Program: c.objs.TraceKmalloc,
	})
	if err != nil {
		return fmt.Errorf("attaching kmalloc: %w", err)
	}
	c.links = append(c.links, kmalloc)

	kfree, err := link.AttachTracepoint(link.TracepointOptions{
		Group:   "kmem",
		Name:    "kfree",
		Program: c.objs.TraceKfree,
	})
	if err != nil {
		return fmt.Errorf("attaching kfree: %w", err)
	}
	c.links = append(c.links, kfree)

	// Attach OOM tracking
	oom, err := link.AttachTracepoint(link.TracepointOptions{
		Group:   "oom",
		Name:    "oom_score_adj_update",
		Program: c.objs.TraceOom,
	})
	if err != nil {
		// OOM tracking might not be available on all kernels
		// Log but don't fail
	} else {
		c.links = append(c.links, oom)
	}

	// Attach network tracking
	tcp, err := link.AttachTracing(link.TracingOptions{
		Program: c.objs.TraceTcpV4Connect,
	})
	if err != nil {
		// Network tracking requires newer kernels
		// Log but don't fail
	} else {
		c.links = append(c.links, tcp)
	}

	// Attach process execution tracking
	exec, err := link.AttachTracepoint(link.TracepointOptions{
		Group:   "sched",
		Name:    "sched_process_exec",
		Program: c.objs.TraceExec,
	})
	if err != nil {
		// Process tracking might fail in some environments
		// Log but don't fail
	} else {
		c.links = append(c.links, exec)
	}

	return nil
}

// processEvents reads events from the ring buffer
func (c *UnifiedCollector) processEvents() {
	defer c.wg.Done()

	for {
		record, err := c.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			// Log error but continue
			continue
		}

		// Parse event
		if len(record.RawSample) < 24 { // Minimum event size
			continue
		}

		event := parseUnifiedEvent(record.RawSample)
		if event == nil {
			continue
		}

		// Convert to RawEvent
		raw := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "ebpf",
			Data:      record.RawSample,
			Metadata:  c.createMetadata(event),
		}

		// Send event
		select {
		case c.events <- raw:
			c.updateMetrics(uint64(len(record.RawSample)))
		case <-c.ctx.Done():
			return
		default:
			// Buffer full, drop event
			c.mu.Lock()
			c.metrics.EventsDropped++
			c.mu.Unlock()
		}
	}
}

// parseUnifiedEvent parses the raw event data
func parseUnifiedEvent(data []byte) *unifiedEvent {
	if len(data) < 24 {
		return nil
	}

	event := &unifiedEvent{}
	buf := bytes.NewReader(data)

	// Read fixed fields
	binary.Read(buf, binary.LittleEndian, &event.Timestamp)
	binary.Read(buf, binary.LittleEndian, &event.Pid)
	binary.Read(buf, binary.LittleEndian, &event.Tid)
	binary.Read(buf, binary.LittleEndian, &event.Cpu)
	binary.Read(buf, binary.LittleEndian, &event.Type)
	binary.Read(buf, binary.LittleEndian, &event.Flags)
	binary.Read(buf, binary.LittleEndian, &event.DataLen)

	// Read variable data
	if event.DataLen > 0 && len(data) >= 24+int(event.DataLen) {
		event.Data = data[24 : 24+event.DataLen]
	}

	return event
}

// createMetadata creates metadata for the event
func (c *UnifiedCollector) createMetadata(event *unifiedEvent) map[string]string {
	metadata := make(map[string]string)

	metadata["cpu"] = fmt.Sprintf("%d", event.Cpu)
	metadata["pid"] = fmt.Sprintf("%d", event.Pid)
	metadata["tid"] = fmt.Sprintf("%d", event.Tid)

	switch event.Type {
	case EventNetwork:
		metadata["event_type"] = "network"
	case EventSyscall:
		metadata["event_type"] = "syscall"
	case EventMemory:
		metadata["event_type"] = "memory"
		if event.Flags == 0 {
			metadata["operation"] = "alloc"
		} else {
			metadata["operation"] = "free"
		}
	case EventOOM:
		metadata["event_type"] = "oom"
	default:
		metadata["event_type"] = "unknown"
	}

	// Add collector labels
	for k, v := range c.config.Labels {
		metadata[k] = v
	}

	return metadata
}

// updateMetrics updates collector metrics
func (c *UnifiedCollector) updateMetrics(bytes uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.metrics.EventsReceived++
	c.metrics.BytesProcessed += bytes
}

// healthMonitor monitors collector health
func (c *UnifiedCollector) healthMonitor() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	lastEvents := uint64(0)

	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			currentEvents := c.metrics.EventsReceived

			// Check if we're receiving events
			if currentEvents == lastEvents {
				c.healthy = false
			} else {
				c.healthy = true
			}

			lastEvents = currentEvents
			c.mu.Unlock()

		case <-c.ctx.Done():
			return
		}
	}
}

// cleanup releases all resources
func (c *UnifiedCollector) cleanup() {
	// Close links
	for _, l := range c.links {
		if l != nil {
			l.Close()
		}
	}
	c.links = nil

	// Close reader
	if c.reader != nil {
		c.reader.Close()
		c.reader = nil
	}

	// Close objects
	c.objs.Close()
}

// unifiedEvent matches the C structure
type unifiedEvent struct {
	Timestamp uint64
	Pid       uint32
	Tid       uint32
	Cpu       uint32
	Type      uint8
	Flags     uint8
	DataLen   uint16
	Data      []byte
}
