package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/events_correlation"
)

// MemoryEventStore implements EventStore interface using in-memory storage
type MemoryEventStore struct {
	events []events_correlation.Event
	mu     sync.RWMutex

	// Configuration
	maxEvents       int
	retentionPeriod time.Duration

	// Metrics
	totalEvents  uint64
	storageSize  uint64
	queryLatency time.Duration
}

// NewMemoryEventStore creates a new in-memory event store
func NewMemoryEventStore(maxEvents int, retentionPeriod time.Duration) *MemoryEventStore {
	return &MemoryEventStore{
		events:          make([]events_correlation.Event, 0, maxEvents),
		maxEvents:       maxEvents,
		retentionPeriod: retentionPeriod,
	}
}

// GetEvents retrieves events matching the filter
func (m *MemoryEventStore) GetEvents(ctx context.Context, filter events_correlation.Filter) ([]events_correlation.Event, error) {
	start := time.Now()
	defer func() {
		m.queryLatency = time.Since(start)
	}()

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []events_correlation.Event

	for _, event := range m.events {
		if filter.Matches(event) {
			result = append(result, event)

			// Apply limit if specified
			if filter.Limit > 0 && len(result) >= filter.Limit {
				break
			}
		}
	}

	return result, nil
}

// GetEventsInWindow retrieves events within a time window matching the filter
func (m *MemoryEventStore) GetEventsInWindow(ctx context.Context, window events_correlation.TimeWindow, filter events_correlation.Filter) ([]events_correlation.Event, error) {
	start := time.Now()
	defer func() {
		m.queryLatency = time.Since(start)
	}()

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []events_correlation.Event

	for _, event := range m.events {
		// Check if event is within the time window
		if !window.Contains(event.Timestamp) {
			continue
		}

		if filter.Matches(event) {
			result = append(result, event)

			// Apply limit if specified
			if filter.Limit > 0 && len(result) >= filter.Limit {
				break
			}
		}
	}

	return result, nil
}

// StoreEvent stores a single event
func (m *MemoryEventStore) StoreEvent(ctx context.Context, event events_correlation.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Add the event
	m.events = append(m.events, event)
	m.totalEvents++

	// Sort events by timestamp (newest first)
	sort.Slice(m.events, func(i, j int) bool {
		return m.events[i].Timestamp.After(m.events[j].Timestamp)
	})

	// Enforce max events limit
	if len(m.events) > m.maxEvents {
		m.events = m.events[:m.maxEvents]
	}

	// Clean up old events based on retention period
	cutoff := time.Now().Add(-m.retentionPeriod)
	newEvents := make([]events_correlation.Event, 0, len(m.events))
	for _, e := range m.events {
		if e.Timestamp.After(cutoff) {
			newEvents = append(newEvents, e)
		}
	}
	m.events = newEvents

	// Update storage size estimate
	m.storageSize = uint64(len(m.events) * 1024) // Rough estimate: 1KB per event

	return nil
}

// StoreBatch stores multiple events
func (m *MemoryEventStore) StoreBatch(ctx context.Context, events []events_correlation.Event) error {
	for _, event := range events {
		if err := m.StoreEvent(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

// GetMetrics retrieves metrics (not implemented for memory store)
func (m *MemoryEventStore) GetMetrics(ctx context.Context, name string, window events_correlation.TimeWindow) (events_correlation.MetricSeries, error) {
	// Memory store doesn't support metrics
	return events_correlation.MetricSeries{
		Name:   name,
		Points: []events_correlation.MetricPoint{},
	}, nil
}

// Cleanup removes events older than the specified time
func (m *MemoryEventStore) Cleanup(ctx context.Context, before time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newEvents := make([]events_correlation.Event, 0, len(m.events))
	for _, event := range m.events {
		if event.Timestamp.After(before) {
			newEvents = append(newEvents, event)
		}
	}

	removedCount := len(m.events) - len(newEvents)
	m.events = newEvents
	m.storageSize = uint64(len(m.events) * 1024)

	if removedCount > 0 {
		// Events removed during cleanup
	}

	return nil
}

// Stats returns statistics about the event store
func (m *MemoryEventStore) Stats(ctx context.Context) (events_correlation.EventStoreStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := events_correlation.EventStoreStats{
		TotalEvents:     m.totalEvents,
		EventsPerSource: make(map[events_correlation.EventSource]uint64),
		StorageSize:     m.storageSize,
		RetentionPeriod: m.retentionPeriod,
		QueryLatency:    m.queryLatency,
	}

	// Find oldest and newest events
	if len(m.events) > 0 {
		stats.NewestEvent = m.events[0].Timestamp
		stats.OldestEvent = m.events[len(m.events)-1].Timestamp

		// Count events per source
		for _, event := range m.events {
			stats.EventsPerSource[event.Source]++
		}
	}

	return stats, nil
}

// Size returns the current number of events stored
func (m *MemoryEventStore) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.events)
}

// Clear removes all events from the store
func (m *MemoryEventStore) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.events = m.events[:0]
	m.totalEvents = 0
	m.storageSize = 0
}
