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

func TestNewK8sNativeCorrelator(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)

	correlator := NewK8sNativeCorrelator(logger, loader)

	assert.NotNil(t, correlator)
	assert.NotNil(t, correlator.logger)
	assert.NotNil(t, correlator.loader)
	assert.NotNil(t, correlator.ownerCache)
	assert.NotNil(t, correlator.selectorCache)
	assert.NotNil(t, correlator.eventCache)
}

func TestFindCorrelations(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)
	correlator := NewK8sNativeCorrelator(logger, loader)

	// Add test data to caches
	setupTestCaches(correlator)

	tests := []struct {
		name     string
		event    *domain.UnifiedEvent
		expected int
	}{
		{
			name: "event with owner relationship",
			event: &domain.UnifiedEvent{
				ID:        "event-1",
				Type:      domain.EventTypeKubernetes,
				Timestamp: time.Now(),
				Entity: &domain.EntityContext{
					Type:      "Pod",
					Namespace: "default",
					Name:      "test-pod-1",
					UID:       "pod-uid-1",
					Labels:    map[string]string{"app": "test"},
				},
			},
			expected: 3, // owner + selector + label correlations
		},
		{
			name: "event without entity",
			event: &domain.UnifiedEvent{
				ID:        "event-2",
				Type:      domain.EventTypeKubernetes,
				Timestamp: time.Now(),
			},
			expected: 0,
		},
		{
			name: "event with kubernetes context",
			event: &domain.UnifiedEvent{
				ID:        "event-3",
				Type:      domain.EventTypeKubernetes,
				Timestamp: time.Now(),
				Kubernetes: &domain.KubernetesData{
					Object: "Pod/test-pod-2",
					Reason: "Created",
				},
			},
			expected: 0, // No correlations without full entity info
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			correlations := correlator.FindCorrelations(tt.event)
			assert.Len(t, correlations, tt.expected)
		})
	}
}

func TestFindOwnerCorrelations(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)
	correlator := NewK8sNativeCorrelator(logger, loader)

	// Setup ownership relationships
	rs := &ResourceRef{
		Kind:      "ReplicaSet",
		Namespace: "default",
		Name:      "test-rs",
		UID:       "rs-uid-1",
	}
	pod := &ResourceRef{
		Kind:      "Pod",
		Namespace: "default",
		Name:      "test-pod-1",
		UID:       "pod-uid-1",
	}
	correlator.ownerCache.AddOwnership(rs, pod)

	// Test event for owned resource
	event := &domain.UnifiedEvent{
		Entity: &domain.EntityContext{
			Type:      "Pod",
			Namespace: "default",
			Name:      "test-pod-1",
			UID:       "pod-uid-1",
		},
	}

	correlations := correlator.findOwnerCorrelations(event)
	require.Len(t, correlations, 1)
	assert.Equal(t, "ownership", correlations[0].Type)
	assert.Equal(t, "ReplicaSet", correlations[0].Source.Kind)
	assert.Equal(t, "Pod", correlations[0].Target.Kind)
	assert.Equal(t, 1.0, correlations[0].Confidence)

	// Test event for owner resource
	event2 := &domain.UnifiedEvent{
		Entity: &domain.EntityContext{
			Type:      "ReplicaSet",
			Namespace: "default",
			Name:      "test-rs",
			UID:       "rs-uid-1",
		},
	}

	correlations2 := correlator.findOwnerCorrelations(event2)
	require.Len(t, correlations2, 1)
	assert.Equal(t, "ownership", correlations2[0].Type)
	assert.Equal(t, "test-pod-1", correlations2[0].Target.Name)
}

func TestFindSelectorCorrelations(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)
	correlator := NewK8sNativeCorrelator(logger, loader)

	// Setup selector relationships
	service := &ResourceRef{
		Kind:      "Service",
		Namespace: "default",
		Name:      "test-service",
		UID:       "svc-uid-1",
	}
	selector := map[string]string{"app": "test", "tier": "web"}
	correlator.selectorCache.AddSelector(service, selector)

	// Test event with matching labels
	event := &domain.UnifiedEvent{
		Entity: &domain.EntityContext{
			Type:      "Pod",
			Namespace: "default",
			Name:      "test-pod-1",
			UID:       "pod-uid-1",
			Labels:    map[string]string{"app": "test", "tier": "web", "version": "v1"},
		},
	}

	correlations := correlator.findSelectorCorrelations(event)
	require.Len(t, correlations, 1)
	assert.Equal(t, "selector", correlations[0].Type)
	assert.Equal(t, "Service", correlations[0].Source.Kind)
	assert.Equal(t, "test-service", correlations[0].Source.Name)
	assert.Equal(t, "bidirectional", correlations[0].Direction)

	// Test event with non-matching labels
	event2 := &domain.UnifiedEvent{
		Entity: &domain.EntityContext{
			Type:      "Pod",
			Namespace: "default",
			Name:      "other-pod",
			UID:       "pod-uid-2",
			Labels:    map[string]string{"app": "other"},
		},
	}

	correlations2 := correlator.findSelectorCorrelations(event2)
	assert.Len(t, correlations2, 0)
}

func TestFindEventCorrelations(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)
	correlator := NewK8sNativeCorrelator(logger, loader)

	// Add related events
	now := time.Now()
	event1 := &K8sEventRef{
		Reason:    "FailedScheduling",
		Message:   "No nodes available",
		Object:    "Pod/test-pod",
		Timestamp: now.Add(-2 * time.Minute),
	}
	event2 := &K8sEventRef{
		Reason:    "BackOff",
		Message:   "Back-off restarting failed container",
		Object:    "Pod/test-pod",
		Timestamp: now.Add(-1 * time.Minute),
	}

	correlator.eventCache.events["Pod/test-pod"] = []*K8sEventRef{event1, event2}

	// Test event correlation
	event := &domain.UnifiedEvent{
		Timestamp: now,
		Kubernetes: &domain.KubernetesData{
			Object: "Pod/test-pod",
			Reason: "CrashLoopBackOff",
		},
	}

	correlations := correlator.findEventCorrelations(event)
	require.Len(t, correlations, 2)
	assert.Equal(t, "event-sequence", correlations[0].Type)
	assert.Equal(t, 0.9, correlations[0].Confidence)
	assert.Contains(t, correlations[0].Reason, "Event sequence")
}

