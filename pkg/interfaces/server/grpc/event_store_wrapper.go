package grpc

import (
	"context"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	corrDomain "github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"go.uber.org/zap"
)

// EventStoreWrapper wraps a simple event store to implement our EventStore interface
type EventStoreWrapper struct {
	store  *SimpleEventStore
	logger *zap.Logger
	mu     sync.RWMutex
}

// NewEventStoreWrapper creates a new event store wrapper
func NewEventStoreWrapper(logger *zap.Logger) *EventStoreWrapper {
	store := NewSimpleEventStore(
		50000,          // Max events in memory
		7*24*time.Hour, // 7 days retention
		logger.Named("wrapped-event-store"),
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

	// Store events directly
	return w.store.Store(ctx, events)
}

// Query implements EventStore interface
func (w *EventStoreWrapper) Query(ctx context.Context, filter domain.Filter) ([]domain.Event, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Query the store directly
	return w.store.Query(ctx, filter)
}

// Get implements EventStore interface
func (w *EventStoreWrapper) Get(ctx context.Context, eventIDs []string) ([]domain.Event, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Get events by IDs directly
	return w.store.Get(ctx, eventIDs)
}

// GetLatest implements EventStore interface
func (w *EventStoreWrapper) GetLatest(ctx context.Context, limit int) ([]domain.Event, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.store.GetLatest(ctx, limit)
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
func (w *EventStoreWrapper) GetStats() EventStoreStats {
	return w.store.GetStats()
}
