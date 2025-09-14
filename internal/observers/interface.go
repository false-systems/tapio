package observers

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Observer defines the minimal interface that all observers must implement
type Observer interface {
	// Name returns the unique identifier for this observer
	Name() string

	// Start begins the observation process
	// It should return quickly and run observation in background
	Start(ctx context.Context) error

	// Stop gracefully shuts down the observer
	Stop() error

	// Events returns a channel of collector events
	// The channel is closed when the observer stops
	Events() <-chan *domain.CollectorEvent

	// IsHealthy returns true if the observer is functioning properly
	IsHealthy() bool
}

// ObserverConfig provides common configuration for all observers
type ObserverConfig struct {
	// BufferSize for the events channel
	BufferSize int

	// MetricsEnabled determines if the observer should expose metrics
	MetricsEnabled bool

	// Labels to add to all events from this observer
	Labels map[string]string
}

// DefaultObserverConfig returns sensible defaults
func DefaultObserverConfig() ObserverConfig {
	return ObserverConfig{
		BufferSize:     1000,
		MetricsEnabled: true,
		Labels:         make(map[string]string),
	}
}
