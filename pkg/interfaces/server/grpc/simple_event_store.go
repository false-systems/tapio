package grpc

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// SimpleEventStore is a basic in-memory event store implementation
type SimpleEventStore struct {
	mu     sync.RWMutex
	events map[string]domain.Event
	logger *zap.Logger

	// Configuration
	maxEvents       int
	retentionPeriod time.Duration

	// Statistics
	stats SimpleEventStoreStats
}

// SimpleEventStoreStats tracks store metrics
type SimpleEventStoreStats struct {
	TotalEvents      int64
	EventsStored     int64
	EventsQueried    int64
	AverageQueryTime time.Duration
	LastCleanup      time.Time
}

// NewSimpleEventStore creates a new simple event store
func NewSimpleEventStore(maxEvents int, retention time.Duration, logger *zap.Logger) *SimpleEventStore {
	return &SimpleEventStore{
		events:          make(map[string]domain.Event),
		logger:          logger,
		maxEvents:       maxEvents,
		retentionPeriod: retention,
		stats: SimpleEventStoreStats{
			LastCleanup: time.Now(),
		},
	}
}

// Store implements EventStore interface
func (s *SimpleEventStore) Store(ctx context.Context, events []domain.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, event := range events {
		// Validate event has an ID
		if event.ID == "" {
			s.logger.Warn("Skipping event with empty ID")
			continue
		}

		// Store event
		s.events[string(event.ID)] = event
		s.stats.EventsStored++
	}

	s.stats.TotalEvents += int64(len(events))

	// Simple cleanup - remove oldest events if we exceed max
	if len(s.events) > s.maxEvents {
		s.cleanupOldEvents()
	}

	return nil
}

// Query implements EventStore interface
func (s *SimpleEventStore) Query(ctx context.Context, filter domain.Filter) ([]domain.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	start := time.Now()
	defer func() {
		s.stats.EventsQueried++
		s.stats.AverageQueryTime = time.Since(start)
	}()

	var results []domain.Event

	// Simple filtering - iterate through all events
	for _, event := range s.events {
		if s.matchesFilter(event, filter) {
			results = append(results, event)
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.After(results[j].Timestamp)
	})

	// Apply limit
	if filter.Limit > 0 && len(results) > filter.Limit {
		results = results[:filter.Limit]
	}

	return results, nil
}

// Get implements EventStore interface
func (s *SimpleEventStore) Get(ctx context.Context, eventIDs []string) ([]domain.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []domain.Event
	for _, id := range eventIDs {
		if event, exists := s.events[id]; exists {
			results = append(results, event)
		}
	}

	return results, nil
}

// GetLatest implements EventStore interface
func (s *SimpleEventStore) GetLatest(ctx context.Context, limit int) ([]domain.Event, error) {
	return s.Query(ctx, domain.Filter{
		Since: time.Now().Add(-time.Hour), // Last hour
		Until: time.Now(),
		Limit: limit,
	})
}

// Cleanup implements EventStore interface
func (s *SimpleEventStore) Cleanup(ctx context.Context, before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var removed int
	for id, event := range s.events {
		if event.Timestamp.Before(before) {
			delete(s.events, id)
			removed++
		}
	}

	s.stats.LastCleanup = time.Now()
	s.logger.Debug("Cleanup completed",
		zap.Int("removed_events", removed),
		zap.Time("before", before),
	)

	return nil
}

// Delete implements EventStore interface
func (s *SimpleEventStore) Delete(ctx context.Context, eventIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range eventIDs {
		delete(s.events, id)
	}

	return nil
}

// GetStats implements EventStore interface - return our own stats as interface{}
func (s *SimpleEventStore) GetStats() interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.stats
}

// Helper methods

// matchesFilter checks if an event matches the filter criteria
func (s *SimpleEventStore) matchesFilter(event domain.Event, filter domain.Filter) bool {
	// Time range filter
	if !filter.Since.IsZero() && event.Timestamp.Before(filter.Since) {
		return false
	}
	if !filter.Until.IsZero() && event.Timestamp.After(filter.Until) {
		return false
	}

	// Type filter
	if filter.Type != "" && string(event.Type) != filter.Type {
		return false
	}

	// Severity filter
	if filter.Severity != "" && string(event.Severity) != filter.Severity {
		return false
	}

	// Namespace filter
	if filter.Namespace != "" && event.Context.Namespace != filter.Namespace {
		return false
	}

	// Entity name filter
	if filter.EntityName != "" && event.Context.Pod != filter.EntityName {
		return false
	}

	// Entity type filter (using event type as proxy)
	if filter.EntityType != "" && string(event.Type) != filter.EntityType {
		return false
	}

	return true
}

// cleanupOldEvents removes old events when max capacity is exceeded
func (s *SimpleEventStore) cleanupOldEvents() {
	// Convert to slice for sorting
	type eventWithID struct {
		id    string
		event domain.Event
	}

	var eventList []eventWithID
	for id, event := range s.events {
		eventList = append(eventList, eventWithID{id: id, event: event})
	}

	// Sort by timestamp (oldest first)
	sort.Slice(eventList, func(i, j int) bool {
		return eventList[i].event.Timestamp.Before(eventList[j].event.Timestamp)
	})

	// Remove oldest events until we're under the limit
	toRemove := len(eventList) - s.maxEvents + 1000 // Remove extra to avoid frequent cleanups
	if toRemove > 0 {
		for i := 0; i < toRemove && i < len(eventList); i++ {
			delete(s.events, eventList[i].id)
		}
		s.logger.Debug("Removed old events during cleanup",
			zap.Int("removed", toRemove),
			zap.Int("remaining", len(s.events)),
		)
	}
}

// GetSimpleStats returns the simple store statistics
func (s *SimpleEventStore) GetSimpleStats() SimpleEventStoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}
