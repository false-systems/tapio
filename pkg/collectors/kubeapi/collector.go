package kubeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// Collector implements minimal KubeAPI monitoring
type Collector struct {
	name    string
	events  chan collectors.RawEvent
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
}

// NewCollector creates a new minimal KubeAPI collector
func NewCollector(name string) (*Collector, error) {
	return &Collector{
		name:    name,
		events:  make(chan collectors.RawEvent, 1000),
		healthy: true,
	}, nil
}

// NewCollectorFromCollectorConfig creates a collector from CollectorConfig
func NewCollectorFromCollectorConfig(config collectors.CollectorConfig) (*Collector, error) {
	name := "kubeapi"
	if config.Labels != nil {
		if n, ok := config.Labels["name"]; ok {
			name = n
		}
	}
	return NewCollector(name)
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

	// Start K8s API monitoring if available
	if err := c.startK8sWatch(); err != nil {
		// Log but don't fail - K8s API watching is optional
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

	// Stop K8s watching if running
	c.stopK8sWatch()

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

// Helper to create a KubeAPI raw event
func (c *Collector) createEvent(eventType string, data interface{}, traceID, spanID string) collectors.RawEvent {
	jsonData, _ := json.Marshal(data)

	// Generate new span ID if not provided
	if spanID == "" {
		spanID = collectors.GenerateSpanID()
	}

	// Generate new trace ID if not provided
	if traceID == "" {
		traceID = collectors.GenerateTraceID()
	}

	return collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubeapi",
		Data:      jsonData,
		Metadata: map[string]string{
			"collector": c.name,
			"event":     eventType,
		},
		TraceID: traceID,
		SpanID:  spanID,
	}
}
