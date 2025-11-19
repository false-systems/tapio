package base

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// RED: Test CausalityTracker creation
func TestNewCausalityTracker(t *testing.T) {
	tracker := NewCausalityTracker() // ❌ Doesn't exist yet - test will FAIL
	require.NotNil(t, tracker, "CausalityTracker should be created")
}

// RED: Test recording events for entities
func TestCausalityTracker_RecordEvent(t *testing.T) {
	tracker := NewCausalityTracker() // ❌ Doesn't exist yet

	event := &domain.ObserverEvent{
		ID:     domain.NewEventID(),
		SpanID: "span-deployment-1",
	}

	// Record event for entity
	tracker.RecordEvent(event, "default/nginx") // ❌ Doesn't exist yet
	// Should not panic
}

// RED: Test retrieving parent span for entity
func TestCausalityTracker_GetParentSpanForEntity(t *testing.T) {
	tracker := NewCausalityTracker() // ❌ Doesn't exist yet

	// Record deployment update
	deploymentEvent := &domain.ObserverEvent{
		ID:     domain.NewEventID(),
		SpanID: "span-dep-1",
	}
	tracker.RecordEvent(deploymentEvent, "default/nginx") // ❌ Doesn't exist yet

	// Get parent span for nginx deployment
	parentSpan := tracker.GetParentSpanForEntity("default/nginx") // ❌ Doesn't exist yet
	assert.Equal(t, "span-dep-1", parentSpan, "Should return most recent span for entity")
}

// RED: Test building causality chain
func TestCausalityTracker_BuildCausalityChain(t *testing.T) {
	tracker := NewCausalityTracker() // ❌ Doesn't exist yet

	// Create causality chain: deployment → pod_restart → oom_kill
	deploymentEvent := &domain.ObserverEvent{
		ID:     domain.NewEventID(),
		SpanID: "span-deployment",
		// No parent (root)
	}

	podEvent := &domain.ObserverEvent{
		ID:           domain.NewEventID(),
		SpanID:       "span-pod",
		ParentSpanID: "span-deployment",
	}

	oomEvent := &domain.ObserverEvent{
		ID:           domain.NewEventID(),
		SpanID:       "span-oom",
		ParentSpanID: "span-pod",
	}

	// Record events
	tracker.RecordEvent(deploymentEvent, "default/nginx")        // ❌ Doesn't exist yet
	tracker.RecordEvent(podEvent, "default/nginx-abc")           // ❌ Doesn't exist yet
	tracker.RecordEvent(oomEvent, "default/nginx-abc/container") // ❌ Doesn't exist yet

	// Build chain from OOM event
	chain := tracker.BuildCausalityChain("span-oom") // ❌ Doesn't exist yet

	// Should return: [root, parent, current] = [span-deployment, span-pod, span-oom]
	require.Len(t, chain, 3, "Chain should have 3 spans")
	assert.Equal(t, "span-deployment", chain[0], "Root span should be first")
	assert.Equal(t, "span-pod", chain[1], "Parent span should be second")
	assert.Equal(t, "span-oom", chain[2], "Current span should be last")
}

// RED: Test building chain with no parent (root event)
func TestCausalityTracker_BuildCausalityChain_Root(t *testing.T) {
	tracker := NewCausalityTracker() // ❌ Doesn't exist yet

	rootEvent := &domain.ObserverEvent{
		ID:     domain.NewEventID(),
		SpanID: "span-root",
		// No ParentSpanID (root)
	}

	tracker.RecordEvent(rootEvent, "default/nginx") // ❌ Doesn't exist yet

	chain := tracker.BuildCausalityChain("span-root") // ❌ Doesn't exist yet

	// Root event should return single-element chain
	require.Len(t, chain, 1, "Root event should have chain of length 1")
	assert.Equal(t, "span-root", chain[0])
}

