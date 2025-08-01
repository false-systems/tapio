package correlation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewK8sRelationshipLoader(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()

	loader := NewK8sRelationshipLoader(logger, clientset)

	assert.NotNil(t, loader)
	assert.NotNil(t, loader.logger)
	assert.NotNil(t, loader.clientset)
	assert.NotNil(t, loader.ownerCache)
	assert.NotNil(t, loader.selectorCache)
	assert.NotNil(t, loader.nodeCache)
	assert.NotNil(t, loader.stopCh)
}

func TestHandlePodAdd(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       types.UID("pod-uid-1"),
			Labels:    map[string]string{"app": "test", "tier": "web"},
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: "test-rs",
					UID:  types.UID("rs-uid-1"),
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
	}

	loader.handlePodAdd(pod)

	// Verify ownership was added
	podRef := ResourceRef{
		Kind:      "Pod",
		Namespace: "default",
		Name:      "test-pod",
		UID:       "pod-uid-1",
		Labels:    map[string]string{"app": "test", "tier": "web"},
	}

	// Check owner cache
	owner := loader.ownerCache.GetOwner(&podRef)
	require.NotNil(t, owner)
	assert.Equal(t, "ReplicaSet", owner.Kind)
	assert.Equal(t, "test-rs", owner.Name)

	// Check node cache
	podsOnNode := loader.nodeCache.GetPodsOnNode("node-1")
	require.Len(t, podsOnNode, 1)
	assert.Equal(t, "test-pod", podsOnNode[0].Name)

	// Check pod's node
	node := loader.nodeCache.GetNodeForPod("pod-uid-1")
	assert.Equal(t, "node-1", node)
}

func TestHandleDeploymentAdd(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)

	deployment := &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
			UID:       types.UID("deploy-uid-1"),
			Labels:    map[string]string{"app": "test"},
		},
		Spec: v1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test", "component": "api"},
			},
		},
	}

	loader.handleDeploymentAdd(deployment)

	// Check if deployment selector was registered
	resources := loader.selectorCache.FindResourcesMatchingSelector(map[string]string{"app": "test", "component": "api"})
	require.Len(t, resources, 1)
	assert.Equal(t, "test-deployment", resources[0].Name)
}

func TestHandleReplicaSetAdd(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)

	rs := &v1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rs",
			Namespace: "default",
			UID:       types.UID("rs-uid-1"),
			Labels:    map[string]string{"app": "test"},
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "Deployment",
					Name: "test-deployment",
					UID:  types.UID("deploy-uid-1"),
				},
			},
		},
		Spec: v1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test", "pod-template-hash": "xyz"},
			},
		},
	}

	loader.handleReplicaSetAdd(rs)

	// Verify ownership was added
	rsRef := ResourceRef{
		Kind:      "ReplicaSet",
		Namespace: "default",
		Name:      "test-rs",
		UID:       "rs-uid-1",
		Labels:    map[string]string{"app": "test"},
	}

	owner := loader.ownerCache.GetOwner(&rsRef)
	require.NotNil(t, owner)
	assert.Equal(t, "Deployment", owner.Kind)
	assert.Equal(t, "test-deployment", owner.Name)

	// Verify selector was added
	resources := loader.selectorCache.FindResourcesMatchingSelector(map[string]string{"app": "test", "pod-template-hash": "xyz"})
	require.Len(t, resources, 1)
	assert.Equal(t, "test-rs", resources[0].Name)
}

func TestHandleServiceAdd(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			UID:       types.UID("svc-uid-1"),
			Labels:    map[string]string{"app": "test"},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "test", "tier": "web"},
		},
	}

	loader.handleServiceAdd(service)

	// Verify selector was added
	resources := loader.selectorCache.FindResourcesMatchingSelector(map[string]string{"app": "test", "tier": "web"})
	require.Len(t, resources, 1)
	assert.Equal(t, "test-service", resources[0].Name)
	assert.Equal(t, "Service", resources[0].Kind)
}

func TestHandlePodDelete(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)

	// First add a pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       types.UID("pod-uid-1"),
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: "test-rs",
					UID:  types.UID("rs-uid-1"),
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
	}

	loader.handlePodAdd(pod)

	// Verify pod was added
	podRef := ResourceRef{
		Kind:      "Pod",
		Namespace: "default",
		Name:      "test-pod",
		UID:       "pod-uid-1",
	}
	owner := loader.ownerCache.GetOwner(&podRef)
	require.NotNil(t, owner)

	// Delete the pod
	loader.handlePodDelete(pod)

	// Verify pod was removed
	owner = loader.ownerCache.GetOwner(&podRef)
	assert.Nil(t, owner)

	// Verify removed from node cache
	podsOnNode := loader.nodeCache.GetPodsOnNode("node-1")
	assert.Len(t, podsOnNode, 0)
}

