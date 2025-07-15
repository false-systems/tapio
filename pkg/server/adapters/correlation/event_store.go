package correlation

import (
	"container/ring"
	"context"
	"fmt"
	"sync"
	"time"

	corrDomain "github.com/yairfalse/tapio/pkg/correlation/domain"
)

// InMemoryEventStore provides a production-ready in-memory event store
// with time-based indexing, efficient querying, and automatic cleanup
type InMemoryEventStore struct {
	// Time-series optimized storage
	events       *ring.Ring           // Circular buffer for recent events
	timeIndex    map[int64][]int      // Unix timestamp -> event positions
	resourceIndex map[string][]int     // Resource ID -> event positions
	typeIndex    map[string][]int     // Event type -> event positions
	
	// Configuration
	maxEvents    int
	retentionPeriod time.Duration
	
	// State management
	mu           sync.RWMutex
	position     int
	eventMap     map[int]corrDomain.Event
	
	// Statistics
	stats        EventStoreStats
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
func (s *InMemoryEventStore) Query(ctx context.Context, query corrDomain.EventQuery) ([]corrDomain.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	start := time.Now()
	defer func() {
		s.stats.EventsQueried++
		s.stats.AverageQueryTime = time.Since(start)
	}()
	
	// Build candidate set using indices
	candidates := s.getCandidates(query)
	
	// Apply filters
	var results []corrDomain.Event
	for pos := range candidates {
		if event, exists := s.eventMap[pos]; exists {
			if s.matchesQuery(event, query) {
				results = append(results, event)
			}
		}
	}
	
	// Sort by timestamp (newest first)
	s.sortEventsByTime(results)
	
	// Apply limit
	if query.Limit > 0 && len(results) > query.Limit {
		results = results[:query.Limit]
	}
	
	return results, nil
}

// GetLatest implements EventStore interface
func (s *InMemoryEventStore) GetLatest(ctx context.Context, limit int) ([]corrDomain.Event, error) {
	return s.Query(ctx, corrDomain.EventQuery{
		TimeRange: &corrDomain.TimeRange{
			Start: time.Now().Add(-time.Hour),
			End:   time.Now(),
		},
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
	
	// Resource index
	if event.ResourceID != "" {
		s.resourceIndex[event.ResourceID] = append(s.resourceIndex[event.ResourceID], position)
	}
	
	// Type index
	s.typeIndex[event.Type] = append(s.typeIndex[event.Type], position)
}

// removeFromIndices removes event from all indices
func (s *InMemoryEventStore) removeFromIndices(event corrDomain.Event, position int) {
	// Remove from time index
	timeBucket := event.Timestamp.Unix() / 60
	s.removeFromSlice(&s.timeIndex[timeBucket], position)
	
	// Remove from resource index
	if event.ResourceID != "" {
		s.removeFromSlice(&s.resourceIndex[event.ResourceID], position)
	}
	
	// Remove from type index
	s.removeFromSlice(&s.typeIndex[event.Type], position)
}

// getCandidates returns event positions that might match the query
func (s *InMemoryEventStore) getCandidates(query corrDomain.EventQuery) map[int]bool {
	candidates := make(map[int]bool)
	
	// If we have a time range, use time index
	if query.TimeRange != nil {
		startBucket := query.TimeRange.Start.Unix() / 60
		endBucket := query.TimeRange.End.Unix() / 60
		
		for bucket := startBucket; bucket <= endBucket; bucket++ {
			for _, pos := range s.timeIndex[bucket] {
				candidates[pos] = true
			}
		}
		return candidates
	}
	
	// If we have resource filters, use resource index
	if len(query.ResourceIDs) > 0 {
		for _, resourceID := range query.ResourceIDs {
			for _, pos := range s.resourceIndex[resourceID] {
				candidates[pos] = true
			}
		}
		return candidates
	}
	
	// If we have type filters, use type index
	if len(query.Types) > 0 {
		for _, eventType := range query.Types {
			for _, pos := range s.typeIndex[eventType] {
				candidates[pos] = true
			}
		}
		return candidates
	}
	
	// No specific filters, return all
	for pos := range s.eventMap {
		candidates[pos] = true
	}
	
	return candidates
}

// matchesQuery checks if event matches all query criteria
func (s *InMemoryEventStore) matchesQuery(event corrDomain.Event, query corrDomain.EventQuery) bool {
	// Time range filter
	if query.TimeRange != nil {
		if event.Timestamp.Before(query.TimeRange.Start) || event.Timestamp.After(query.TimeRange.End) {
			return false
		}
	}
	
	// Resource filter
	if len(query.ResourceIDs) > 0 {
		found := false
		for _, id := range query.ResourceIDs {
			if event.ResourceID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	
	// Type filter
	if len(query.Types) > 0 {
		found := false
		for _, t := range query.Types {
			if event.Type == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	
	// Severity filter
	if query.MinSeverity != "" && event.Severity < query.MinSeverity {
		return false
	}
	
	return true
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