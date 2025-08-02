package storage

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"go.uber.org/zap"
)

// MemoryStorage provides bounded in-memory storage for correlations
type MemoryStorage struct {
	logger *zap.Logger

	// Storage
	correlations map[string]*storedCorrelation
	byTrace      map[string][]string // traceID -> correlation IDs
	byTime       *timeIndex          // time-based index

	// Bounds
	maxSize int
	maxAge  time.Duration

	// Metrics
	evictions  int64
	stores     int64
	retrievals int64

	mu sync.RWMutex
}

// storedCorrelation wraps a correlation with metadata
type storedCorrelation struct {
	result      *correlation.CorrelationResult
	storedAt    time.Time
	accessedAt  time.Time
	accessCount int
}

// timeIndex maintains correlations ordered by time
type timeIndex struct {
	entries []timeEntry
	mu      sync.RWMutex
}

type timeEntry struct {
	id        string
	timestamp time.Time
}

// MemoryStorageConfig configures the memory storage
type MemoryStorageConfig struct {
	MaxSize int           // Maximum number of correlations to store
	MaxAge  time.Duration // Maximum age of correlations
}

// DefaultMemoryStorageConfig returns sensible defaults
func DefaultMemoryStorageConfig() MemoryStorageConfig {
	return MemoryStorageConfig{
		MaxSize: 10000,
		MaxAge:  24 * time.Hour,
	}
}

// NewMemoryStorage creates a new bounded memory storage
func NewMemoryStorage(logger *zap.Logger, config MemoryStorageConfig) *MemoryStorage {
	return &MemoryStorage{
		logger:       logger,
		correlations: make(map[string]*storedCorrelation),
		byTrace:      make(map[string][]string),
		byTime:       &timeIndex{entries: make([]timeEntry, 0)},
		maxSize:      config.MaxSize,
		maxAge:       config.MaxAge,
	}
}

// Store saves a correlation result
func (m *MemoryStorage) Store(ctx context.Context, result *correlation.CorrelationResult) error {
	if result == nil {
		return fmt.Errorf("correlation result is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if we need to evict
	if len(m.correlations) >= m.maxSize {
		m.evictOldest()
	}

	// Store the correlation
	stored := &storedCorrelation{
		result:      result,
		storedAt:    time.Now(),
		accessedAt:  time.Now(),
		accessCount: 0,
	}

	m.correlations[result.ID] = stored
	m.stores++

	// Update trace index
	if result.TraceID != "" {
		m.byTrace[result.TraceID] = append(m.byTrace[result.TraceID], result.ID)
	}

	// Update time index
	m.byTime.add(result.ID, result.StartTime)

	return nil
}

// GetRecent retrieves recent correlations
func (m *MemoryStorage) GetRecent(ctx context.Context, limit int) ([]*correlation.CorrelationResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.retrievals++

	// Get recent entries from time index
	entries := m.byTime.getRecent(limit)

	results := make([]*correlation.CorrelationResult, 0, len(entries))
	for _, entry := range entries {
		if stored, exists := m.correlations[entry.id]; exists {
			stored.accessedAt = time.Now()
			stored.accessCount++
			results = append(results, stored.result)
		}
	}

	return results, nil
}

// GetByTraceID retrieves correlations for a specific trace
func (m *MemoryStorage) GetByTraceID(ctx context.Context, traceID string) ([]*correlation.CorrelationResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.retrievals++

	correlationIDs, exists := m.byTrace[traceID]
	if !exists {
		return nil, nil
	}

	results := make([]*correlation.CorrelationResult, 0, len(correlationIDs))
	for _, id := range correlationIDs {
		if stored, exists := m.correlations[id]; exists {
			stored.accessedAt = time.Now()
			stored.accessCount++
			results = append(results, stored.result)
		}
	}

	return results, nil
}

// Cleanup removes old correlations
func (m *MemoryStorage) Cleanup(ctx context.Context, olderThan time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	removed := 0

	// Find correlations to remove
	toRemove := make([]string, 0)
	for id, stored := range m.correlations {
		if stored.storedAt.Before(cutoff) {
			toRemove = append(toRemove, id)
		}
	}

	// Remove them
	for _, id := range toRemove {
		m.removeCorrelation(id)
		removed++
	}

	if removed > 0 {
		m.logger.Info("Cleaned up old correlations",
			zap.Int("removed", removed),
			zap.Duration("older_than", olderThan),
		)
	}

	return nil
}

// evictOldest removes the oldest correlation (LRU)
func (m *MemoryStorage) evictOldest() {
	// Find least recently accessed
	var oldestID string
	var oldestAccess time.Time

	for id, stored := range m.correlations {
		if oldestID == "" || stored.accessedAt.Before(oldestAccess) {
			oldestID = id
			oldestAccess = stored.accessedAt
		}
	}

	if oldestID != "" {
		m.removeCorrelation(oldestID)
		m.evictions++
	}
}

// removeCorrelation removes a correlation and updates indices
func (m *MemoryStorage) removeCorrelation(id string) {
	stored, exists := m.correlations[id]
	if !exists {
		return
	}

	// Remove from main storage
	delete(m.correlations, id)

	// Remove from trace index
	if stored.result.TraceID != "" {
		m.removeFromTraceIndex(stored.result.TraceID, id)
	}

	// Remove from time index
	m.byTime.remove(id)
}

// removeFromTraceIndex removes a correlation ID from trace index
func (m *MemoryStorage) removeFromTraceIndex(traceID, correlationID string) {
	ids := m.byTrace[traceID]
	if len(ids) == 0 {
		return
	}

	// Filter out the correlation ID
	filtered := make([]string, 0, len(ids)-1)
	for _, id := range ids {
		if id != correlationID {
			filtered = append(filtered, id)
		}
	}

	if len(filtered) == 0 {
		delete(m.byTrace, traceID)
	} else {
		m.byTrace[traceID] = filtered
	}
}

// GetMetrics returns storage metrics
func (m *MemoryStorage) GetMetrics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"correlations_stored": len(m.correlations),
		"traces_indexed":      len(m.byTrace),
		"total_stores":        m.stores,
		"total_retrievals":    m.retrievals,
		"total_evictions":     m.evictions,
		"capacity":            m.maxSize,
		"utilization":         float64(len(m.correlations)) / float64(m.maxSize) * 100,
	}
}

// timeIndex methods

func (ti *timeIndex) add(id string, timestamp time.Time) {
	ti.mu.Lock()
	defer ti.mu.Unlock()

	ti.entries = append(ti.entries, timeEntry{
		id:        id,
		timestamp: timestamp,
	})

	// Keep sorted by timestamp (newest first)
	sort.Slice(ti.entries, func(i, j int) bool {
		return ti.entries[i].timestamp.After(ti.entries[j].timestamp)
	})
}

func (ti *timeIndex) remove(id string) {
	ti.mu.Lock()
	defer ti.mu.Unlock()

	filtered := make([]timeEntry, 0, len(ti.entries)-1)
	for _, entry := range ti.entries {
		if entry.id != id {
			filtered = append(filtered, entry)
		}
	}
	ti.entries = filtered
}

func (ti *timeIndex) getRecent(limit int) []timeEntry {
	ti.mu.RLock()
	defer ti.mu.RUnlock()

	if limit > len(ti.entries) {
		limit = len(ti.entries)
	}

	result := make([]timeEntry, limit)
	copy(result, ti.entries[:limit])
	return result
}
