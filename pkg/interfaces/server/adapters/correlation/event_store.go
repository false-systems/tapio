package correlation

import (
	"container/ring"
	"context"
	"fmt"
	"sync"
	"time"

	corrDomain "github.com/yairfalse/tapio/pkg/intelligence/correlation/domain"
)

// InMemoryEventStore provides a production-ready in-memory event store
// with time-based indexing, efficient querying, and automatic cleanup
type InMemoryEventStore struct {
	// Time-series optimized storage
	events        *ring.Ring       // Circular buffer for recent events
	timeIndex     map[int64][]int  // Unix timestamp -> event positions
	resourceIndex map[string][]int // Resource ID -> event positions
	typeIndex     map[string][]int // Event type -> event positions

	// Configuration
	maxEvents       int
	retentionPeriod time.Duration

	// State management
	mu       sync.RWMutex
	position int
	eventMap map[int]corrDomain.Event

	// Statistics
	stats EventStoreStats
}

// EventStoreStats tracks performance metrics
type EventStoreStats struct {
	TotalEvents      int64
	EventsStored     int64
	EventsQueried    int64
	AverageQueryTime time.Duration
	LastCleanup      time.Time
}

// NewInMemoryEventStore creates a production-ready event store
func NewInMemoryEventStore(maxEvents int, retention time.Duration) *InMemoryEventStore {
	return &InMemoryEventStore{
		events:          ring.New(maxEvents),
		timeIndex:       make(map[int64][]int),
		resourceIndex:   make(map[string][]int),
		typeIndex:       make(map[string][]int),
		maxEvents:       maxEvents,
		retentionPeriod: retention,
		eventMap:        make(map[int]corrDomain.Event),
		stats:           EventStoreStats{LastCleanup: time.Now()},
	}
}

// Store implements EventStore interface with efficient storage
func (s *InMemoryEventStore) Store(ctx context.Context, events []corrDomain.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, event := range events {
		// Validate event
		if err := s.validateEvent(event); err != nil {
			return fmt.Errorf("invalid event: %w", err)
		}

		// Store in circular buffer
		s.position = (s.position + 1) % s.maxEvents
		s.events.Value = s.position
		s.events = s.events.Next()

		// Update event map
		if oldEvent, exists := s.eventMap[s.position]; exists {
			// Remove old event from indices
			s.removeFromIndices(oldEvent, s.position)
		}

		// Store new event
		s.eventMap[s.position] = event

		// Update indices for fast querying
		s.updateIndices(event, s.position)

		// Update statistics
		s.stats.TotalEvents++
		s.stats.EventsStored++
	}

	// Periodic cleanup
	if time.Since(s.stats.LastCleanup) > time.Hour {
		go s.cleanup()
	}

	return nil
}

// Query implements EventStore interface with optimized querying
func (s *InMemoryEventStore) Query(ctx context.Context, filter corrDomain.Filter) ([]corrDomain.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	start := time.Now()
	defer func() {
		s.stats.EventsQueried++
		s.stats.AverageQueryTime = time.Since(start)
	}()

	// Build candidate set using indices
	candidates := s.getCandidates(filter)

	// Apply filters
	var results []corrDomain.Event
	for pos := range candidates {
		if event, exists := s.eventMap[pos]; exists {
			if s.matchesFilter(event, filter) {
				results = append(results, event)
			}
		}
	}

	// Sort by timestamp (newest first)
	s.sortEventsByTime(results)

	// Apply limit
	if filter.Limit > 0 && len(results) > filter.Limit {
		results = results[:filter.Limit]
	}

	return results, nil
}

// GetLatest implements EventStore interface
func (s *InMemoryEventStore) GetLatest(ctx context.Context, limit int) ([]corrDomain.Event, error) {
	return s.Query(ctx, corrDomain.Filter{
		Since: time.Now().Add(-time.Hour),
		Until: time.Now(),
		Limit: limit,
	})
}

// validateEvent ensures event has required fields
func (s *InMemoryEventStore) validateEvent(event corrDomain.Event) error {
	if event.ID == "" {
		return fmt.Errorf("event ID is required")
	}
	if event.Timestamp.IsZero() {
		return fmt.Errorf("event timestamp is required")
	}
	if event.Type == "" {
		return fmt.Errorf("event type is required")
	}
	return nil
}

// updateIndices adds event to all relevant indices
func (s *InMemoryEventStore) updateIndices(event corrDomain.Event, position int) {
	// Time index (bucket by minute)
	timeBucket := event.Timestamp.Unix() / 60
	s.timeIndex[timeBucket] = append(s.timeIndex[timeBucket], position)

	// Resource index - use Entity.Name as ResourceID
	resourceID := event.Entity.Name
	if resourceID == "" && event.Entity.Type != "" {
		resourceID = event.Entity.Type + "/" + event.Entity.Namespace + "/" + event.Entity.Name
	}
	if resourceID != "" {
		s.resourceIndex[resourceID] = append(s.resourceIndex[resourceID], position)
	}

	// Type index
	s.typeIndex[event.Type] = append(s.typeIndex[event.Type], position)
}

// removeFromIndices removes event from all indices
func (s *InMemoryEventStore) removeFromIndices(event corrDomain.Event, position int) {
	// Remove from time index
	timeBucket := event.Timestamp.Unix() / 60
	if positions, exists := s.timeIndex[timeBucket]; exists {
		s.removeFromSlice(&positions, position)
		s.timeIndex[timeBucket] = positions
	}

	// Remove from resource index - use Entity.Name as ResourceID
	resourceID := event.Entity.Name
	if resourceID == "" && event.Entity.Type != "" {
		resourceID = event.Entity.Type + "/" + event.Entity.Namespace + "/" + event.Entity.Name
	}
	if resourceID != "" && len(s.resourceIndex[resourceID]) > 0 {
		positions := s.resourceIndex[resourceID]
		s.removeFromSlice(&positions, position)
		s.resourceIndex[resourceID] = positions
	}

	// Remove from type index
	if positions, exists := s.typeIndex[event.Type]; exists {
		s.removeFromSlice(&positions, position)
		s.typeIndex[event.Type] = positions
	}
}

