package collectors

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
)

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
	Events() <-chan domain.RawEvent

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
