package lifecycle

import (
	"sync"
	"time"
)

// StateTracker tracks resource states and detects patterns
type StateTracker struct {
	mu       sync.RWMutex
	states   map[string]*TrackedResource   // UID -> state
	patterns map[string]*TransitionPattern // UID -> detected pattern
}

// NewStateTracker creates a new state tracker
func NewStateTracker() *StateTracker {
	return &StateTracker{
		states:   make(map[string]*TrackedResource),
		patterns: make(map[string]*TransitionPattern),
	}
}

// Track records a transition and updates state
func (st *StateTracker) Track(transition *LifecycleTransition) {
	st.mu.Lock()
	defer st.mu.Unlock()

	uid := string(transition.State.Resource.UID)

	// Get or create tracked resource
	tracked, exists := st.states[uid]
	if !exists {
		tracked = &TrackedResource{
			Identifier: transition.State.Resource,
			LastSeen:   time.Now(),
		}
		st.states[uid] = tracked
	}

	// Update state
	tracked.LastState = transition.State.ToState
	tracked.LastSeen = time.Now()

	// Track specific patterns
	switch transition.Type {
	case TransitionOOMKill:
		tracked.OOMCount++
	case TransitionEviction:
		tracked.EvictionCount++
	case TransitionCrashLoop:
		tracked.RestartCount++
	}

	// Detect patterns
	st.detectAndStorePattern(uid, tracked)
}

// DetectPattern checks for dangerous patterns
func (st *StateTracker) DetectPattern(resource ResourceIdentifier) *TransitionPattern {
	st.mu.RLock()
	defer st.mu.RUnlock()

	pattern, exists := st.patterns[string(resource.UID)]
	if !exists {
		return nil
	}

	// Return pattern if it's still relevant (within window)
	tracked, exists := st.states[string(resource.UID)]
	if exists && time.Since(tracked.LastSeen) < pattern.Window {
		return pattern
	}

	return nil
}

// detectAndStorePattern looks for concerning patterns
func (st *StateTracker) detectAndStorePattern(uid string, tracked *TrackedResource) {

	// OOM pattern - memory limits too low
	if tracked.OOMCount >= 3 {
		st.patterns[uid] = &TransitionPattern{
			Pattern:     "repeated_oom",
			Occurrences: tracked.OOMCount,
			Window:      5 * time.Minute,
			Prediction:  "memory_limits_insufficient",
		}
		return
	}

	// Eviction pattern - node pressure
	if tracked.EvictionCount >= 2 {
		st.patterns[uid] = &TransitionPattern{
			Pattern:     "repeated_eviction",
			Occurrences: tracked.EvictionCount,
			Window:      10 * time.Minute,
			Prediction:  "node_pressure_recurring",
		}
		return
	}

	// Crash loop pattern
	if tracked.RestartCount >= 5 {
		st.patterns[uid] = &TransitionPattern{
			Pattern:     "crash_loop",
			Occurrences: tracked.RestartCount,
			Window:      5 * time.Minute,
			Prediction:  "deployment_unstable",
		}
		return
	}
}

// Cleanup removes old tracked resources
func (st *StateTracker) Cleanup(maxAge time.Duration) {
	st.mu.Lock()
	defer st.mu.Unlock()

	now := time.Now()
	for uid, tracked := range st.states {
		if now.Sub(tracked.LastSeen) > maxAge {
			delete(st.states, uid)
			delete(st.patterns, uid)
		}
	}
}

// GetStats returns tracking statistics
func (st *StateTracker) GetStats() map[string]int {
	st.mu.RLock()
	defer st.mu.RUnlock()

	return map[string]int{
		"tracked_resources": len(st.states),
		"detected_patterns": len(st.patterns),
	}
}