// getCandidates returns event positions that might match the filter
func (s *InMemoryEventStore) getCandidates(filter corrDomain.Filter) map[int]bool {
	candidates := make(map[int]bool)

	// If we have a time range, use time index
	if !filter.Since.IsZero() || !filter.Until.IsZero() {
		start := filter.Since
		if start.IsZero() {
			start = time.Unix(0, 0)
		}
		end := filter.Until
		if end.IsZero() {
			end = time.Now()
		}
		startBucket := start.Unix() / 60
		endBucket := end.Unix() / 60

		for bucket := startBucket; bucket <= endBucket; bucket++ {
			for _, pos := range s.timeIndex[bucket] {
				candidates[pos] = true
			}
		}
		return candidates
	}

	// If we have entity filters, use resource index
	if filter.EntityName != "" {
		for _, pos := range s.resourceIndex[filter.EntityName] {
			candidates[pos] = true
		}
		return candidates
	}

	// If we have type filter, use type index
	if filter.Type != "" {
		for _, pos := range s.typeIndex[filter.Type] {
			candidates[pos] = true
		}
		return candidates
	}

	// No specific filters, return all
	for pos := range s.eventMap {
		candidates[pos] = true
	}

	return candidates
}

// matchesFilter checks if event matches all filter criteria
func (s *InMemoryEventStore) matchesFilter(event corrDomain.Event, filter corrDomain.Filter) bool {
	// Time range filter
	if !filter.Since.IsZero() && event.Timestamp.Before(filter.Since) {
		return false
	}
	if !filter.Until.IsZero() && event.Timestamp.After(filter.Until) {
		return false
	}

	// Entity filter
	if filter.EntityName != "" && event.Entity.Name != filter.EntityName {
		return false
	}
	if filter.EntityType != "" && event.Entity.Type != filter.EntityType {
		return false
	}
	if filter.Namespace != "" && event.Entity.Namespace != filter.Namespace {
		return false
	}

	// Type filter
	if filter.Type != "" && event.Type != filter.Type {
		return false
	}

	// Severity filter
	if filter.Severity != "" && event.Severity != filter.Severity {
		return false
	}

	return true
}

// Cleanup implements EventStore interface - removes events before specified time
func (s *InMemoryEventStore) Cleanup(ctx context.Context, before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove events before specified time
	for pos, event := range s.eventMap {
		if event.Timestamp.Before(before) {
			s.removeFromIndices(event, pos)
			delete(s.eventMap, pos)
		}
	}

	// Compact indices
	s.compactIndices()

	s.stats.LastCleanup = time.Now()
	return nil
}

// cleanup removes old events and compacts indices
func (s *InMemoryEventStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.retentionPeriod)

	// Remove old events
	for pos, event := range s.eventMap {
		if event.Timestamp.Before(cutoff) {
			s.removeFromIndices(event, pos)
			delete(s.eventMap, pos)
		}
	}

	// Compact indices
	s.compactIndices()

	s.stats.LastCleanup = time.Now()
}

// compactIndices removes empty entries from indices
func (s *InMemoryEventStore) compactIndices() {
	// Clean time index
	for bucket, positions := range s.timeIndex {
		if len(positions) == 0 {
			delete(s.timeIndex, bucket)
		}
	}

	// Clean resource index
	for resource, positions := range s.resourceIndex {
		if len(positions) == 0 {
			delete(s.resourceIndex, resource)
		}
	}

	// Clean type index
	for eventType, positions := range s.typeIndex {
		if len(positions) == 0 {
			delete(s.typeIndex, eventType)
		}
	}
}

// Helper functions

func (s *InMemoryEventStore) removeFromSlice(slice *[]int, value int) {
	for i, v := range *slice {
		if v == value {
			*slice = append((*slice)[:i], (*slice)[i+1:]...)
			return
		}
	}
}

func (s *InMemoryEventStore) sortEventsByTime(events []corrDomain.Event) {
	// Simple insertion sort for small datasets
	for i := 1; i < len(events); i++ {
		j := i
		for j > 0 && events[j].Timestamp.After(events[j-1].Timestamp) {
			events[j], events[j-1] = events[j-1], events[j]
			j--
		}
	}
}

// GetStats returns store statistics
func (s *InMemoryEventStore) GetStats() EventStoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

// Delete implements EventStore interface - deletes events by ID
func (s *InMemoryEventStore) Delete(ctx context.Context, eventIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create set for quick lookup
	toDelete := make(map[string]bool)
	for _, id := range eventIDs {
		toDelete[id] = true
	}

	// Delete matching events
	for pos, event := range s.eventMap {
		if toDelete[event.ID] {
			s.removeFromIndices(event, pos)
			delete(s.eventMap, pos)
		}
	}

	return nil
}

// Get implements EventStore interface - retrieves events by IDs
func (s *InMemoryEventStore) Get(ctx context.Context, eventIDs []string) ([]corrDomain.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create set for quick lookup
	requested := make(map[string]bool)
	for _, id := range eventIDs {
		requested[id] = true
	}

	// Find matching events
	var events []corrDomain.Event
	for _, event := range s.eventMap {
		if requested[event.ID] {
			events = append(events, event)
		}
	}

	return events, nil
}
