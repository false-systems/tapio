package correlation

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
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

	// Configuration
	config  MemoryStorageConfig
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

// timeIndex maintains correlations ordered by time with bounds
type timeIndex struct {
	entries    []timeEntry
	maxEntries int
	mu         sync.RWMutex
}

type timeEntry struct {
	id        string
	timestamp time.Time
}

// MemoryStorageConfig configures the memory storage
type MemoryStorageConfig struct {
	MaxSize                 int           // Maximum number of correlations to store
	MaxAge                  time.Duration // Maximum age of correlations
	MaxCorrelationsPerTrace int           // Maximum correlations per trace ID
	MaxTimeEntries          int           // Maximum entries in time index
	EvictionPolicy          string        // "lru" (default), "lfu", "ttl"
	MemoryPressureThreshold float64       // Memory pressure threshold (0.0 to 1.0)
}

// DefaultMemoryStorageConfig returns sensible defaults
func DefaultMemoryStorageConfig() MemoryStorageConfig {
	return MemoryStorageConfig{
		MaxSize:                 getConfigInt("MEMORY_STORAGE_MAX_SIZE", 10000),
		MaxAge:                  getConfigDuration("MEMORY_STORAGE_MAX_AGE", 24*time.Hour),
		MaxCorrelationsPerTrace: getConfigInt("MEMORY_STORAGE_MAX_PER_TRACE", 1000),
		MaxTimeEntries:          getConfigInt("MEMORY_STORAGE_MAX_TIME_ENTRIES", 10000),
		EvictionPolicy:          getConfigString("MEMORY_STORAGE_EVICTION_POLICY", "lru"),
		MemoryPressureThreshold: getConfigFloat("MEMORY_STORAGE_PRESSURE_THRESHOLD", 0.8),
	}
}

// NewMemoryStorage creates a new bounded memory storage
func NewMemoryStorage(logger *zap.Logger, config MemoryStorageConfig) *MemoryStorage {
	// Validate and apply defaults
	if config.MaxSize <= 0 {
		config.MaxSize = 10000
	}
	if config.MaxAge <= 0 {
		config.MaxAge = 24 * time.Hour
	}
	if config.MaxCorrelationsPerTrace <= 0 {
		config.MaxCorrelationsPerTrace = 1000
	}
	if config.MaxTimeEntries <= 0 {
		config.MaxTimeEntries = config.MaxSize
	}
	if config.EvictionPolicy == "" {
		config.EvictionPolicy = "lru"
	}
	if config.MemoryPressureThreshold <= 0 || config.MemoryPressureThreshold > 1 {
		config.MemoryPressureThreshold = 0.8
	}

	storage := &MemoryStorage{
		logger:       logger,
		correlations: make(map[string]*storedCorrelation),
		byTrace:      make(map[string][]string),
		byTime:       &timeIndex{entries: make([]timeEntry, 0), maxEntries: config.MaxTimeEntries},
		config:       config,
		maxSize:      config.MaxSize,
		maxAge:       config.MaxAge,
	}

	// Start memory pressure monitoring
	go storage.monitorMemoryPressure(config.MemoryPressureThreshold)

	return storage
}

// Store saves a correlation result with bounds checking
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

	// Update trace index with configurable bounds
	if result.TraceID != "" {
		correlationIDs := m.byTrace[result.TraceID]
		correlationIDs = append(correlationIDs, result.ID)

		// Use configurable limit from storage config
		maxPerTrace := m.getMaxCorrelationsPerTrace()
		if len(correlationIDs) > maxPerTrace {
			// Keep only the most recent correlations
			correlationIDs = correlationIDs[len(correlationIDs)-maxPerTrace:]
			m.logger.Debug("Trimmed trace correlations to bounds",
				zap.String("trace_id", result.TraceID),
				zap.Int("max_per_trace", maxPerTrace))
		}
		m.byTrace[result.TraceID] = correlationIDs
	}

	// Update time index with bounds checking
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
	accessedIDs := make([]string, 0, len(entries))

	for _, entry := range entries {
		if stored, exists := m.correlations[entry.id]; exists {
			results = append(results, stored.result)
			accessedIDs = append(accessedIDs, entry.id)
		}
	}

	// Asynchronously update access metadata without holding read lock
	go func(ids []string) {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, id := range ids {
			if stored, exists := m.correlations[id]; exists {
				stored.accessedAt = time.Now()
				stored.accessCount++
			}
		}
	}(accessedIDs)

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
	accessedIDs := make([]string, 0, len(correlationIDs))

	for _, id := range correlationIDs {
		if stored, exists := m.correlations[id]; exists {
			results = append(results, stored.result)
			accessedIDs = append(accessedIDs, id)
		}
	}

	// Asynchronously update access metadata without holding read lock
	go func(ids []string) {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, id := range ids {
			if stored, exists := m.correlations[id]; exists {
				stored.accessedAt = time.Now()
				stored.accessCount++
			}
		}
	}(accessedIDs)

	return results, nil
}

// GetByTimeRange retrieves correlations within a time range
func (m *MemoryStorage) GetByTimeRange(ctx context.Context, start, end time.Time) ([]*correlation.CorrelationResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.retrievals++

	var results []*correlation.CorrelationResult
	for _, stored := range m.correlations {
		if stored.result.StartTime.After(start) && stored.result.StartTime.Before(end) {
			// Race condition fix: Don't modify stored during read lock
			// Make a copy with updated access info
			updatedStored := *stored
			updatedStored.accessedAt = time.Now()
			updatedStored.accessCount++
			results = append(results, updatedStored.result)
		}
	}

	// Replace O(n²) bubble sort with efficient sort.Slice
	sort.Slice(results, func(i, j int) bool {
		return results[i].StartTime.After(results[j].StartTime)
	})

	// Asynchronously update access metadata without holding lock
	go func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, result := range results {
			if stored, exists := m.correlations[result.ID]; exists {
				stored.accessedAt = time.Now()
				stored.accessCount++
			}
		}
	}()

	return results, nil
}

