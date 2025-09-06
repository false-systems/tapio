package lifecycle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
)

func TestStateTracker_Track(t *testing.T) {
	tracker := NewStateTracker()

	resource := ResourceIdentifier{
		Kind:      "Pod",
		Name:      "test-pod",
		Namespace: "default",
		UID:       "uid-123",
	}

	// Track an OOM kill
	transition := &LifecycleTransition{
		Type: TransitionOOMKill,
		State: StateChange{
			Resource:  resource,
			FromState: "running",
			ToState:   "oom_killed",
		},
	}

	tracker.Track(transition)

	// Verify tracking
	tracked, exists := tracker.states[string(resource.UID)]
	assert.True(t, exists)
	assert.Equal(t, "oom_killed", tracked.LastState)
	assert.Equal(t, 1, tracked.OOMCount)
}

func TestStateTracker_DetectPattern(t *testing.T) {
	tracker := NewStateTracker()

	resource := ResourceIdentifier{
		Kind:      "Pod",
		Name:      "test-pod",
		Namespace: "default",
		UID:       "uid-123",
	}

	// Track multiple OOM kills
	for i := 0; i < 3; i++ {
		transition := &LifecycleTransition{
			Type: TransitionOOMKill,
			State: StateChange{
				Resource:  resource,
				FromState: "running",
				ToState:   "oom_killed",
			},
		}
		tracker.Track(transition)
	}

	// Should detect OOM pattern
	pattern := tracker.DetectPattern(resource)
	assert.NotNil(t, pattern)
	assert.Equal(t, "repeated_oom", pattern.Pattern)
	assert.Equal(t, "memory_limits_insufficient", pattern.Prediction)
	assert.Equal(t, 3, pattern.Occurrences)
}

func TestStateTracker_EvictionPattern(t *testing.T) {
	tracker := NewStateTracker()

	resource := ResourceIdentifier{
		Kind:      "Pod",
		Name:      "test-pod",
		Namespace: "default",
		UID:       "uid-456",
	}

	// Track multiple evictions
	for i := 0; i < 2; i++ {
		transition := &LifecycleTransition{
			Type: TransitionEviction,
			State: StateChange{
				Resource:  resource,
				FromState: "running",
				ToState:   "evicted",
			},
		}
		tracker.Track(transition)
	}

	// Should detect eviction pattern
	pattern := tracker.DetectPattern(resource)
	assert.NotNil(t, pattern)
	assert.Equal(t, "repeated_eviction", pattern.Pattern)
	assert.Equal(t, "node_pressure_recurring", pattern.Prediction)
}

func TestStateTracker_CrashLoopPattern(t *testing.T) {
	tracker := NewStateTracker()

	resource := ResourceIdentifier{
		Kind:      "Pod",
		Name:      "test-pod",
		Namespace: "default",
		UID:       "uid-789",
	}

	// Track multiple restarts
	for i := 0; i < 5; i++ {
		transition := &LifecycleTransition{
			Type: TransitionCrashLoop,
			State: StateChange{
				Resource:  resource,
				FromState: "restarting",
				ToState:   "crash_loop",
			},
		}
		tracker.Track(transition)
	}

	// Should detect crash loop pattern
	pattern := tracker.DetectPattern(resource)
	assert.NotNil(t, pattern)
	assert.Equal(t, "crash_loop", pattern.Pattern)
	assert.Equal(t, "deployment_unstable", pattern.Prediction)
}

func TestStateTracker_Cleanup(t *testing.T) {
	tracker := NewStateTracker()

	// Add old resource
	oldResource := ResourceIdentifier{UID: "old-uid"}
	tracker.states[string(oldResource.UID)] = &TrackedResource{
		Identifier: oldResource,
		LastSeen:   time.Now().Add(-2 * time.Hour),
	}

	// Add recent resource
	newResource := ResourceIdentifier{UID: "new-uid"}
	tracker.states[string(newResource.UID)] = &TrackedResource{
		Identifier: newResource,
		LastSeen:   time.Now(),
	}

	// Cleanup old resources (older than 1 hour)
	tracker.Cleanup(1 * time.Hour)

	// Old resource should be gone
	_, exists := tracker.states[string(oldResource.UID)]
	assert.False(t, exists)

	// New resource should still exist
	_, exists = tracker.states[string(newResource.UID)]
	assert.True(t, exists)
}

func TestStateTracker_GetStats(t *testing.T) {
	tracker := NewStateTracker()

	// Add some tracked resources
	tracker.states["uid1"] = &TrackedResource{}
	tracker.states["uid2"] = &TrackedResource{}
	tracker.patterns["uid1"] = &TransitionPattern{}

	stats := tracker.GetStats()
	assert.Equal(t, 2, stats["tracked_resources"])
	assert.Equal(t, 1, stats["detected_patterns"])
}

func TestStateTracker_NoPatternForMinorIssues(t *testing.T) {
	tracker := NewStateTracker()

	resource := ResourceIdentifier{
		Kind:      "Pod",
		Name:      "test-pod",
		Namespace: "default",
		UID:       "uid-minor",
	}

	// Track only 1 OOM (not enough for pattern)
	transition := &LifecycleTransition{
		Type: TransitionOOMKill,
		State: StateChange{
			Resource:  resource,
			FromState: "running",
			ToState:   "oom_killed",
		},
	}
	tracker.Track(transition)

	// Should not detect pattern yet
	pattern := tracker.DetectPattern(resource)
	assert.Nil(t, pattern)
}

func TestStateTracker_PatternExpiration(t *testing.T) {
	tracker := NewStateTracker()

	resource := ResourceIdentifier{
		Kind:      "Pod",
		Name:      "test-pod",
		Namespace: "default",
		UID:       types.UID("uid-expire"),
	}

	// Manually add an expired pattern
	tracker.patterns[string(resource.UID)] = &TransitionPattern{
		Pattern:     "test_pattern",
		Occurrences: 5,
		Window:      1 * time.Millisecond, // Very short window
		Prediction:  "test_prediction",
	}

	// Add tracked resource with old timestamp
	tracker.states[string(resource.UID)] = &TrackedResource{
		Identifier: resource,
		LastSeen:   time.Now().Add(-1 * time.Second), // Outside window
	}

	// Pattern should be expired
	pattern := tracker.DetectPattern(resource)
	assert.Nil(t, pattern)
}
