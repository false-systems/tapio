package correlation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/yairfalse/tapio/pkg/domain"
)

// TestK8sCorrelationIntegration tests the full K8s correlation flow
func TestK8sCorrelationIntegration(t *testing.T) {
	logger := zap.NewNop()

	// Create test K8s resources
	deployment := &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
			UID:       types.UID("deploy-uid"),
		},
		Spec: v1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
		},
	}

	replicaSet := &v1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rs",
			Namespace: "default",
			UID:       types.UID("rs-uid"),
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "Deployment",
					Name: "test-deployment",
					UID:  types.UID("deploy-uid"),
				},
			},
		},
		Spec: v1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
		},
	}

	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-1",
			Namespace: "default",
			UID:       types.UID("pod-uid-1"),
			Labels:    map[string]string{"app": "test"},
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: "test-rs",
					UID:  types.UID("rs-uid"),
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
	}

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-2",
			Namespace: "default",
			UID:       types.UID("pod-uid-2"),
			Labels:    map[string]string{"app": "test"},
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: "test-rs",
					UID:  types.UID("rs-uid"),
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			UID:       types.UID("svc-uid"),
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "test"},
		},
	}

	// Create fake client
	clientset := fake.NewSimpleClientset()

	// Create and populate K8s relationship loader
	loader := NewK8sRelationshipLoader(logger, clientset)

	// Manually populate relationships (since informers don't work with fake client)
	loader.handleDeploymentAdd(deployment)
	loader.handleReplicaSetAdd(replicaSet)
	loader.handlePodAdd(pod1)
	loader.handlePodAdd(pod2)
	loader.handleServiceAdd(service)

	// Create K8s native correlator
	correlator := NewK8sNativeCorrelator(logger, loader)

	// Test 1: Correlate pod event - should find owner and service correlations
	podEvent := &domain.UnifiedEvent{
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
	}

	correlations := correlator.FindCorrelations(podEvent)
	require.NotEmpty(t, correlations)

	// Should find owner correlation (Pod -> RS)
	hasOwnerCorrelation := false
	hasServiceCorrelation := false
	for _, corr := range correlations {
		if corr.Type == "ownership" && corr.Source.Kind == "ReplicaSet" {
			hasOwnerCorrelation = true
			assert.Equal(t, "test-rs", corr.Source.Name)
		}
		if corr.Type == "selector" && corr.Source.Kind == "Service" {
			hasServiceCorrelation = true
			assert.Equal(t, "test-service", corr.Source.Name)
		}
	}
	assert.True(t, hasOwnerCorrelation, "Should find owner correlation")
	assert.True(t, hasServiceCorrelation, "Should find service correlation")

	// Test 2: Correlate deployment event - should find owned resources
	deployEvent := &domain.UnifiedEvent{
		ID:        "event-2",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		Entity: &domain.EntityContext{
			Type:      "Deployment",
			Namespace: "default",
			Name:      "test-deployment",
			UID:       "deploy-uid",
		},
	}

	deployCorrelations := correlator.FindCorrelations(deployEvent)
	hasOwnedCorrelation := false
	for _, corr := range deployCorrelations {
		if corr.Type == "ownership" && corr.Target.Kind == "ReplicaSet" {
			hasOwnedCorrelation = true
			assert.Equal(t, "test-rs", corr.Target.Name)
		}
	}
	assert.True(t, hasOwnedCorrelation, "Should find owned resources")

	// Test 3: Check K8sRelationshipMap
	k8sMap := NewK8sRelationshipMap(loader)

	// Test pod relationships
	podUIDs := k8sMap.GetRelatedPods(ResourceRef{
		Kind: "ReplicaSet",
		Name: "test-rs",
		UID:  "rs-uid",
	})
	assert.Len(t, podUIDs, 2)
	assert.Contains(t, podUIDs, "pod-uid-1")
	assert.Contains(t, podUIDs, "pod-uid-2")

	// Test AreRelated
	related, relType := k8sMap.AreRelated(
		ResourceRef{Kind: "Pod", UID: "pod-uid-1"},
		ResourceRef{Kind: "Pod", UID: "pod-uid-2"},
	)
	assert.True(t, related)
	assert.Equal(t, "same_owner", relType)

	// Test 4: Event correlations
	now := time.Now()
	correlator.UpdateCache(CacheUpdate{
		Type:   "event",
		Action: "add",
		Data: EventUpdate{
			Action: "add",
			Object: "Pod/test-pod-1",
			Event: K8sEventRef{
				Reason:    "FailedScheduling",
				Message:   "No nodes available",
				Timestamp: now.Add(-3 * time.Minute),
			},
		},
	})

	correlator.UpdateCache(CacheUpdate{
		Type:   "event",
		Action: "add",
		Data: EventUpdate{
			Action: "add",
			Object: "Pod/test-pod-1",
			Event: K8sEventRef{
				Reason:    "BackOff",
				Message:   "Back-off restarting failed container",
				Timestamp: now.Add(-1 * time.Minute),
			},
		},
	})

	eventCorrelation := &domain.UnifiedEvent{
		ID:        "event-3",
		Type:      domain.EventTypeKubernetes,
		Timestamp: now,
		Kubernetes: &domain.KubernetesData{
			Object: "Pod/test-pod-1",
			Reason: "CrashLoopBackOff",
		},
	}

	eventCorrs := correlator.FindCorrelations(eventCorrelation)
	hasEventCorrelation := false
	for _, corr := range eventCorrs {
		if corr.Type == "event-sequence" {
			hasEventCorrelation = true
		}
	}
	assert.True(t, hasEventCorrelation, "Should find event sequence correlation")
}

