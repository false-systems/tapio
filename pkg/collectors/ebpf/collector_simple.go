package ebpf

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

	mu      sync.RWMutex
	started bool
	healthy bool
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

	c.started = true

	// Start event reader
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

	c.cancel()

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

	// Wait for goroutines
	c.wg.Wait()

	close(c.events)
	c.started = false
	c.healthy = false

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

	return nil
}

// readEvents reads events from the perf buffer
func (c *SimpleCollector) readEvents() {
	defer c.wg.Done()

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
				// Log error but continue
				continue
			}

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
			}
		}
	}
}

// determineEventType tries to determine the event type from raw data
// This is minimal metadata extraction - no business logic
func (c *SimpleCollector) determineEventType(data []byte) string {
	// Our memory_event struct has event_type at offset 32
	// struct memory_event {
	//     __u64 timestamp;      // 0-7
	//     __u32 pid;           // 8-11
	//     __u32 tid;           // 12-15
	//     __u64 addr;          // 16-23
	//     __u64 size;          // 24-31
	//     __u8 event_type;     // 32
	//     ...
	// }

	if len(data) > 32 {
		eventType := data[32]
		switch eventType {
		case 0:
			return "memory_alloc"
		case 1:
			return "memory_free"
		case 2:
			return "oom_kill"
		}
	}

	return "unknown"
}

// DefaultSimpleConfig returns a default configuration
func DefaultSimpleConfig() collectors.CollectorConfig {
	config := collectors.DefaultCollectorConfig()
	config.Labels["collector"] = "ebpf"
	return config
}