// GetByResource retrieves correlations affecting a specific resource
func (m *MemoryStorage) GetByResource(ctx context.Context, resourceType, namespace, name string) ([]*correlation.CorrelationResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.retrievals++

	resourceName := fmt.Sprintf("%s/%s", namespace, name)
	var results []*correlation.CorrelationResult
	accessedIDs := make([]string, 0)

	for _, stored := range m.correlations {
		if stored.result.Impact != nil {
			for _, res := range stored.result.Impact.Resources {
				if res == resourceName {
					results = append(results, stored.result)
					accessedIDs = append(accessedIDs, stored.result.ID)
					break
				}
			}
		}
	}

	// Asynchronously update access metadata without holding read lock
	go func(ids []string) {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, id := range ids {
			if stored, exists := m.correlations[id]; exists {
				stored.accessedAt = time.Now()
				stored.accessCount++
			}
		}
	}(accessedIDs)

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

// evictOldest removes correlations based on configured eviction policy
func (m *MemoryStorage) evictOldest() {
	switch m.getEvictionPolicy() {
	case "lfu":
		m.evictLeastFrequent()
	case "ttl":
		m.evictByTTL()
	default: // "lru"
		m.evictLeastRecent()
	}
}

// evictLeastRecent removes the least recently accessed correlation (LRU)
func (m *MemoryStorage) evictLeastRecent() {
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

// evictLeastFrequent removes the least frequently accessed correlation (LFU)
func (m *MemoryStorage) evictLeastFrequent() {
	var leastID string
	var leastCount int = -1

	for id, stored := range m.correlations {
		if leastCount == -1 || stored.accessCount < leastCount {
			leastID = id
			leastCount = stored.accessCount
		}
	}

	if leastID != "" {
		m.removeCorrelation(leastID)
		m.evictions++
	}
}

// evictByTTL removes correlations older than MaxAge
func (m *MemoryStorage) evictByTTL() {
	cutoff := time.Now().Add(-m.maxAge)
	toRemove := []string{}

	for id, stored := range m.correlations {
		if stored.storedAt.Before(cutoff) {
			toRemove = append(toRemove, id)
		}
	}

	for _, id := range toRemove {
		m.removeCorrelation(id)
		m.evictions++
	}

	// If nothing was removed by TTL, fall back to LRU
	if len(toRemove) == 0 {
		m.evictLeastRecent()
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

// StorageMetrics provides typed metrics for memory storage
type StorageMetrics struct {
	CorrelationsStored int     `json:"correlations_stored"`
	TracesIndexed      int     `json:"traces_indexed"`
	TotalStores        int64   `json:"total_stores"`
	TotalRetrievals    int64   `json:"total_retrievals"`
	TotalEvictions     int64   `json:"total_evictions"`
	Capacity           int     `json:"capacity"`
	Utilization        float64 `json:"utilization"`
}

// GetMetrics returns storage metrics
func (m *MemoryStorage) GetMetrics() StorageMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	utilization := float64(0)
	if m.maxSize > 0 {
		utilization = float64(len(m.correlations)) / float64(m.maxSize) * 100
	}

	return StorageMetrics{
		CorrelationsStored: len(m.correlations),
		TracesIndexed:      len(m.byTrace),
		TotalStores:        m.stores,
		TotalRetrievals:    m.retrievals,
		TotalEvictions:     m.evictions,
		Capacity:           m.maxSize,
		Utilization:        utilization,
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

	// Use configured max entries
	if ti.maxEntries > 0 && len(ti.entries) > ti.maxEntries {
		// Keep only the most recent entries
		ti.entries = ti.entries[:ti.maxEntries]
	}
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

// Configuration helper methods

func (m *MemoryStorage) getMaxCorrelationsPerTrace() int {
	if m.config.MaxCorrelationsPerTrace > 0 {
		return m.config.MaxCorrelationsPerTrace
	}
	return 1000 // Default
}

func (m *MemoryStorage) getEvictionPolicy() string {
	if m.config.EvictionPolicy != "" {
		return m.config.EvictionPolicy
	}
	return "lru" // Default
}

// monitorMemoryPressure monitors system memory and triggers eviction if needed
func (m *MemoryStorage) monitorMemoryPressure(threshold float64) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Check memory pressure
		if m.isUnderMemoryPressure(threshold) {
			m.mu.Lock()
			// Evict 10% of entries when under pressure
			evictCount := len(m.correlations) / 10
			if evictCount < 1 {
				evictCount = 1
			}

			for i := 0; i < evictCount; i++ {
				m.evictOldest()
			}

			m.logger.Warn("Memory pressure detected, evicted correlations",
				zap.Int("evicted", evictCount),
				zap.Int("remaining", len(m.correlations)))
			m.mu.Unlock()
		}
	}
}

// isUnderMemoryPressure checks if storage is using too much memory
func (m *MemoryStorage) isUnderMemoryPressure(threshold float64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Simple heuristic: check if we're using more than threshold of max capacity
	utilization := float64(len(m.correlations)) / float64(m.maxSize)
	return utilization > threshold
}

// Helper functions for configuration

func getConfigInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultValue
}

func getConfigDuration(key string, defaultValue time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultValue
}

func getConfigString(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}

func getConfigFloat(key string, defaultValue float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultValue
}