func TestOwnershipCache(t *testing.T) {
	cache := &OwnershipCache{
		owners: make(map[string][]*ResourceRef),
		owned:  make(map[string]*ResourceRef),
	}

	owner := &ResourceRef{
		Kind: "Deployment",
		Name: "test-deployment",
		UID:  "deploy-uid",
	}

	owned1 := &ResourceRef{
		Kind: "ReplicaSet",
		Name: "test-rs-1",
		UID:  "rs-uid-1",
	}

	owned2 := &ResourceRef{
		Kind: "ReplicaSet",
		Name: "test-rs-2",
		UID:  "rs-uid-2",
	}

	// Test adding ownership
	cache.AddOwnership(owner, owned1)
	cache.AddOwnership(owner, owned2)

	// Test GetOwned
	owned := cache.GetOwned(owner)
	assert.Len(t, owned, 2)

	// Test GetOwner
	foundOwner := cache.GetOwner(owned1)
	require.NotNil(t, foundOwner)
	assert.Equal(t, owner.UID, foundOwner.UID)

	// Test RemoveResource
	cache.RemoveResource(owned1)
	owned = cache.GetOwned(owner)
	assert.Len(t, owned, 1)
	assert.Equal(t, "rs-uid-2", owned[0].UID)
}

func TestSelectorCache(t *testing.T) {
	cache := &SelectorCache{
		selectors: make(map[string][]*ResourceRef),
		matches:   make(map[string][]string),
	}

	service := &ResourceRef{
		Kind: "Service",
		Name: "test-service",
		UID:  "svc-uid",
	}

	selector := map[string]string{"app": "test", "tier": "web"}

	// Test adding selector
	cache.AddSelector(service, selector)

	// Test finding resources
	resources := cache.FindResourcesMatchingSelector(selector)
	require.Len(t, resources, 1)
	assert.Equal(t, "test-service", resources[0].Name)

	// Test updating resource labels
	pod := &ResourceRef{
		Kind: "Pod",
		Name: "test-pod",
		UID:  "pod-uid",
	}
	cache.UpdateResourceLabels(pod, map[string]string{"app": "test", "tier": "web"})

	// Pod should now match the selector
	selectorKey := makeSelectorKey(selector)
	assert.Len(t, cache.selectors[selectorKey], 2)

	// Test removing resource
	cache.RemoveResource(service)
	resources = cache.FindResourcesMatchingSelector(selector)
	assert.Len(t, resources, 1)
	assert.Equal(t, "test-pod", resources[0].Name)
}

func TestNodeCache(t *testing.T) {
	cache := &NodeResourceCache{
		nodePods: make(map[string][]ResourceRef),
		podNode:  make(map[string]string),
	}

	pod1 := ResourceRef{
		Kind: "Pod",
		Name: "pod-1",
		UID:  "pod-uid-1",
	}

	pod2 := ResourceRef{
		Kind: "Pod",
		Name: "pod-2",
		UID:  "pod-uid-2",
	}

	// Test adding pods to node
	cache.AddPodToNode("node-1", pod1)
	cache.AddPodToNode("node-1", pod2)

	// Test getting pods on node
	pods := cache.GetPodsOnNode("node-1")
	assert.Len(t, pods, 2)

	// Test getting node for pod
	node := cache.GetNodeForPod("pod-uid-1")
	assert.Equal(t, "node-1", node)

	// Test removing pod from node
	cache.RemovePodFromNode("node-1", pod1)
	pods = cache.GetPodsOnNode("node-1")
	assert.Len(t, pods, 1)
	assert.Equal(t, "pod-2", pods[0].Name)

	// Verify pod-node mapping was removed
	node = cache.GetNodeForPod("pod-uid-1")
	assert.Empty(t, node)
}

func TestK8sRelationshipMap(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)

	// Setup some relationships
	deployment := &ResourceRef{
		Kind: "Deployment",
		Name: "test-deployment",
		UID:  "deploy-uid",
	}
	rs := &ResourceRef{
		Kind: "ReplicaSet",
		Name: "test-rs",
		UID:  "rs-uid",
	}
	pod1 := &ResourceRef{
		Kind: "Pod",
		Name: "test-pod-1",
		UID:  "pod-uid-1",
	}
	pod2 := &ResourceRef{
		Kind: "Pod",
		Name: "test-pod-2",
		UID:  "pod-uid-2",
	}

	loader.ownerCache.AddOwnership(deployment, rs)
	loader.ownerCache.AddOwnership(rs, pod1)
	loader.ownerCache.AddOwnership(rs, pod2)

	k8sMap := NewK8sRelationshipMap(loader)

	// Test GetRelatedPods
	podUIDs := k8sMap.GetRelatedPods(*rs)
	assert.Len(t, podUIDs, 2)
	assert.Contains(t, podUIDs, "pod-uid-1")
	assert.Contains(t, podUIDs, "pod-uid-2")

	// Test AreRelated - same owner
	related, relType := k8sMap.AreRelated(*pod1, *pod2)
	assert.True(t, related)
	assert.Equal(t, "same_owner", relType)

	// Test AreRelated - owner/child
	related, relType = k8sMap.AreRelated(*rs, *pod1)
	assert.True(t, related)
	assert.Equal(t, "owner_child", relType)

	// Test AreRelated - not related
	unrelated := ResourceRef{
		Kind: "Service",
		Name: "test-service",
		UID:  "svc-uid",
	}
	related, _ = k8sMap.AreRelated(*pod1, unrelated)
	assert.False(t, related)
}

