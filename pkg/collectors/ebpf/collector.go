//go:build linux
// +build linux

package ebpf

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/yairfalse/tapio/pkg/collectors"
)

// SimpleCollector is a minimal eBPF collector that only collects raw data
type SimpleCollector struct {
	config     collectors.CollectorConfig
	events     chan collectors.RawEvent
	collection *ebpf.Collection
	links      []link.Link
	reader     *perf.Reader

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu            sync.RWMutex
	started       bool
	healthy       bool
	errorCount    uint64
	droppedEvents uint64
}

// NewSimpleCollector creates a new minimal eBPF collector
func NewSimpleCollector(config collectors.CollectorConfig) collectors.Collector {
	return &SimpleCollector{
		config:  config,
		events:  make(chan collectors.RawEvent, config.BufferSize),
		healthy: true,
		links:   make([]link.Link, 0),
	}
}

// Name returns the collector name
func (c *SimpleCollector) Name() string {
	return "ebpf"
}

// Start begins collection
func (c *SimpleCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return nil
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Load eBPF programs
	spec, err := loadMemorytracker()
	if err != nil {
		return fmt.Errorf("failed to load eBPF spec: %w", err)
	}

	c.collection, err = ebpf.NewCollection(spec)
	if err != nil {
		return fmt.Errorf("failed to create eBPF collection: %w", err)
	}

	// Get the events map
	eventsMap, ok := c.collection.Maps["events"]
	if !ok {
		c.collection.Close()
		return errors.New("events map not found")
	}

	// Create perf reader
	c.reader, err = perf.NewReader(eventsMap, 4096)
	if err != nil {
		c.collection.Close()
		return fmt.Errorf("failed to create perf reader: %w", err)
	}

	// Attach programs
	if err := c.attachPrograms(); err != nil {
		c.reader.Close()
		c.collection.Close()
		return fmt.Errorf("failed to attach programs: %w", err)
	}

	// Only mark as started after all resources are initialized
	c.started = true

	// Start event reader goroutine only after everything is set up
	c.wg.Add(1)
	go c.readEvents()

	return nil
}

// Stop gracefully shuts down the collector
func (c *SimpleCollector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return nil
	}

	// Signal shutdown
	c.cancel()

	// Mark as unhealthy immediately
	c.healthy = false

	// Close links
	for _, l := range c.links {
		l.Close()
	}

	// Close reader
	if c.reader != nil {
		c.reader.Close()
	}

	// Close collection
	if c.collection != nil {
		c.collection.Close()
	}

	// Wait for goroutines to finish
	c.wg.Wait()

	// Close events channel only after goroutines are done
	close(c.events)
	c.started = false

	return nil
}