// RED: Test building chain with multi-hop causality (5 levels deep)
func TestCausalityTracker_BuildCausalityChain_MultiHop(t *testing.T) {
	tracker := NewCausalityTracker() // ❌ Doesn't exist yet

	// Create 5-level chain: deploy → pod → oom → restart → failure
	events := []*domain.ObserverEvent{
		{ID: domain.NewEventID(), SpanID: "span-1", ParentSpanID: ""},
		{ID: domain.NewEventID(), SpanID: "span-2", ParentSpanID: "span-1"},
		{ID: domain.NewEventID(), SpanID: "span-3", ParentSpanID: "span-2"},
		{ID: domain.NewEventID(), SpanID: "span-4", ParentSpanID: "span-3"},
		{ID: domain.NewEventID(), SpanID: "span-5", ParentSpanID: "span-4"},
	}

	for i, evt := range events {
		tracker.RecordEvent(evt, "entity-"+string(rune(i))) // ❌ Doesn't exist yet
	}

	chain := tracker.BuildCausalityChain("span-5") // ❌ Doesn't exist yet

	require.Len(t, chain, 5, "Chain should have 5 spans")
	assert.Equal(t, "span-1", chain[0], "Root should be first")
	assert.Equal(t, "span-5", chain[4], "Leaf should be last")
}

// RED: Test infinite loop prevention (circular reference)
func TestCausalityTracker_BuildCausalityChain_CircularPrevention(t *testing.T) {
	tracker := NewCausalityTracker() // ❌ Doesn't exist yet

	// Create circular reference (shouldn't happen in practice, but test defense)
	events := []*domain.ObserverEvent{
		{ID: domain.NewEventID(), SpanID: "span-1", ParentSpanID: "span-3"},
		{ID: domain.NewEventID(), SpanID: "span-2", ParentSpanID: "span-1"},
		{ID: domain.NewEventID(), SpanID: "span-3", ParentSpanID: "span-2"},
	}

	for i, evt := range events {
		tracker.RecordEvent(evt, "entity-"+string(rune(i))) // ❌ Doesn't exist yet
	}

	chain := tracker.BuildCausalityChain("span-3") // ❌ Doesn't exist yet

	// Should stop at max depth (10 hops + current span = 11 max) to prevent infinite loop
	assert.LessOrEqual(t, len(chain), 11, "Chain should not exceed max depth (10 hops + current)")
}

// RED: Test thread-safe concurrent access
func TestCausalityTracker_ThreadSafe(t *testing.T) {
	tracker := NewCausalityTracker() // ❌ Doesn't exist yet

	// Record events concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			event := &domain.ObserverEvent{
				ID:     domain.NewEventID(),
				SpanID: "span-" + string(rune(idx)),
			}
			tracker.RecordEvent(event, "entity-"+string(rune(idx))) // ❌ Doesn't exist yet
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic (thread-safe)
}

// RED: Test nil event handling
func TestCausalityTracker_RecordEvent_NilEvent(t *testing.T) {
	tracker := NewCausalityTracker() // ❌ Doesn't exist yet

	// Should not panic on nil event
	tracker.RecordEvent(nil, "default/nginx") // ❌ Doesn't exist yet
}

// RED: Test empty entity ID
func TestCausalityTracker_RecordEvent_EmptyEntity(t *testing.T) {
	tracker := NewCausalityTracker() // ❌ Doesn't exist yet

	event := &domain.ObserverEvent{
		ID:     domain.NewEventID(),
		SpanID: "span-1",
	}

	// Should not panic on empty entity
	tracker.RecordEvent(event, "") // ❌ Doesn't exist yet
}

// RED: Test unknown span ID
func TestCausalityTracker_BuildCausalityChain_UnknownSpan(t *testing.T) {
	tracker := NewCausalityTracker() // ❌ Doesn't exist yet

	chain := tracker.BuildCausalityChain("unknown-span-id") // ❌ Doesn't exist yet

	// Should return single-element chain with unknown span
	require.Len(t, chain, 1)
	assert.Equal(t, "unknown-span-id", chain[0])
}
