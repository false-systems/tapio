package performance

import (
	"github.com/yairfalse/tapio/pkg/domain"
)

// Event is an alias for UnifiedEvent to maintain compatibility
// with performance-focused components that reference performance.Event
type Event = domain.UnifiedEvent

// EventMetadata represents performance-specific metadata
// that can be attached to events for analytics processing
type EventMetadata struct {
	// Validation state (0 = not validated, 1 = validated)
	Validated uint64
	// Enrichment state (0 = not enriched, 1 = enriched)
	Enriched uint64
	// Correlation state (0 = not correlated, 1 = correlated)
	Correlated uint64
	// Analytics state (0 = not analyzed, 1 = analyzed)
	Analyzed uint64
	// Performance timestamp for latency tracking
	ProcessingStartTime int64
	// Pointer to the actual event data
	EventPointer uintptr
}
