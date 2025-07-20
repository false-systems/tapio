package grpc

import (
	"context"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/interfaces/server/adapters/correlation"
	corrDomain "github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"go.uber.org/zap"
)

// EventStoreWrapper wraps the correlation event store to implement our EventStore interface
type EventStoreWrapper struct {
	store  *correlation.InMemoryEventStore
	logger *zap.Logger
	mu     sync.RWMutex
}

// NewEventStoreWrapper creates a new event store wrapper
func NewEventStoreWrapper(logger *zap.Logger) *EventStoreWrapper {
	store := correlation.NewInMemoryEventStore(
		50000,          // Max events in circular buffer
		7*24*time.Hour, // 7 days retention
	)

	return &EventStoreWrapper{
		store:  store,
		logger: logger,
	}
}

// Store implements EventStore interface
func (w *EventStoreWrapper) Store(ctx context.Context, events []domain.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Convert to the format expected by correlation store (domain.Event)
	corrEvents := make([]corrDomain.Event, len(events))
	for i, event := range events {
		corrEvents[i] = corrDomain.Event(event) // Since Event is aliased to domain.Event
	}

	return w.store.Store(ctx, corrEvents)
}

// Query implements EventStore interface
func (w *EventStoreWrapper) Query(ctx context.Context, filter domain.Filter) ([]domain.Event, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Convert filter to correlation filter
	corrFilter := w.convertToCorrelationFilter(filter)

	// Query the store
	corrEvents, err := w.store.Query(ctx, corrFilter)
	if err != nil {
		return nil, err
	}

	// Convert back to domain events (no conversion needed since they're aliases)
	events := make([]domain.Event, len(corrEvents))
	for i, event := range corrEvents {
		events[i] = domain.Event(event)
	}

	return events, nil
}

// Get implements EventStore interface
func (w *EventStoreWrapper) Get(ctx context.Context, eventIDs []string) ([]domain.Event, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Get events by IDs
	corrEvents, err := w.store.Get(ctx, eventIDs)
	if err != nil {
		return nil, err
	}

	// Convert back to domain events (no conversion needed)
	events := make([]domain.Event, len(corrEvents))
	for i, event := range corrEvents {
		events[i] = domain.Event(event)
	}

	return events, nil
}

// GetLatest implements EventStore interface
func (w *EventStoreWrapper) GetLatest(ctx context.Context, limit int) ([]domain.Event, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	corrEvents, err := w.store.GetLatest(ctx, limit)
	if err != nil {
		return nil, err
	}

	// Convert back to domain events (no conversion needed)
	events := make([]domain.Event, len(corrEvents))
	for i, event := range corrEvents {
		events[i] = domain.Event(event)
	}

	return events, nil
}

// Cleanup implements EventStore interface
func (w *EventStoreWrapper) Cleanup(ctx context.Context, before time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.store.Cleanup(ctx, before)
}

// Delete implements EventStore interface
func (w *EventStoreWrapper) Delete(ctx context.Context, eventIDs []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.store.Delete(ctx, eventIDs)
}

// GetStats implements EventStore interface
func (w *EventStoreWrapper) GetStats() correlation.EventStoreStats {
	return w.store.GetStats()
}

// Helper conversion methods

// convertToCorrelationFilter converts domain.Filter to corrDomain.Filter
func (w *EventStoreWrapper) convertToCorrelationFilter(filter domain.Filter) corrDomain.Filter {
	// Convert to the correlation Filter structure
	return corrDomain.Filter{
		TimeRange: &corrDomain.TimeRange{
			Start: filter.Since,
			End:   filter.Until,
		},
		Limit: filter.Limit,
		// Map other filter fields as needed
		// The correlation Filter has different fields than domain.Filter
	}
}