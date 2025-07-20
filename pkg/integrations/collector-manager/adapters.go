package manager

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// LegacyCollectorAdapter adapts old domain.Event collectors to UnifiedEvent interface
// This enables gradual migration while maintaining backward compatibility
type LegacyCollectorAdapter struct {
	legacyCollector LegacyCollector
	eventChan       chan domain.UnifiedEvent
	ctx             context.Context
	cancel          context.CancelFunc
}

// LegacyCollector interface for old collectors
type LegacyCollector interface {
	Start(ctx context.Context) error
	Stop() error
	Events() <-chan domain.Event
	Health() domain.HealthStatus
}

// NewLegacyCollectorAdapter creates adapter for legacy collectors
func NewLegacyCollectorAdapter(legacy LegacyCollector) *LegacyCollectorAdapter {
	return &LegacyCollectorAdapter{
		legacyCollector: legacy,
		eventChan:       make(chan domain.UnifiedEvent, 1000),
	}
}

// Start starts the legacy collector and event conversion
func (a *LegacyCollectorAdapter) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)

	// Start legacy collector
	if err := a.legacyCollector.Start(a.ctx); err != nil {
		return err
	}

	// Start event conversion goroutine
	go a.convertEvents()
	return nil
}

// Stop stops the legacy collector
func (a *LegacyCollectorAdapter) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	close(a.eventChan)
	return a.legacyCollector.Stop()
}

// Events returns converted UnifiedEvent stream
func (a *LegacyCollectorAdapter) Events() <-chan domain.UnifiedEvent {
	return a.eventChan
}

// Health adapts legacy health to modern interface
func (a *LegacyCollectorAdapter) Health() CollectorHealth {
	legacyHealth := a.legacyCollector.Health()
	return &legacyHealthAdapter{status: string(legacyHealth)}
}

// Statistics provides basic statistics for legacy collectors
func (a *LegacyCollectorAdapter) Statistics() CollectorStatistics {
	return &legacyStatsAdapter{}
}

// convertEvents converts legacy domain.Event to domain.UnifiedEvent
func (a *LegacyCollectorAdapter) convertEvents() {
	for {
		select {
		case event, ok := <-a.legacyCollector.Events():
			if !ok {
				return // Channel closed
			}

			// Convert legacy event to UnifiedEvent
			unifiedEvent := &domain.UnifiedEvent{
				ID:        string(event.ID),
				Timestamp: event.Timestamp,
				Type:      event.Type,
				Source:    string(event.Source),

				// Add basic semantic context for legacy events
				Semantic: &domain.SemanticContext{
					Intent:     "legacy-event",
					Category:   "operations",
					Tags:       []string{"legacy", "migrated"},
					Narrative:  "Legacy event converted to UnifiedEvent",
					Confidence: 0.5, // Lower confidence for converted events
				},

				// Basic impact context
				Impact: &domain.ImpactContext{
					Severity:       "info",
					BusinessImpact: 0.1,
					CustomerFacing: false,
				},

				// Store original event data
				RawData: []byte(event.Message),
			}

			select {
			case a.eventChan <- *unifiedEvent:
			case <-a.ctx.Done():
				return
			}

		case <-a.ctx.Done():
			return
		}
	}
}

// legacyHealthAdapter implements CollectorHealth for legacy collectors
type legacyHealthAdapter struct {
	status string
}

func (h *legacyHealthAdapter) Status() string              { return h.status }
func (h *legacyHealthAdapter) IsHealthy() bool             { return h.status == "healthy" }
func (h *legacyHealthAdapter) LastEventTime() time.Time    { return time.Now() }
func (h *legacyHealthAdapter) ErrorCount() uint64          { return 0 }
func (h *legacyHealthAdapter) Metrics() map[string]float64 { return make(map[string]float64) }

// legacyStatsAdapter implements CollectorStatistics for legacy collectors
type legacyStatsAdapter struct{}

func (s *legacyStatsAdapter) EventsProcessed() uint64        { return 0 }
func (s *legacyStatsAdapter) EventsDropped() uint64          { return 0 }
func (s *legacyStatsAdapter) StartTime() time.Time           { return time.Now() }
func (s *legacyStatsAdapter) Custom() map[string]interface{} { return make(map[string]interface{}) }
