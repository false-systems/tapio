package cni

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// Collector implements minimal CNI monitoring
type Collector struct {
	name      string
	events    chan collectors.RawEvent
	ctx       context.Context
	cancel    context.CancelFunc
	healthy   bool
	ebpfState interface{} // Platform-specific eBPF state
}

// NewCollector creates a new minimal CNI collector
func NewCollector(name string) (*Collector, error) {
	return &Collector{
		name:    name,
		events:  make(chan collectors.RawEvent, 1000),
		healthy: true,
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins collection
func (c *Collector) Start(ctx context.Context) error {
	if c.ctx != nil {
		return fmt.Errorf("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start eBPF monitoring if available
	if err := c.startEBPF(); err != nil {
		// Log but don't fail - eBPF is optional
		// In minimal collector, we just collect what we can
	}

	return nil
}

// Stop gracefully shuts down
func (c *Collector) Stop() error {
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	// Stop eBPF if running
	c.stopEBPF()

	// Close events channel
	if c.events != nil {
		close(c.events)
	}

	c.healthy = false
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return c.healthy
}

// Helper to create a CNI raw event
func (c *Collector) createEvent(eventType string, data interface{}) collectors.RawEvent {
	jsonData, _ := json.Marshal(data)

	return collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "cni",
		Data:      jsonData,
		Metadata: map[string]string{
			"collector": c.name,
			"event":     eventType,
		},
		// Generate new trace ID for CNI events
		TraceID: collectors.GenerateTraceID(),
		SpanID:  collectors.GenerateSpanID(),
	}
}