func TestUpdateCache(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)
	correlator := NewK8sNativeCorrelator(logger, loader)

	// Test ownership update
	ownerUpdate := CacheUpdate{
		Type:   "owner",
		Action: "add",
		Data: OwnershipUpdate{
			Action: "add",
			Owner: ResourceRef{
				Kind: "Deployment",
				UID:  "deploy-uid",
			},
			Owned: ResourceRef{
				Kind: "ReplicaSet",
				UID:  "rs-uid",
			},
		},
	}
	correlator.UpdateCache(ownerUpdate)

	// Verify ownership was added
	owned := correlator.ownerCache.GetOwned(&ResourceRef{UID: "deploy-uid"})
	require.Len(t, owned, 1)
	assert.Equal(t, "rs-uid", owned[0].UID)

	// Test selector update
	selectorUpdate := CacheUpdate{
		Type:   "selector",
		Action: "add",
		Data: SelectorUpdate{
			Action: "add",
			Resource: ResourceRef{
				Kind: "Service",
				UID:  "svc-uid",
			},
			Selector: map[string]string{"app": "test"},
		},
	}
	correlator.UpdateCache(selectorUpdate)

	// Test event update
	eventUpdate := CacheUpdate{
		Type:   "event",
		Action: "add",
		Data: EventUpdate{
			Action: "add",
			Object: "Pod/test-pod",
			Event: K8sEventRef{
				Reason:    "Created",
				Timestamp: time.Now(),
			},
		},
	}
	correlator.UpdateCache(eventUpdate)

	// Verify event was added
	events := correlator.eventCache.events["Pod/test-pod"]
	require.Len(t, events, 1)
	assert.Equal(t, "Created", events[0].Reason)
}

func TestK8sNativeCorrelator_MatchesSelector(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)
	correlator := NewK8sNativeCorrelator(logger, loader)

	tests := []struct {
		name         string
		labels       map[string]string
		selectorHash string
		expected     bool
	}{
		{
			name:         "matching labels",
			labels:       map[string]string{"app": "test", "tier": "web"},
			selectorHash: "app=test,tier=web,",
			expected:     true,
		},
		{
			name:         "partial match",
			labels:       map[string]string{"app": "test"},
			selectorHash: "app=test,tier=web,",
			expected:     false,
		},
		{
			name:         "no match",
			labels:       map[string]string{"app": "other"},
			selectorHash: "app=test,",
			expected:     false,
		},
		{
			name:         "nil labels",
			labels:       nil,
			selectorHash: "app=test,",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := correlator.matchesSelector(tt.labels, tt.selectorHash)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractResourceRef(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)
	correlator := NewK8sNativeCorrelator(logger, loader)

	tests := []struct {
		name     string
		event    *domain.UnifiedEvent
		expected *ResourceRef
	}{
		{
			name: "from entity",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Type:      "Pod",
					Namespace: "default",
					Name:      "test-pod",
					UID:       "pod-uid",
					Labels:    map[string]string{"app": "test"},
				},
			},
			expected: &ResourceRef{
				Kind:      "Pod",
				Namespace: "default",
				Name:      "test-pod",
				UID:       "pod-uid",
				Labels:    map[string]string{"app": "test"},
			},
		},
		{
			name: "from kubernetes object",
			event: &domain.UnifiedEvent{
				Kubernetes: &domain.KubernetesData{
					Object: "Pod/test-pod",
				},
			},
			expected: &ResourceRef{
				Kind: "Pod",
				Name: "test-pod",
			},
		},
		{
			name:     "no resource info",
			event:    &domain.UnifiedEvent{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := correlator.extractResourceRef(tt.event)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expected.Kind, result.Kind)
				assert.Equal(t, tt.expected.Name, result.Name)
				if tt.expected.Namespace != "" {
					assert.Equal(t, tt.expected.Namespace, result.Namespace)
				}
			}
		})
	}
}

// Helper function to setup test caches
func setupTestCaches(correlator *K8sNativeCorrelator) {
	// Add ownership relationships
	deployment := &ResourceRef{
		Kind:      "Deployment",
		Namespace: "default",
		Name:      "test-deployment",
		UID:       "deploy-uid-1",
	}
	rs := &ResourceRef{
		Kind:      "ReplicaSet",
		Namespace: "default",
		Name:      "test-rs",
		UID:       "rs-uid-1",
	}
	pod := &ResourceRef{
		Kind:      "Pod",
		Namespace: "default",
		Name:      "test-pod-1",
		UID:       "pod-uid-1",
		Labels:    map[string]string{"app": "test"},
	}

	correlator.ownerCache.AddOwnership(deployment, rs)
	correlator.ownerCache.AddOwnership(rs, pod)

	// Add selector relationships
	service := &ResourceRef{
		Kind:      "Service",
		Namespace: "default",
		Name:      "test-service",
		UID:       "svc-uid-1",
	}
	correlator.selectorCache.AddSelector(service, map[string]string{"app": "test"})

	// Add events
	correlator.eventCache.events["Pod/test-pod-1"] = []*K8sEventRef{
		{
			Reason:    "Created",
			Timestamp: time.Now().Add(-5 * time.Minute),
		},
	}
}