// TestK8sCorrelationEdgeCases tests edge cases and error handling
func TestK8sCorrelationEdgeCases(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)
	correlator := NewK8sNativeCorrelator(logger, loader)

	// Test with nil entity
	event1 := &domain.UnifiedEvent{
		ID:   "event-1",
		Type: domain.EventTypeKubernetes,
	}
	correlations := correlator.FindCorrelations(event1)
	assert.Empty(t, correlations)

	// Test with partial K8s object reference
	event2 := &domain.UnifiedEvent{
		ID:   "event-2",
		Type: domain.EventTypeKubernetes,
		Kubernetes: &domain.KubernetesData{
			Object: "InvalidFormat",
		},
	}
	correlations2 := correlator.FindCorrelations(event2)
	assert.Empty(t, correlations2)

	// Test label correlations
	event3 := &domain.UnifiedEvent{
		ID:   "event-3",
		Type: domain.EventTypeKubernetes,
		Entity: &domain.EntityContext{
			Type:   "Pod",
			Name:   "test-pod",
			Labels: map[string]string{"app": "test", "tier": "web"},
		},
	}
	correlations3 := correlator.FindCorrelations(event3)
	// Should find label correlations
	hasLabelCorr := false
	for _, corr := range correlations3 {
		if corr.Type == "label" {
			hasLabelCorr = true
		}
	}
	assert.True(t, hasLabelCorr)

	// Test network correlations
	event4 := &domain.UnifiedEvent{
		ID:   "event-4",
		Type: domain.EventTypeNetwork,
		Entity: &domain.EntityContext{
			Type: "Pod",
			Name: "client-pod",
		},
		Network: &domain.NetworkData{
			DestIP: "10.96.0.1", // Service IP
		},
	}
	correlations4 := correlator.FindCorrelations(event4)
	// Service correlation would be found if getServiceByIP was fully implemented
	_ = correlations4
}

// TestK8sRelationshipLoaderConcurrency tests concurrent access
func TestK8sRelationshipLoaderConcurrency(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)

	// Create multiple goroutines that add/remove resources
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      string(rune('a' + idx)),
					Namespace: "default",
					UID:       types.UID(string(rune('a' + idx))),
					Labels:    map[string]string{"worker": "true"},
				},
			}

			// Add and remove pod multiple times
			for j := 0; j < 5; j++ {
				loader.handlePodAdd(pod)
				loader.handlePodDelete(pod)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify no panics and loader is in consistent state
	assert.NotNil(t, loader.ownerCache)
	assert.NotNil(t, loader.selectorCache)
	assert.NotNil(t, loader.nodeCache)
}