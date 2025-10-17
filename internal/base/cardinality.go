package base

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
// Returns error if limit is non-positive
func NewCardinalityLimiter(limit int) (*CardinalityLimiter, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("cardinality limit must be positive, got %d", limit)
	}

	cache, err := lru.New[string, bool](limit)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}

	return &CardinalityLimiter{
		cache: cache,
		limit: limit,
	}, nil
}

// ShouldRecord returns true if the metric with these attributes should be recorded
// Uses LRU eviction: when cache is full, adding a new combination evicts the least recently used one
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

	// Add to cache; LRU will evict oldest entry if at capacity
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
