package base

import (
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/yairfalse/tapio/pkg/domain"
)

const (
	// maxCachedEntities limits entity span cache size (memory: ~1MB)
	maxCachedEntities = 10000
	// maxCachedSpans limits span parent cache size (memory: ~1MB)
	maxCachedSpans = 10000
)

// CausalityTracker tracks span IDs for causality chain propagation.
// Enables root cause analysis: "this event caused that event".
//
// Thread-safe for concurrent access from multiple observers.
// Uses LRU caches to bound memory (10K entities + 10K spans = ~2MB).
type CausalityTracker struct {
	mu sync.RWMutex

	// LRU cache: entity ID → most recent span ID
	// Example: "default/nginx-abc" → "span-123"
	// Used to link events: deployment update → pod restart
	entitySpans *lru.Cache[string, string]

	// LRU cache: span ID → parent span ID
	// Enables multi-hop causality: span3 → span2 → span1
	spanParents *lru.Cache[string, string]
}

// NewCausalityTracker creates a new causality tracker with LRU caches.
// Panics if cache initialization fails (programming error, should never happen).
func NewCausalityTracker() *CausalityTracker {
	entityCache, err := lru.New[string, string](maxCachedEntities)
	if err != nil {
		panic("failed to create entity LRU cache: " + err.Error())
	}

	spanCache, err := lru.New[string, string](maxCachedSpans)
	if err != nil {
		panic("failed to create span LRU cache: " + err.Error())
	}

	return &CausalityTracker{
		entitySpans: entityCache,
		spanParents: spanCache,
	}
}

// RecordEvent tracks span ID for an entity.
// Example: deployment observer records "default/nginx" → "span-deployment-1"
//
// Thread-safe for concurrent calls from multiple observers.
// LRU eviction: oldest entries evicted when cache exceeds 10K items.
func (c *CausalityTracker) RecordEvent(event *domain.ObserverEvent, primaryEntity string) {
	if event == nil || event.SpanID == "" || primaryEntity == "" {
		return // Ignore invalid inputs
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Track entity → span mapping (most recent span wins, LRU evicts old)
	c.entitySpans.Add(primaryEntity, event.SpanID)

	// Track span → parent span mapping (LRU evicts old)
	if event.ParentSpanID != "" {
		c.spanParents.Add(event.SpanID, event.ParentSpanID)
	}
}

// GetParentSpanForEntity retrieves parent span ID for an entity.
// Example: pod observer asks "what caused changes to default/nginx?"
// Returns: "span-deployment-1" (the deployment update span)
//
// Thread-safe for concurrent reads.
// Returns empty string if entity not in cache (evicted or never recorded).
// Uses Peek() to avoid mutating LRU cache (no access time update).
func (c *CausalityTracker) GetParentSpanForEntity(entityID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	spanID, ok := c.entitySpans.Peek(entityID)
	if !ok {
		return "" // Not found (evicted or never recorded)
	}
	return spanID
}

// BuildCausalityChain builds full causality chain for a span.
// Returns: [root_span, parent_span, current_span]
//
// Example:
//
//	BuildCausalityChain("span-oom-1")
//	→ ["span-deployment-1", "span-pod-1", "span-oom-1"]
//
// Interpretation: OOM caused by pod restart, which was caused by deployment.
//
// Thread-safe for concurrent reads.
// Chain may be incomplete if parent spans evicted from LRU cache.
// Uses Peek() to avoid mutating LRU cache (no access time update).
func (c *CausalityTracker) BuildCausalityChain(spanID string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	chain := []string{spanID}
	current := spanID

	// Walk up parent chain (max 10 hops to prevent infinite loops)
	for i := 0; i < 10; i++ {
		parent, exists := c.spanParents.Peek(current)
		if !exists {
			break // Reached root (or parent evicted from cache)
		}
		chain = append([]string{parent}, chain...) // Prepend parent
		current = parent
	}

	return chain
}