// Events returns the event channel
func (c *SimpleCollector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns true if the collector is functioning
func (c *SimpleCollector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// attachPrograms attaches the eBPF programs to their hooks
func (c *SimpleCollector) attachPrograms() error {
	// Attach memory allocation tracepoint
	if prog, ok := c.collection.Programs["TraceMmPageAlloc"]; ok {
		l, err := link.Tracepoint("kmem", "mm_page_alloc", prog, nil)
		if err != nil {
			return fmt.Errorf("failed to attach mm_page_alloc: %w", err)
		}
		c.links = append(c.links, l)
	}

	// Attach memory free tracepoint
	if prog, ok := c.collection.Programs["TraceMmPageFree"]; ok {
		l, err := link.Tracepoint("kmem", "mm_page_free", prog, nil)
		if err != nil {
			return fmt.Errorf("failed to attach mm_page_free: %w", err)
		}
		c.links = append(c.links, l)
	}

	// Attach OOM kill tracepoint
	if prog, ok := c.collection.Programs["TraceOomKillProcess"]; ok {
		l, err := link.Tracepoint("oom", "oom_kill_process", prog, nil)
		if err != nil {
			// OOM tracepoint might not exist on all kernels
			// Log but don't fail
		} else {
			c.links = append(c.links, l)
		}
	}

	// Attach K8s-specific programs
	if err := c.attachK8sPrograms(); err != nil {
		// K8s programs are optional, log but don't fail
		// In production, would log this error
	}

	return nil
}

// attachK8sPrograms attaches K8s-specific eBPF programs
func (c *SimpleCollector) attachK8sPrograms() error {
	// Attach container creation tracking
	if prog, ok := c.collection.Programs["TraceContainerCreate"]; ok {
		l, err := link.RawTracepoint("sys_enter", prog, nil)
		if err != nil {
			return fmt.Errorf("failed to attach container create: %w", err)
		}
		c.links = append(c.links, l)
	}

	// Attach cgroup tracking
	if prog, ok := c.collection.Programs["TraceCgroupMkdir"]; ok {
		l, err := link.Kprobe("cgroup_mkdir", prog, nil)
		if err != nil {
			// Might not exist on all kernels
		} else {
			c.links = append(c.links, l)
		}
	}

	if prog, ok := c.collection.Programs["TraceCgroupRmdir"]; ok {
		l, err := link.Kprobe("cgroup_rmdir", prog, nil)
		if err != nil {
			// Might not exist on all kernels
		} else {
			c.links = append(c.links, l)
		}
	}

	// Attach exec in container tracking
	if prog, ok := c.collection.Programs["TraceExecInContainer"]; ok {
		l, err := link.Tracepoint("syscalls", "sys_enter_execve", prog, nil)
		if err != nil {
			// Optional feature
		} else {
			c.links = append(c.links, l)
		}
	}

	// Attach network namespace tracking
	if prog, ok := c.collection.Programs["TraceNetnsCreate"]; ok {
		l, err := link.Kprobe("create_new_namespaces", prog, nil)
		if err != nil {
			// Optional feature
		} else {
			c.links = append(c.links, l)
		}
	}

	return nil
}

// readEvents reads events from the perf buffer
func (c *SimpleCollector) readEvents() {
	defer c.wg.Done()

	const maxConsecutiveErrors = 10
	consecutiveErrors := 0

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			record, err := c.reader.Read()
			if err != nil {
				if errors.Is(err, perf.ErrClosed) {
					return
				}
				// Track errors
				atomic.AddUint64(&c.errorCount, 1)
				consecutiveErrors++

				// If too many consecutive errors, mark unhealthy
				if consecutiveErrors >= maxConsecutiveErrors {
					c.mu.Lock()
					c.healthy = false
					c.mu.Unlock()
				}
				continue
			}

			// Reset consecutive error counter on success
			consecutiveErrors = 0

			// Create raw event with just the bytes
			event := collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "ebpf",
				Data:      record.RawSample,
				Metadata: map[string]string{
					"cpu":  fmt.Sprintf("%d", record.CPU),
					"size": fmt.Sprintf("%d", len(record.RawSample)),
				},
			}

			// Add minimal metadata about event type if we can determine it
			if len(record.RawSample) >= 8 {
				// First 8 bytes should be timestamp in our struct
				eventType := c.determineEventType(record.RawSample)
				if eventType != "" {
					event.Metadata["event_type"] = eventType
				}
			}

			select {
			case c.events <- event:
			case <-c.ctx.Done():
				return
			default:
				// Buffer full, track dropped event
				atomic.AddUint64(&c.droppedEvents, 1)
			}
		}
	}
}

// determineEventType tries to determine the event type from raw data
// This is minimal metadata extraction - no business logic
func (c *SimpleCollector) determineEventType(data []byte) string {
	// Unified event structure from unified.c:
	// struct event {
	//     __u64 timestamp;  // 0-7
	//     __u32 pid;       // 8-11
	//     __u32 tid;       // 12-15
	//     __u32 cpu;       // 16-19
	//     __u8  type;      // 20
	//     __u8  flags;     // 21
	//     __u16 data_len;  // 22-23
	//     __u8  data[64];  // 24+
	// };

	if data == nil || len(data) < 21 {
		return "unknown"
	}

	// Event type is at offset 20 in unified structure
	eventType := data[20]
	switch eventType {
	case 1:
		return "network"
	case 2:
		return "syscall"
	case 3:
		return "memory"
	case 4:
		return "oom"
	// K8s event types
	case 10:
		return "k8s_container_create"
	case 11:
		return "k8s_container_delete"
	case 12:
		return "k8s_cgroup_create"
	case 13:
		return "k8s_cgroup_delete"
	case 14:
		return "k8s_exec_in_pod"
	default:
		return "unknown"
	}
}

// Stats returns collector statistics
func (c *SimpleCollector) Stats() map[string]uint64 {
	return map[string]uint64{
		"errors":         atomic.LoadUint64(&c.errorCount),
		"dropped_events": atomic.LoadUint64(&c.droppedEvents),
	}
}

// DefaultSimpleConfig returns a default configuration
func DefaultSimpleConfig() collectors.CollectorConfig {
	config := collectors.DefaultCollectorConfig()
	config.Labels["collector"] = "ebpf"
	return config
}
