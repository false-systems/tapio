package correlation

import (
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

// DEPRECATED: Use interfaces.CorrelationEngine instead
// This interface is kept for backward compatibility and will be removed in future versions

// Additional type aliases for collector package compatibility

// Context aliases for event contexts
type EventContext = domain.EventContext
type EventPayload = domain.EventPayload

// Severity constants
const (
	SeverityCritical Severity = domain.SeverityCritical
	SeverityHigh     Severity = domain.SeverityHigh
	SeverityMedium   Severity = domain.SeverityMedium
	SeverityLow      Severity = domain.SeverityLow
	SeverityInfo     Severity = domain.SeverityInfo
)