func TestMakeSelectorKey(t *testing.T) {
	tests := []struct {
		name     string
		selector map[string]string
		expected string
	}{
		{
			name:     "single label",
			selector: map[string]string{"app": "test"},
			expected: "app=test,",
		},
		{
			name:     "multiple labels",
			selector: map[string]string{"app": "test", "tier": "web"},
			// Note: map iteration order is not guaranteed, so we just check length
		},
		{
			name:     "empty selector",
			selector: map[string]string{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := makeSelectorKey(tt.selector)
			if tt.name == "single label" || tt.name == "empty selector" {
				assert.Equal(t, tt.expected, result)
			} else {
				// For multiple labels, just verify it contains the expected parts
				assert.Contains(t, result, "app=test")
				assert.Contains(t, result, "tier=web")
			}
		})
	}
}

func TestParseSelectorKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected map[string]string
	}{
		{
			name:     "single label",
			key:      "app=test,",
			expected: map[string]string{"app": "test"},
		},
		{
			name:     "multiple labels",
			key:      "app=test,tier=web,",
			expected: map[string]string{"app": "test", "tier": "web"},
		},
		{
			name:     "empty key",
			key:      "",
			expected: map[string]string{},
		},
		{
			name:     "no trailing comma",
			key:      "app=test",
			expected: map[string]string{"app": "test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSelectorKey(tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchesSelector(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		selector map[string]string
		expected bool
	}{
		{
			name:     "exact match",
			labels:   map[string]string{"app": "test", "tier": "web"},
			selector: map[string]string{"app": "test", "tier": "web"},
			expected: true,
		},
		{
			name:     "subset match",
			labels:   map[string]string{"app": "test", "tier": "web", "version": "v1"},
			selector: map[string]string{"app": "test", "tier": "web"},
			expected: true,
		},
		{
			name:     "no match",
			labels:   map[string]string{"app": "other"},
			selector: map[string]string{"app": "test"},
			expected: false,
		},
		{
			name:     "missing label",
			labels:   map[string]string{"app": "test"},
			selector: map[string]string{"app": "test", "tier": "web"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesSelector(tt.labels, tt.selector)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandleEndpointAdd(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)

	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP: "10.0.0.1",
						TargetRef: &corev1.ObjectReference{
							Kind:      "Pod",
							Name:      "test-pod-1",
							Namespace: "default",
						},
					},
				},
			},
		},
	}

	// Should not panic and should log appropriately
	loader.handleEndpointAdd(endpoints)
}

func TestHandleUpdateMethods(t *testing.T) {
	logger := zap.NewNop()
	clientset := fake.NewSimpleClientset()
	loader := NewK8sRelationshipLoader(logger, clientset)

	// Test pod update
	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
			UID:  types.UID("pod-uid-1"),
		},
	}
	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-pod",
			UID:    types.UID("pod-uid-1"),
			Labels: map[string]string{"updated": "true"},
		},
	}

	// Should handle update by removing old and adding new
	loader.handlePodUpdate(oldPod, newPod)

	// Test deployment update
	oldDeploy := createTestDeployment("test-deploy", "default")
	newDeploy := createTestDeployment("test-deploy", "default")
	newDeploy.Spec.Selector.MatchLabels["version"] = "v2"

	loader.handleDeploymentUpdate(oldDeploy, newDeploy)

	// Verify new selector is registered
	resources := loader.selectorCache.FindResourcesMatchingSelector(newDeploy.Spec.Selector.MatchLabels)
	assert.Len(t, resources, 1)
}

// Helper functions to create test resources
func createTestDeployment(name, namespace string) *v1.Deployment {
	return &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("deploy-uid"),
			Labels:    map[string]string{"app": "test"},
		},
		Spec: v1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test", "version": "v1"},
			},
		},
	}
}

func TestConcurrentCacheAccess(t *testing.T) {
	// Test concurrent access to caches
	cache := &OwnershipCache{
		owners: make(map[string][]*ResourceRef),
		owned:  make(map[string]*ResourceRef),
	}

	owner := &ResourceRef{
		Kind: "Deployment",
		Name: "test-deployment",
		UID:  "deploy-uid",
	}

	// Run concurrent operations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			owned := &ResourceRef{
				Kind: "Pod",
				Name: string(rune(idx)),
				UID:  string(rune(idx)),
			}
			cache.AddOwnership(owner, owned)
			cache.GetOwner(owned)
			cache.GetOwned(owner)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all operations completed successfully
	owned := cache.GetOwned(owner)
	assert.Len(t, owned, 10)
}
