package template

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// Collector implements the standard collector interface
// This is the TEMPLATE all collectors must follow
type Collector struct {
	name   string
	config Config
	events chan collectors.RawEvent
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex
	stats  Stats
}

// Stats tracks collector metrics
type Stats struct {
	EventsGenerated uint64
	EventsDropped   uint64
	Errors          uint64
	LastEventTime   time.Time
}

// New creates a new collector instance
func New(name string, config Config) (*Collector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Collector{
		name:   name,
		config: config,
		events: make(chan collectors.RawEvent, config.BufferSize),
	}, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins event collection
func (c *Collector) Start(ctx context.Context) error {
	if c.ctx != nil {
		return fmt.Errorf("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start worker goroutines
	for i := 0; i < c.config.Workers; i++ {
		c.wg.Add(1)
		go c.worker()
	}

	return nil
}

// Stop gracefully shuts down the collector
func (c *Collector) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	// Wait for workers to finish
	c.wg.Wait()

	// Close events channel
	close(c.events)

	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns the health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Healthy if we've seen events in the last minute
	return time.Since(c.stats.LastEventTime) < time.Minute
}

// GetStats returns collector statistics
func (c *Collector) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"events_generated": c.stats.EventsGenerated,
		"events_dropped":   c.stats.EventsDropped,
		"errors":           c.stats.Errors,
		"last_event_time":  c.stats.LastEventTime,
	}
}

// worker processes events
func (c *Collector) worker() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.collectEvents()
		}
	}
}

// collectEvents gathers events from the source
func (c *Collector) collectEvents() {
	// This is where specific collectors implement their logic
	// For template, we generate sample events

	event := c.createEvent("sample", map[string]interface{}{
		"message": "Sample event from template collector",
		"value":   42,
	})

	// Send event
	select {
	case c.events <- event:
		c.updateStats(true)
	default:
		// Buffer full
		c.updateStats(false)
	}
}

// createEvent creates a RawEvent with enhanced metadata
func (c *Collector) createEvent(eventType string, data interface{}) collectors.RawEvent {
	// Standard metadata ALL collectors must include
	metadata := map[string]string{
		"collector_name": c.name,
		"event_type":     eventType,
	}

	// Enhanced K8s metadata (if available)
	// This is what makes Tapio special - every event has context
	if k8sData := c.extractK8sMetadata(data); k8sData != nil {
		metadata["k8s_namespace"] = k8sData.Namespace
		metadata["k8s_name"] = k8sData.Name
		metadata["k8s_kind"] = k8sData.Kind
		metadata["k8s_uid"] = k8sData.UID
		metadata["k8s_labels"] = k8sData.Labels
		metadata["k8s_owner_refs"] = k8sData.OwnerRefs
	}

	// Convert data to JSON bytes
	jsonData, _ := json.Marshal(data)

	return collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      c.name,
		Data:      jsonData,
		Metadata:  metadata,
		TraceID:   collectors.GenerateTraceID(),
		SpanID:    collectors.GenerateSpanID(),
	}
}

// extractK8sMetadata extracts Kubernetes metadata from event data
func (c *Collector) extractK8sMetadata(data interface{}) *K8sMetadata {
	// Each collector implements this based on their data source
	// Template returns nil as example
	return nil
}

// updateStats updates collector statistics
func (c *Collector) updateStats(success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if success {
		c.stats.EventsGenerated++
		c.stats.LastEventTime = time.Now()
	} else {
		c.stats.EventsDropped++
	}
}
