package correlation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewEventRelationshipTracker(t *testing.T) {
	logger := zap.NewNop()
	k8sMap := createTestK8sMap(t)

	tracker := NewEventRelationshipTracker(k8sMap, logger)

	assert.NotNil(t, tracker)
	assert.NotNil(t, tracker.ResourceEvents)
	assert.NotNil(t, tracker.EventSequences)
	assert.NotNil(t, tracker.EventCausality)
	assert.NotNil(t, tracker.TemporalBuckets)
	assert.NotNil(t, tracker.k8sMap)
	assert.NotNil(t, tracker.logger)
}

func TestTrackEvent(t *testing.T) {
	logger := zap.NewNop()
	k8sMap := createTestK8sMap(t)
	tracker := NewEventRelationshipTracker(k8sMap, logger)

	// Test event with K8s context
	event1 := &domain.UnifiedEvent{
		ID:        "event-1",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		K8sContext: &domain.K8sContext{
			WorkloadKind: "Pod",
			Namespace:    "default",
			Name:         "test-pod",
		},
	}

	tracker.TrackEvent(event1)

	// Verify event was tracked by resource
	resKey := "Pod/default/test-pod"
	assert.Len(t, tracker.ResourceEvents[resKey], 1)
	assert.Equal(t, "event-1", tracker.ResourceEvents[resKey][0].ID)

	// Verify temporal tracking
	bucket := event1.Timestamp.Unix() / 10
	assert.Len(t, tracker.TemporalBuckets[bucket], 1)

	// Test event with causality
	event2 := &domain.UnifiedEvent{
		ID:        "event-2",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		K8sContext: &domain.K8sContext{
			WorkloadKind: "Pod",
			Namespace:    "default",
			Name:         "test-pod-2",
		},
		Correlation: &domain.CorrelationContext{
			ParentEventID: "event-1",
		},
	}

	tracker.TrackEvent(event2)

	// Verify causality was tracked
	assert.Contains(t, tracker.EventCausality["event-1"], "event-2")
}

func TestRelateEvents(t *testing.T) {
	logger := zap.NewNop()
	k8sMap := createTestK8sMap(t)
	tracker := NewEventRelationshipTracker(k8sMap, logger)

	now := time.Now()

	tests := []struct {
		name     string
		eventA   *domain.UnifiedEvent
		eventB   *domain.UnifiedEvent
		wantType string
		wantConf float64
		wantNil  bool
	}{
		{
			name: "same resource",
			eventA: &domain.UnifiedEvent{
				ID:        "event-1",
				Timestamp: now,
				K8sContext: &domain.K8sContext{
					WorkloadKind: "Pod",
					Namespace:    "default",
					Name:         "test-pod",
				},
			},
			eventB: &domain.UnifiedEvent{
				ID:        "event-2",
				Timestamp: now.Add(1 * time.Second),
				K8sContext: &domain.K8sContext{
					WorkloadKind: "Pod",
					Namespace:    "default",
					Name:         "test-pod",
				},
			},
			wantType: "same_resource",
			wantConf: 1.0,
		},
		{
			name: "temporal proximity",
			eventA: &domain.UnifiedEvent{
				ID:        "event-3",
				Timestamp: now,
				K8sContext: &domain.K8sContext{
					WorkloadKind: "Pod",
					Namespace:    "default",
					Name:         "pod-a",
				},
			},
			eventB: &domain.UnifiedEvent{
				ID:        "event-4",
				Timestamp: now.Add(3 * time.Second),
				K8sContext: &domain.K8sContext{
					WorkloadKind: "Pod",
					Namespace:    "default",
					Name:         "pod-b",
				},
			},
			wantType: "temporal_proximity",
			wantConf: 0.8,
		},
		{
			name: "temporal correlation with same severity",
			eventA: &domain.UnifiedEvent{
				ID:        "event-5",
				Timestamp: now,
				Severity:  domain.EventSeverityError,
				K8sContext: &domain.K8sContext{
					WorkloadKind: "Pod",
					Namespace:    "default",
					Name:         "pod-c",
				},
			},
			eventB: &domain.UnifiedEvent{
				ID:        "event-6",
				Timestamp: now.Add(20 * time.Second),
				Severity:  domain.EventSeverityError,
				K8sContext: &domain.K8sContext{
					WorkloadKind: "Pod",
					Namespace:    "default",
					Name:         "pod-d",
				},
			},
			wantType: "temporal_correlation",
			wantConf: 0.6,
		},
		{
			name: "no relationship",
			eventA: &domain.UnifiedEvent{
				ID:        "event-7",
				Timestamp: now,
				K8sContext: &domain.K8sContext{
					WorkloadKind: "Pod",
					Namespace:    "default",
					Name:         "pod-e",
				},
			},
			eventB: &domain.UnifiedEvent{
				ID:        "event-8",
				Timestamp: now.Add(5 * time.Minute),
				K8sContext: &domain.K8sContext{
					WorkloadKind: "Pod",
					Namespace:    "other",
					Name:         "pod-f",
				},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := tracker.RelateEvents(tt.eventA, tt.eventB)

			if tt.wantNil {
				assert.Nil(t, rel)
			} else {
				require.NotNil(t, rel)
				assert.Equal(t, tt.wantType, rel.Type)
				assert.Equal(t, tt.wantConf, rel.Confidence)
				assert.NotEmpty(t, rel.Evidence)
			}
		})
	}
}

