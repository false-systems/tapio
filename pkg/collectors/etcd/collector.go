package etcd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// Collector implements minimal etcd collection
type Collector struct {
	config collectors.CollectorConfig
	events chan collectors.RawEvent

	// Add source-specific fields
	// e.g., client, watcher, reader

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.RWMutex
	healthy bool
}

// NewCollector creates a new etcd collector
func NewCollector(config collectors.CollectorConfig) (collectors.Collector, error) {
	// Try eBPF collector first (preferred)
	ebpfCollector, err := NewEBPFCollector(config)
	if err == nil {
		return ebpfCollector, nil
	}

	// Fallback to basic collector
	return &Collector{
		config:  config,
		events:  make(chan collectors.RawEvent, config.BufferSize),
		healthy: true,
	}, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return "etcd"
}

// Start begins collection
func (c *Collector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		return fmt.Errorf("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Initialize source connection
	// Start collection goroutine
	c.wg.Add(1)
	go c.collect()

	return nil
}

// Stop gracefully shuts down
func (c *Collector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	c.wg.Wait()
	close(c.events)

	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// collect is the main collection loop
func (c *Collector) collect() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return

		default:
			// Read from source
			data := c.readFromSource()

			// Create raw event
			event := collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "etcd",
				Data:      data, // Raw bytes
				Metadata:  c.createMetadata(),
			}

			// Send event
			select {
			case c.events <- event:
				// Sent
			case <-c.ctx.Done():
				return
			default:
				// Buffer full, drop
			}
		}
	}
}

// readFromSource reads raw data from etcd
func (c *Collector) readFromSource() []byte {
	// TODO: Implement etcd reading
	// This is where we'll implement eBPF-based etcd observation
	return []byte{}
}

// createMetadata creates event metadata
func (c *Collector) createMetadata() map[string]string {
	return map[string]string{
		"collector": "etcd",
		"version":   "1.0.0",
	}
}
