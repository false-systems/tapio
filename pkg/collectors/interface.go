package collectors

import (
	"context"
	"time"
)

// RawEvent represents raw data collected from any source
type RawEvent struct {
	// Timestamp when the event was collected
	Timestamp time.Time

	// Type identifies the collector that generated this event
	Type string

	// Data contains the raw bytes from the collector
	// This will be interpreted by the pipeline based on Type
	Data []byte

	// Metadata provides basic context without business logic
	Metadata map[string]string
}

// Collector defines the minimal interface that all collectors must implement
type Collector interface {
	// Name returns the unique identifier for this collector
	Name() string

	// Start begins the collection process
	// It should return quickly and run collection in background
	Start(ctx context.Context) error

	// Stop gracefully shuts down the collector
	Stop() error

	// Events returns a channel of raw events
	// The channel is closed when the collector stops
	Events() <-chan RawEvent

	// IsHealthy returns true if the collector is functioning properly
	IsHealthy() bool
}

// CollectorConfig provides common configuration for all collectors
type CollectorConfig struct {
	// BufferSize for the events channel
	BufferSize int

	// MetricsEnabled determines if the collector should expose metrics
	MetricsEnabled bool

	// Labels to add to all events from this collector
	Labels map[string]string
}

// DefaultCollectorConfig returns sensible defaults
func DefaultCollectorConfig() CollectorConfig {
	return CollectorConfig{
		BufferSize:     1000,
		MetricsEnabled: true,
		Labels:         make(map[string]string),
	}
}
