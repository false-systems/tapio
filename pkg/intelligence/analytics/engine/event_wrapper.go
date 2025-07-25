package engine

import (
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/performance"
)

// AnalyticsEvent wraps a UnifiedEvent with metadata for analytics processing
// This allows the analytics engine to track processing state without modifying
// the original event structure
type AnalyticsEvent struct {
	*domain.UnifiedEvent

	// Metadata array for tracking processing state
	// Index 0: EventPointer (uintptr) - not used with wrapper
	// Index 1: Validated (0 or 1)
	// Index 2: Enriched (0 or 1)
	// Index 3: Correlated (0 or 1)
	// Index 4: AnomalyScore (0-100)
	// Index 5: AnomalyFlag (0 or 1)
	// Index 6: ProcessingTimeNanos
	// Index 7: Analyzed (0 or 1)
	Metadata [8]uint64
}

// WrapEvent creates an AnalyticsEvent from a UnifiedEvent
func WrapEvent(event *performance.Event) *AnalyticsEvent {
	return &AnalyticsEvent{
		UnifiedEvent: event,
		Metadata:     [8]uint64{},
	}
}

// UnwrapEvent extracts the UnifiedEvent from an AnalyticsEvent
func UnwrapEvent(event *AnalyticsEvent) *performance.Event {
	return event.UnifiedEvent
}
