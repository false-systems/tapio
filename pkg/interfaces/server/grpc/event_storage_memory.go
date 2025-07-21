package grpc

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
)

// MemoryEventStorage implements EventStorage using in-memory storage with indexing
type MemoryEventStorage struct {
	mu         sync.RWMutex
	events     map[string]*domain.UnifiedEvent
	eventOrder []string // Maintains insertion order
	maxSize    int

	// Indexes for faster querying
	typeIndex      map[domain.EventType][]string
	sourceIndex    map[string][]string
	namespaceIndex map[string][]string
	timeIndex      *TimeIndex

	// Configuration
	retentionPeriod time.Duration

	// Statistics
	totalStored    uint64
	totalRetrieved uint64
	queryCount     uint64
	evictedCount   uint64
}

// TimeIndex provides time-based indexing for efficient range queries
type TimeIndex struct {
	buckets    map[int64][]string // Unix timestamp (minute) -> event IDs
	bucketSize time.Duration      // 1 minute buckets
}

// NewMemoryEventStorage creates a new in-memory event storage
func NewMemoryEventStorage(maxSize int, retentionPeriod time.Duration) *MemoryEventStorage {
	return &MemoryEventStorage{
		events:          make(map[string]*domain.UnifiedEvent),
		eventOrder:      make([]string, 0, maxSize),
		maxSize:         maxSize,
		typeIndex:       make(map[domain.EventType][]string),
		sourceIndex:     make(map[string][]string),
		namespaceIndex:  make(map[string][]string),
		timeIndex:       newTimeIndex(),
		retentionPeriod: retentionPeriod,
	}
}

// newTimeIndex creates a new time index
func newTimeIndex() *TimeIndex {
	return &TimeIndex{
		buckets:    make(map[int64][]string),
		bucketSize: time.Minute,
	}
}

// Store stores a single event
func (ms *MemoryEventStorage) Store(ctx context.Context, event *domain.UnifiedEvent) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	return ms.storeEventLocked(event)
}

// StoreBatch stores a batch of events
func (ms *MemoryEventStorage) StoreBatch(ctx context.Context, events []*domain.UnifiedEvent) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	for _, event := range events {
		if err := ms.storeEventLocked(event); err != nil {
			return fmt.Errorf("failed to store event %s: %w", event.ID, err)
		}
	}

	return nil
}

// storeEventLocked stores an event (requires lock to be held)
func (ms *MemoryEventStorage) storeEventLocked(event *domain.UnifiedEvent) error {
	// Check if event already exists
	if _, exists := ms.events[event.ID]; exists {
		return nil // Idempotent operation
	}

	// Evict old events if at capacity
	if len(ms.events) >= ms.maxSize && ms.maxSize > 0 {
		ms.evictOldestLocked()
	}

	// Store event
	ms.events[event.ID] = event
	ms.eventOrder = append(ms.eventOrder, event.ID)
	ms.totalStored++

	// Update indexes
	ms.updateIndexes(event)

	// Clean up expired events
	ms.cleanupExpiredEventsLocked()

	return nil
}

// updateIndexes updates all indexes for the event
func (ms *MemoryEventStorage) updateIndexes(event *domain.UnifiedEvent) {
	// Type index
	ms.typeIndex[event.Type] = append(ms.typeIndex[event.Type], event.ID)

	// Source index
	ms.sourceIndex[event.Source] = append(ms.sourceIndex[event.Source], event.ID)

	// Namespace index
	if event.Entity != nil && event.Entity.Namespace != "" {
		ms.namespaceIndex[event.Entity.Namespace] = append(ms.namespaceIndex[event.Entity.Namespace], event.ID)
	}

	// Time index
	bucket := event.Timestamp.Truncate(ms.timeIndex.bucketSize).Unix()
	ms.timeIndex.buckets[bucket] = append(ms.timeIndex.buckets[bucket], event.ID)
}

// removeFromIndexes removes an event from all indexes
func (ms *MemoryEventStorage) removeFromIndexes(event *domain.UnifiedEvent) {
	// Type index
	ms.typeIndex[event.Type] = ms.removeFromSlice(ms.typeIndex[event.Type], event.ID)

	// Source index
	ms.sourceIndex[event.Source] = ms.removeFromSlice(ms.sourceIndex[event.Source], event.ID)

	// Namespace index
	if event.Entity != nil && event.Entity.Namespace != "" {
		ms.namespaceIndex[event.Entity.Namespace] = ms.removeFromSlice(ms.namespaceIndex[event.Entity.Namespace], event.ID)
	}

	// Time index
	bucket := event.Timestamp.Truncate(ms.timeIndex.bucketSize).Unix()
	ms.timeIndex.buckets[bucket] = ms.removeFromSlice(ms.timeIndex.buckets[bucket], event.ID)
}