func TestCausalRelationship(t *testing.T) {
	logger := zap.NewNop()
	k8sMap := createTestK8sMap(t)
	tracker := NewEventRelationshipTracker(k8sMap, logger)

	// Track parent event
	parentEvent := &domain.UnifiedEvent{
		ID:        "parent-event",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
	}

	// Track child event with causality
	childEvent := &domain.UnifiedEvent{
		ID:        "child-event",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now().Add(1 * time.Second),
		Correlation: &domain.CorrelationContext{
			ParentEventID: "parent-event",
		},
	}

	tracker.TrackEvent(parentEvent)
	tracker.TrackEvent(childEvent)

	// Check causal relationship
	rel := tracker.RelateEvents(parentEvent, childEvent)
	require.NotNil(t, rel)
	assert.Equal(t, "causal", rel.Type)
	assert.Equal(t, 0.95, rel.Confidence)
}

func TestCausalPattern(t *testing.T) {
	logger := zap.NewNop()
	k8sMap := createTestK8sMap(t)
	tracker := NewEventRelationshipTracker(k8sMap, logger)

	// OOM event
	oomEvent := &domain.UnifiedEvent{
		ID:        "oom-event",
		Type:      domain.EventType("kernel"),
		Timestamp: time.Now(),
		Kernel: &domain.KernelData{
			Syscall: "oom_kill",
		},
	}

	// Pod killed event
	podKilledEvent := &domain.UnifiedEvent{
		ID:        "pod-killed",
		Type:      domain.EventType("kubernetes"),
		Timestamp: time.Now().Add(2 * time.Second),
		Kubernetes: &domain.KubernetesData{
			Reason: "OOMKilled",
		},
	}

	rel := tracker.RelateEvents(oomEvent, podKilledEvent)
	require.NotNil(t, rel)
	assert.Equal(t, "causal_pattern", rel.Type)
	assert.Equal(t, 0.85, rel.Confidence)
}

func TestFindRelatedEvents(t *testing.T) {
	logger := zap.NewNop()
	k8sMap := createTestK8sMap(t)
	tracker := NewEventRelationshipTracker(k8sMap, logger)

	now := time.Now()

	// Track multiple events
	events := []*domain.UnifiedEvent{
		{
			ID:        "event-1",
			Type:      domain.EventTypeKubernetes,
			Timestamp: now,
			K8sContext: &domain.K8sContext{
				WorkloadKind: "Pod",
				Namespace:    "default",
				Name:         "test-pod",
			},
		},
		{
			ID:        "event-2",
			Type:      domain.EventTypeKubernetes,
			Timestamp: now.Add(1 * time.Second),
			K8sContext: &domain.K8sContext{
				WorkloadKind: "Pod",
				Namespace:    "default",
				Name:         "test-pod", // Same resource
			},
		},
		{
			ID:        "event-3",
			Type:      domain.EventTypeKubernetes,
			Timestamp: now.Add(2 * time.Second),
			K8sContext: &domain.K8sContext{
				WorkloadKind: "Pod",
				Namespace:    "default",
				Name:         "other-pod", // Different resource, same time
			},
		},
	}

	for _, e := range events {
		tracker.TrackEvent(e)
	}

	// Find related events for event-1
	related := tracker.FindRelatedEvents(events[0], 10)

	// Should find event-2 (same resource) and event-3 (temporal)
	assert.GreaterOrEqual(t, len(related), 2)

	// Verify event-2 is in related
	hasEvent2 := false
	for _, e := range related {
		if e.ID == "event-2" {
			hasEvent2 = true
			break
		}
	}
	assert.True(t, hasEvent2, "Should find event-2 as related")
}

