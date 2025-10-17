package base

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"go.opentelemetry.io/otel/attribute"
)

// CardinalityLimiter prevents metric cardinality explosion by tracking unique label combinations
// Uses LRU cache to bound memory and drop metrics when limit exceeded
type CardinalityLimiter struct {
	cache *lru.Cache[string, bool]
	limit int
	mu    sync.RWMutex
}

// NewCardinalityLimiter creates a cardinality limiter with the specified limit
func NewCardinalityLimiter(limit int) *CardinalityLimiter {
	cache, _ := lru.New[string, bool](limit)
	return &CardinalityLimiter{
		cache: cache,
		limit: limit,
	}
}

// ShouldRecord returns true if the metric with these attributes should be recorded
// Returns false if cardinality limit would be exceeded
func (cl *CardinalityLimiter) ShouldRecord(attrs []attribute.KeyValue) bool {
	hash := hashAttributes(attrs)

	cl.mu.RLock()
	if cl.cache.Contains(hash) {
		cl.mu.RUnlock()
		return true // Known combination, allow
	}
	cl.mu.RUnlock()

	cl.mu.Lock()
	defer cl.mu.Unlock()

	// Double-check after acquiring write lock
	if cl.cache.Contains(hash) {
		return true
	}

	// Check if adding would exceed limit
	if cl.cache.Len() >= cl.limit {
		return false // Limit reached, drop metric
	}

	// Add to cache
	cl.cache.Add(hash, true)
	return true
}

// Count returns the number of unique label combinations currently tracked
func (cl *CardinalityLimiter) Count() int {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	return cl.cache.Len()
}

// hashAttributes creates a stable hash of attribute key-value pairs
// Sorts attributes to ensure consistent hashing regardless of order
func hashAttributes(attrs []attribute.KeyValue) string {
	if len(attrs) == 0 {
		return "empty"
	}

	// Create sortable string representations
	pairs := make([]string, len(attrs))
	for i, attr := range attrs {
		pairs[i] = string(attr.Key) + "=" + attr.Value.Emit()
	}

	// Sort for stable hashing
	sort.Strings(pairs)

	// Hash sorted pairs
	h := sha256.New()
	for _, pair := range pairs {
		h.Write([]byte(pair))
	}

	return hex.EncodeToString(h.Sum(nil))
}