// removeFromSlice removes an element from a slice
func (ms *MemoryEventStorage) removeFromSlice(slice []string, element string) []string {
	for i, v := range slice {
		if v == element {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

// evictOldestLocked evicts the oldest event
func (ms *MemoryEventStorage) evictOldestLocked() {
	if len(ms.eventOrder) == 0 {
		return
	}

	oldestID := ms.eventOrder[0]
	if event, exists := ms.events[oldestID]; exists {
		ms.removeFromIndexes(event)
		delete(ms.events, oldestID)
		ms.evictedCount++
	}

	ms.eventOrder = ms.eventOrder[1:]
}

// cleanupExpiredEventsLocked removes events older than retention period
func (ms *MemoryEventStorage) cleanupExpiredEventsLocked() {
	if ms.retentionPeriod == 0 {
		return
	}

	cutoff := time.Now().Add(-ms.retentionPeriod)
	expiredIDs := make([]string, 0)

	for id, event := range ms.events {
		if event.Timestamp.Before(cutoff) {
			expiredIDs = append(expiredIDs, id)
		}
	}

	for _, id := range expiredIDs {
		if event := ms.events[id]; event != nil {
			ms.removeFromIndexes(event)
			delete(ms.events, id)
			ms.evictedCount++
		}

		// Remove from order slice
		for i, orderID := range ms.eventOrder {
			if orderID == id {
				ms.eventOrder = append(ms.eventOrder[:i], ms.eventOrder[i+1:]...)
				break
			}
		}
	}
}

// Get retrieves an event by ID
func (ms *MemoryEventStorage) Get(ctx context.Context, id string) (*domain.UnifiedEvent, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	event, exists := ms.events[id]
	if !exists {
		return nil, fmt.Errorf("event not found")
	}

	ms.totalRetrieved++
	return event, nil
}

// Query queries events with filtering, time range, and pagination
func (ms *MemoryEventStorage) Query(ctx context.Context, filter *pb.Filter, timeRange *pb.TimeRange, limit int, token string) ([]*domain.UnifiedEvent, string, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	ms.queryCount++

	// Get candidate event IDs based on indexes
	candidateIDs := ms.getCandidateIDs(filter, timeRange)

	// Filter and sort candidates
	var matchingEvents []*domain.UnifiedEvent
	for _, id := range candidateIDs {
		if event, exists := ms.events[id]; exists && ms.matchesFilter(event, filter) && ms.inTimeRange(event, timeRange) {
			matchingEvents = append(matchingEvents, event)
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(matchingEvents, func(i, j int) bool {
		return matchingEvents[i].Timestamp.After(matchingEvents[j].Timestamp)
	})

	// Apply pagination
	start := 0
	if token != "" {
		for i, event := range matchingEvents {
			if event.ID == token {
				start = i + 1
				break
			}
		}
	}

	end := start + limit
	if end > len(matchingEvents) {
		end = len(matchingEvents)
	}

	result := matchingEvents[start:end]
	nextToken := ""
	if end < len(matchingEvents) {
		nextToken = matchingEvents[end-1].ID
	}

	return result, nextToken, nil
}

// getCandidateIDs gets candidate event IDs based on indexes
func (ms *MemoryEventStorage) getCandidateIDs(filter *pb.Filter, timeRange *pb.TimeRange) []string {
	var candidateIDs []string

	// Use the most selective index first
	if filter != nil && len(filter.EventTypes) > 0 {
		// Use type index
		for _, eventType := range filter.EventTypes {
			if ids, exists := ms.typeIndex[domain.EventType(eventType)]; exists {
				candidateIDs = append(candidateIDs, ids...)
			}
		}
	} else if filter != nil && len(filter.Sources) > 0 {
		// Use source index
		for _, source := range filter.Sources {
			if ids, exists := ms.sourceIndex[source]; exists {
				candidateIDs = append(candidateIDs, ids...)
			}
		}
	} else if filter != nil && len(filter.Namespaces) > 0 {
		// Use namespace index
		for _, namespace := range filter.Namespaces {
			if ids, exists := ms.namespaceIndex[namespace]; exists {
				candidateIDs = append(candidateIDs, ids...)
			}
		}
	} else if timeRange != nil {
		// Use time index
		candidateIDs = ms.getTimeRangeCandidates(timeRange)
	} else {
		// No filter, return all events
		candidateIDs = ms.eventOrder
	}

	// Remove duplicates
	return ms.removeDuplicates(candidateIDs)
}

// getTimeRangeCandidates gets event IDs within a time range
func (ms *MemoryEventStorage) getTimeRangeCandidates(timeRange *pb.TimeRange) []string {
	var candidateIDs []string

	start := time.Time{}
	end := time.Now()

	if timeRange.Start != nil {
		start = timeRange.Start.AsTime()
	}
	if timeRange.End != nil {
		end = timeRange.End.AsTime()
	}

	// Find all time buckets within range
	startBucket := start.Truncate(ms.timeIndex.bucketSize).Unix()
	endBucket := end.Truncate(ms.timeIndex.bucketSize).Unix()

	for bucket := startBucket; bucket <= endBucket; bucket += int64(ms.timeIndex.bucketSize.Seconds()) {
		if ids, exists := ms.timeIndex.buckets[bucket]; exists {
			candidateIDs = append(candidateIDs, ids...)
		}
	}

	return candidateIDs
}

// removeDuplicates removes duplicate strings from slice
func (ms *MemoryEventStorage) removeDuplicates(slice []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(slice))

	for _, str := range slice {
		if !seen[str] {
			seen[str] = true
			result = append(result, str)
		}
	}

	return result
}

// Count returns the number of events matching the filter
func (ms *MemoryEventStorage) Count(ctx context.Context, filter *pb.Filter, timeRange *pb.TimeRange) (int64, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	count := int64(0)
	candidateIDs := ms.getCandidateIDs(filter, timeRange)

	for _, id := range candidateIDs {
		if event, exists := ms.events[id]; exists && ms.matchesFilter(event, filter) && ms.inTimeRange(event, timeRange) {
			count++
		}
	}

	return count, nil
}

// matchesFilter checks if event matches the filter
func (ms *MemoryEventStorage) matchesFilter(event *domain.UnifiedEvent, filter *pb.Filter) bool {
	if filter == nil {
		return true
	}

	// Query filter
	if filter.Query != "" {
		queryLower := strings.ToLower(filter.Query)
		if !strings.Contains(strings.ToLower(string(event.Type)), queryLower) &&
			!strings.Contains(strings.ToLower(event.Source), queryLower) &&
			!strings.Contains(strings.ToLower(event.GetSemanticIntent()), queryLower) {
			return false
		}
	}

	// Event type filter
	if len(filter.EventTypes) > 0 {
		matched := false
		for _, t := range filter.EventTypes {
			if string(event.Type) == t {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Source filter
	if len(filter.Sources) > 0 {
		matched := false
		for _, s := range filter.Sources {
			if event.Source == s {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Severity filter
	if len(filter.Severities) > 0 {
		matched := false
		severity := event.GetSeverity()
		for _, s := range filter.Severities {
			if severity == s {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Namespace filter
	if len(filter.Namespaces) > 0 && event.Entity != nil {
		matched := false
		for _, ns := range filter.Namespaces {
			if event.Entity.Namespace == ns {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// inTimeRange checks if event is within time range
func (ms *MemoryEventStorage) inTimeRange(event *domain.UnifiedEvent, timeRange *pb.TimeRange) bool {
	if timeRange == nil {
		return true
	}

	if timeRange.Start != nil && event.Timestamp.Before(timeRange.Start.AsTime()) {
		return false
	}

	if timeRange.End != nil && event.Timestamp.After(timeRange.End.AsTime()) {
		return false
	}

	return true
}

// Health returns storage health status
func (ms *MemoryEventStorage) Health() HealthStatus {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	status := pb.HealthStatus_HEALTH_STATUS_HEALTHY
	message := "Memory storage is healthy"

	// Check capacity
	utilizationPct := float64(len(ms.events)) / float64(ms.maxSize) * 100
	if ms.maxSize > 0 && utilizationPct > 90 {
		status = pb.HealthStatus_HEALTH_STATUS_DEGRADED
		message = fmt.Sprintf("Storage utilization high: %.1f%%", utilizationPct)
	}

	// Check if we're evicting too many events
	if ms.evictedCount > ms.totalStored/10 {
		status = pb.HealthStatus_HEALTH_STATUS_DEGRADED
		message = "High event eviction rate detected"
	}

	return HealthStatus{
		Status:      status,
		Message:     message,
		LastHealthy: time.Now(),
		Metrics: map[string]float64{
			"events_stored":     float64(len(ms.events)),
			"total_stored":      float64(ms.totalStored),
			"total_retrieved":   float64(ms.totalRetrieved),
			"query_count":       float64(ms.queryCount),
			"evicted_count":     float64(ms.evictedCount),
			"utilization_pct":   utilizationPct,
			"type_indexes":      float64(len(ms.typeIndex)),
			"source_indexes":    float64(len(ms.sourceIndex)),
			"namespace_indexes": float64(len(ms.namespaceIndex)),
			"time_buckets":      float64(len(ms.timeIndex.buckets)),
		},
	}
}

// Close closes the storage (no-op for memory storage)
func (ms *MemoryEventStorage) Close() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Clear all data
	ms.events = make(map[string]*domain.UnifiedEvent)
	ms.eventOrder = make([]string, 0)
	ms.typeIndex = make(map[domain.EventType][]string)
	ms.sourceIndex = make(map[string][]string)
	ms.namespaceIndex = make(map[string][]string)
	ms.timeIndex = newTimeIndex()

	return nil
}

// GetStatistics returns storage statistics
func (ms *MemoryEventStorage) GetStatistics() map[string]interface{} {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	return map[string]interface{}{
		"events_count":      len(ms.events),
		"max_size":          ms.maxSize,
		"total_stored":      ms.totalStored,
		"total_retrieved":   ms.totalRetrieved,
		"query_count":       ms.queryCount,
		"evicted_count":     ms.evictedCount,
		"retention_period":  ms.retentionPeriod.String(),
		"type_indexes":      len(ms.typeIndex),
		"source_indexes":    len(ms.sourceIndex),
		"namespace_indexes": len(ms.namespaceIndex),
		"time_buckets":      len(ms.timeIndex.buckets),
	}
}
