package base

import (
	"sync"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CausalityTracker tracks span IDs for causality chain propagation.
// Enables root cause analysis: "this event caused that event".
//
// Thread-safe for concurrent access from multiple observers.
type CausalityTracker struct {
	mu sync.RWMutex

	// Maps entity ID → most recent span ID
	// Example: "default/nginx-abc" → "span-123"
	// Used to link events: deployment update → pod restart
	entitySpans map[string]string

	// Maps span ID → parent span ID
	// Enables multi-hop causality: span3 → span2 → span1
	spanParents map[string]string
}

// NewCausalityTracker creates a new causality tracker.
func NewCausalityTracker() *CausalityTracker {
	return &CausalityTracker{
		entitySpans: make(map[string]string),
		spanParents: make(map[string]string),
	}
}

// RecordEvent tracks span ID for an entity.
// Example: deployment observer records "default/nginx" → "span-deployment-1"
//
// Thread-safe for concurrent calls from multiple observers.
func (c *CausalityTracker) RecordEvent(event *domain.ObserverEvent, primaryEntity string) {
	if event == nil || event.SpanID == "" || primaryEntity == "" {
		return // Ignore invalid inputs
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Track entity → span mapping (most recent span wins)
	c.entitySpans[primaryEntity] = event.SpanID

	// Track span → parent span mapping
	if event.ParentSpanID != "" {
		c.spanParents[event.SpanID] = event.ParentSpanID
	}
}

// GetParentSpanForEntity retrieves parent span ID for an entity.
// Example: pod observer asks "what caused changes to default/nginx?"
// Returns: "span-deployment-1" (the deployment update span)
//
// Thread-safe for concurrent reads.
func (c *CausalityTracker) GetParentSpanForEntity(entityID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.entitySpans[entityID]
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
func (c *CausalityTracker) BuildCausalityChain(spanID string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	chain := []string{spanID}
	current := spanID

	// Walk up parent chain (max 10 hops to prevent infinite loops)
	for i := 0; i < 10; i++ {
		parent, exists := c.spanParents[current]
		if !exists {
			break // Reached root
		}
		chain = append([]string{parent}, chain...) // Prepend parent
		current = parent
	}

	return chain
}