func TestEventSequences(t *testing.T) {
	logger := zap.NewNop()
	k8sMap := createTestK8sMap(t)
	tracker := NewEventRelationshipTracker(k8sMap, logger)

	// Track cascade start event
	cascadeEvent := &domain.UnifiedEvent{
		ID:        "cascade-1",
		Type:      "error",
		Timestamp: time.Now(),
	}

	tracker.TrackEvent(cascadeEvent)

	// Check sequence was created
	sequences := tracker.GetEventSequences()
	require.Len(t, sequences, 1)
	assert.Equal(t, "cascade", sequences[0].Pattern)
	assert.Len(t, sequences[0].Events, 1)

	// Add another event to sequence
	followupEvent := &domain.UnifiedEvent{
		ID:        "cascade-2",
		Type:      "error",
		Timestamp: time.Now().Add(30 * time.Second),
	}

	tracker.TrackEvent(followupEvent)

	// Check sequence was updated
	sequences = tracker.GetEventSequences()
	require.Len(t, sequences, 1)
	assert.Len(t, sequences[0].Events, 2)
}

func TestCleanupOldData(t *testing.T) {
	logger := zap.NewNop()
	k8sMap := createTestK8sMap(t)
	tracker := NewEventRelationshipTracker(k8sMap, logger)

	now := time.Now()

	// Add old and new events
	oldEvent := &domain.UnifiedEvent{
		ID:        "old-event",
		Type:      domain.EventTypeKubernetes,
		Timestamp: now.Add(-2 * time.Hour),
		K8sContext: &domain.K8sContext{
			WorkloadKind: "Pod",
			Namespace:    "default",
			Name:         "old-pod",
		},
	}

	newEvent := &domain.UnifiedEvent{
		ID:        "new-event",
		Type:      domain.EventTypeKubernetes,
		Timestamp: now,
		K8sContext: &domain.K8sContext{
			WorkloadKind: "Pod",
			Namespace:    "default",
			Name:         "new-pod",
		},
	}

	tracker.TrackEvent(oldEvent)
	tracker.TrackEvent(newEvent)

	// Verify both are tracked
	assert.Len(t, tracker.ResourceEvents["Pod/default/old-pod"], 1)
	assert.Len(t, tracker.ResourceEvents["Pod/default/new-pod"], 1)

	// Cleanup with 1 hour cutoff
	cutoff := now.Add(-1 * time.Hour)
	tracker.CleanupOldData(cutoff)

	// Old event should be removed
	assert.Len(t, tracker.ResourceEvents["Pod/default/old-pod"], 0)
	assert.Len(t, tracker.ResourceEvents["Pod/default/new-pod"], 1)
}

func TestOwnerRelationship(t *testing.T) {
	logger := zap.NewNop()

	// Create K8s map with ownership relationships
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)

	// Add ownership: RS owns Pod
	rs := &ResourceRef{
		Kind:      "ReplicaSet",
		Namespace: "default",
		Name:      "test-rs",
		UID:       "rs-uid",
	}
	pod := &ResourceRef{
		Kind:      "Pod",
		Namespace: "default",
		Name:      "test-pod",
		UID:       "pod-uid",
	}
	loader.ownerCache.AddOwnership(rs, pod)

	k8sMap := NewK8sRelationshipMap(loader)
	tracker := NewEventRelationshipTracker(k8sMap, logger)

	// Create events for owner and child
	rsEvent := &domain.UnifiedEvent{
		ID:        "rs-event",
		Timestamp: time.Now(),
		K8sContext: &domain.K8sContext{
			WorkloadKind: "ReplicaSet",
			Namespace:    "default",
			Name:         "test-rs",
			UID:          "rs-uid",
		},
	}

	podEvent := &domain.UnifiedEvent{
		ID:        "pod-event",
		Timestamp: time.Now().Add(1 * time.Second),
		K8sContext: &domain.K8sContext{
			WorkloadKind: "Pod",
			Namespace:    "default",
			Name:         "test-pod",
			UID:          "pod-uid",
		},
	}

	rel := tracker.RelateEvents(rsEvent, podEvent)
	require.NotNil(t, rel)
	assert.Equal(t, "owner_child", rel.Type)
	assert.Equal(t, 0.95, rel.Confidence)
}

// Helper function to create test K8s map
func createTestK8sMap(t *testing.T) *K8sRelationshipMap {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)
	return NewK8sRelationshipMap(loader)
}
