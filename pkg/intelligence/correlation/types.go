package correlation

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Simple type aliases to fix undefined types in semantic_correlation_engine.go
// These map the undefined types to actual domain types

// Event is an alias for domain.Event
type Event = domain.Event

// Insight is an alias for domain.Insight
type Insight = domain.Insight

// Severity is an alias for domain.Severity
type Severity = domain.Severity

// Collector interface for collectors that provide events
type Collector interface {
	// Name returns the collector name
	Name() string

	// Events returns the event channel
	Events() <-chan domain.Event

	// Start begins event collection
	Start() error

	// Stop ends event collection
	Stop() error
}

// CorrelationEngineInterface defines the correlation engine contract
type CorrelationEngineInterface interface {
	// RegisterCollector registers a collector
	RegisterCollector(c Collector) error

	// Start begins correlation processing
	Start(ctx context.Context) error

	// Stop ends correlation processing
	Stop()

	// Insights returns the insights channel
	Insights() <-chan Insight

	// Events returns the events channel
	Events() <-chan Event
}

// Additional type aliases for collector package compatibility

// Context aliases for event contexts
type EventContext = domain.EventContext
type EventPayload = domain.EventPayload
type EventMetadata = domain.EventMetadata
