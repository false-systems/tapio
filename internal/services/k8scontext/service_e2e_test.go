package k8scontext

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// TestE2E_PodLifecycle verifies complete pod lifecycle from creation to deletion
func TestE2E_PodLifecycle(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// 1. Create pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-pod",
			Namespace: "default",
			UID:       "pod-123",
			Labels:    map[string]string{"app": "nginx"},
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: "nginx-rs",
				},
			},
		},
		Status: corev1.PodStatus{
			PodIP:  "10.244.1.10",
			HostIP: "192.168.1.100",
		},
	}

	// 2. Handler processes add event
	service.handlePodAdd(pod)
	waitForEvents()

	// 3. Verify pod metadata stored
	_, err := mockKV.Get("pod.ip.10.244.1.10")
	require.NoError(t, err, "Pod should be stored")

	// 4. Verify ownership stored
	ownerEntry, err := mockKV.Get("ownership.pod-123")
	require.NoError(t, err, "Owner should be stored")
	assert.NotNil(t, ownerEntry)

	// 5. Update pod (change labels)
	updatedPod := pod.DeepCopy()
	updatedPod.Labels["version"] = "v2"
	service.handlePodUpdate(pod, updatedPod)
	waitForEvents()

	// 6. Verify metadata updated
	_, err = mockKV.Get("pod.ip.10.244.1.10")
	require.NoError(t, err, "Pod should still be stored")

	// 7. Delete pod
	service.handlePodDelete(updatedPod)
	waitForEvents()

	// 8. Verify pod metadata deleted
	_, err = mockKV.Get("pod.ip.10.244.1.10")
	assert.Error(t, err, "Pod should be deleted")

	// 9. Verify ownership deleted
	_, err = mockKV.Get("ownership.pod-123")
	assert.Error(t, err, "Owner should be deleted")
}

// TestE2E_PodIPChange verifies IP change cleanup
func TestE2E_PodIPChange(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// 1. Create pod with initial IP
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "10.244.1.5",
		},
	}

	service.handlePodAdd(pod)
	waitForEvents()

	// 2. Verify stored at initial IP
	_, err := mockKV.Get("pod.ip.10.244.1.5")
	require.NoError(t, err)

	// 3. Update pod with new IP
	updatedPod := pod.DeepCopy()
	updatedPod.Status.PodIP = "10.244.1.6"
	service.handlePodUpdate(pod, updatedPod)
	waitForEvents()

	// 4. Verify old IP deleted
	_, err = mockKV.Get("pod.ip.10.244.1.5")
	assert.Error(t, err, "Old IP should be deleted")

	// 5. Verify new IP stored
	_, err = mockKV.Get("pod.ip.10.244.1.6")
	require.NoError(t, err, "New IP should be stored")
}

// TestE2E_ServiceLifecycle verifies complete service lifecycle
func TestE2E_ServiceLifecycle(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// 1. Create service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-service",
			Namespace: "default",
			Labels:    map[string]string{"app": "nginx"},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.50",
			Type:      corev1.ServiceTypeClusterIP,
		},
	}

	// 2. Handler processes add event
	service.handleServiceAdd(svc)
	waitForEvents()

	// 3. Verify service metadata stored
	_, err := mockKV.Get("service.ip.10.96.0.50")
	require.NoError(t, err, "Service should be stored")

	// 4. Update service (change type to LoadBalancer)
	updatedSvc := svc.DeepCopy()
	updatedSvc.Spec.Type = corev1.ServiceTypeLoadBalancer
	service.handleServiceUpdate(svc, updatedSvc)
	waitForEvents()

	// 5. Verify metadata updated
	_, err = mockKV.Get("service.ip.10.96.0.50")
	require.NoError(t, err, "Service should still be stored")

	// 6. Delete service
	service.handleServiceDelete(updatedSvc)
	waitForEvents()

	// 7. Verify service metadata deleted
	_, err = mockKV.Get("service.ip.10.96.0.50")
	assert.Error(t, err, "Service should be deleted")
}

// TestE2E_DeploymentLifecycle verifies complete deployment lifecycle
func TestE2E_DeploymentLifecycle(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// 1. Create deployment
	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-deployment",
			Namespace: "default",
			Labels:    map[string]string{"app": "nginx"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.21",
						},
					},
				},
			},
		},
	}

	// 2. Handler processes add event
	service.handleDeploymentAdd(deployment)
	waitForEvents()

	// 3. Verify deployment metadata stored
	_, err := mockKV.Get("deployment.default.nginx-deployment")
	require.NoError(t, err, "Deployment should be stored")

	// 4. Scale deployment (update replicas)
	updatedDeployment := deployment.DeepCopy()
	newReplicas := int32(5)
	updatedDeployment.Spec.Replicas = &newReplicas
	service.handleDeploymentUpdate(deployment, updatedDeployment)
	waitForEvents()

	// 5. Verify metadata updated
	_, err = mockKV.Get("deployment.default.nginx-deployment")
	require.NoError(t, err, "Deployment should still be stored")

	// 6. Update image (rolling update)
	updatedDeployment2 := updatedDeployment.DeepCopy()
	updatedDeployment2.Spec.Template.Spec.Containers[0].Image = "nginx:1.22"
	service.handleDeploymentUpdate(updatedDeployment, updatedDeployment2)
	waitForEvents()

	// 7. Verify metadata updated again
	_, err = mockKV.Get("deployment.default.nginx-deployment")
	require.NoError(t, err, "Deployment should still be stored")

	// 8. Delete deployment
	service.handleDeploymentDelete(updatedDeployment2)
	waitForEvents()

	// 9. Verify deployment metadata deleted
	_, err = mockKV.Get("deployment.default.nginx-deployment")
	assert.Error(t, err, "Deployment should be deleted")
}

// TestE2E_NodeLifecycle verifies complete node lifecycle
func TestE2E_NodeLifecycle(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// 1. Create node
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-node-1",
			Labels: map[string]string{
				"kubernetes.io/hostname":         "worker-node-1",
				"topology.kubernetes.io/zone":    "us-east-1a",
				"topology.kubernetes.io/region":  "us-east-1",
				"node-role.kubernetes.io/worker": "true",
			},
		},
	}

	// 2. Handler processes add event
	service.handleNodeAdd(node)
	waitForEvents()

	// 3. Verify node metadata stored
	entry, err := mockKV.Get("node.worker-node-1")
	require.NoError(t, err, "Node should be stored")
	assert.NotNil(t, entry)

	// 4. Update node (change zone - simulating node migration)
	updatedNode := node.DeepCopy()
	updatedNode.Labels["topology.kubernetes.io/zone"] = "us-east-1b"
	service.handleNodeUpdate(node, updatedNode)
	waitForEvents()

	// 5. Verify metadata updated
	_, err = mockKV.Get("node.worker-node-1")
	require.NoError(t, err, "Node should still be stored")

	// 6. Delete node (node decommission)
	service.handleNodeDelete(updatedNode)
	waitForEvents()

	// 7. Verify node metadata deleted
	_, err = mockKV.Get("node.worker-node-1")
	assert.Error(t, err, "Node should be deleted")
}

// TestE2E_ConcurrentEvents verifies handling of multiple concurrent events
func TestE2E_ConcurrentEvents(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Create 10 pods concurrently
	for i := 0; i < 10; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-" + string(rune(i+'0')),
				Namespace: "default",
				UID:       types.UID("uid-" + string(rune(i+'0'))),
			},
			Status: corev1.PodStatus{
				PodIP: "10.244.1." + string(rune(i+'0')),
			},
		}
		service.handlePodAdd(pod)
	}

	// Wait for all events to process
	time.Sleep(200 * time.Millisecond)

	// Verify all 10 pods stored (multi-index: 3 keys per pod = 30 keys)
	assert.Equal(t, 30, mockKV.len(), "All 10 pods should be stored with 3 keys each")

	// Delete all 10 pods concurrently (must match UIDs from creation)
	for i := 0; i < 10; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-" + string(rune(i+'0')),
				Namespace: "default",
				UID:       types.UID("uid-" + string(rune(i+'0'))),
			},
			Status: corev1.PodStatus{
				PodIP: "10.244.1." + string(rune(i+'0')),
			},
		}
		service.handlePodDelete(pod)
	}

	// Wait for all deletes to process
	time.Sleep(200 * time.Millisecond)

	// Verify all pods deleted
	assert.Equal(t, 0, mockKV.len(), "All pods should be deleted")
}

// TestE2E_MixedResourceTypes verifies handling multiple resource types concurrently
func TestE2E_MixedResourceTypes(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// 1. Create pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "10.244.1.20",
		},
	}
	service.handlePodAdd(pod)

	// 2. Create service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.20",
		},
	}
	service.handleServiceAdd(svc)

	// 3. Create deployment
	replicas := int32(2)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-deployment",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
	}
	service.handleDeploymentAdd(deployment)

	// 4. Create node
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-1",
		},
	}
	service.handleNodeAdd(node)

	// Wait for all events
	waitForEvents()

	// 5. Verify all resources stored
	_, err := mockKV.Get("pod.ip.10.244.1.20")
	assert.NoError(t, err, "Pod should be stored")

	_, err = mockKV.Get("service.ip.10.96.0.20")
	assert.NoError(t, err, "Service should be stored")

	_, err = mockKV.Get("deployment.default.app-deployment")
	assert.NoError(t, err, "Deployment should be stored")

	_, err = mockKV.Get("node.worker-1")
	assert.NoError(t, err, "Node should be stored")

	// 1 pod (3 keys: IP, UID, Name) + 1 service + 1 deployment + 1 node = 6 keys
	assert.Equal(t, 6, mockKV.len(), "Should have 6 KV entries (pod has 3 indexes)")
}

// TestE2E_OwnershipTracking verifies pod ownership tracking through lifecycle
func TestE2E_OwnershipTracking(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// 1. Create deployment
	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-app",
			Namespace: "production",
			UID:       "deploy-123",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
	}
	service.handleDeploymentAdd(deployment)
	waitForEvents()

	// 2. Create pod owned by deployment (via ReplicaSet)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-app-pod-1",
			Namespace: "production",
			UID:       "pod-456",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: "web-app-rs-abc",
					UID:  "rs-789",
				},
			},
		},
		Status: corev1.PodStatus{
			PodIP: "10.244.2.10",
		},
	}
	service.handlePodAdd(pod)
	waitForEvents()

	// 3. Verify both stored
	_, err := mockKV.Get("deployment.production.web-app")
	require.NoError(t, err, "Deployment should be stored")

	_, err = mockKV.Get("pod.ip.10.244.2.10")
	require.NoError(t, err, "Pod should be stored")

	_, err = mockKV.Get("ownership.pod-456")
	require.NoError(t, err, "Ownership should be stored")

	// 4. Delete pod (simulating pod crash)
	service.handlePodDelete(pod)
	waitForEvents()

	// 5. Verify pod deleted but deployment remains
	_, err = mockKV.Get("pod.ip.10.244.2.10")
	assert.Error(t, err, "Pod should be deleted")

	_, err = mockKV.Get("deployment.production.web-app")
	assert.NoError(t, err, "Deployment should still exist")

	// 6. Delete deployment
	service.handleDeploymentDelete(deployment)
	waitForEvents()

	// 7. Verify deployment deleted
	_, err = mockKV.Get("deployment.production.web-app")
	assert.Error(t, err, "Deployment should be deleted")
}

// TestE2E_EventEmission verifies end-to-end event emission workflow
func TestE2E_EventEmission(t *testing.T) {
	mockKV := newMockKV()

	// Create mock emitter to capture events
	mockEmitter := &mockEventEmitter{events: make([]*domain.ObserverEvent, 0)}

	// Create service with event emission enabled
	service := &Service{
		kv:          mockKV,
		logger:      base.NewLogger("test"),
		emitter:     mockEmitter,
		eventBuffer: make(chan func() error, 100),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	// Start event processing
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service.ctx = ctx
	service.workerWG.Add(1)
	go func() {
		defer service.workerWG.Done()
		service.processEvents(ctx)
	}()

	// Test 1: Deployment image change
	oldDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-deployment",
			Namespace: "production",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Image: "nginx:1.19"},
					},
				},
			},
		},
	}

	newDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-deployment",
			Namespace: "production",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Image: "nginx:1.20"},
					},
				},
			},
		},
	}

	service.handleDeploymentUpdate(oldDeployment, newDeployment)
	waitForEvents()

	// Verify image change event emitted
	require.GreaterOrEqual(t, len(mockEmitter.events), 1, "Should emit at least one event")
	imageChangeEvent := findEvent(mockEmitter.events, "deployment", "image_changed")
	require.NotNil(t, imageChangeEvent, "Should emit image_changed event")
	assert.Equal(t, "k8scontext", imageChangeEvent.Source)
	assert.Equal(t, "nginx:1.19", imageChangeEvent.K8sData.OldImage)
	assert.Equal(t, "nginx:1.20", imageChangeEvent.K8sData.NewImage)

	// Test 2: Pod crash loop
	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-pod",
			Namespace: "production",
		},
		Status: corev1.PodStatus{
			PodIP: "10.244.1.5",
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "app",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	}

	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-pod",
			Namespace: "production",
		},
		Status: corev1.PodStatus{
			PodIP: "10.244.1.5",
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					RestartCount: 5,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "CrashLoopBackOff",
						},
					},
				},
			},
		},
	}

	mockEmitter.events = make([]*domain.ObserverEvent, 0) // Reset
	service.handlePodUpdate(oldPod, newPod)
	waitForEvents()

	// Verify crash loop event emitted
	require.GreaterOrEqual(t, len(mockEmitter.events), 1, "Should emit crash loop event")
	crashEvent := findEvent(mockEmitter.events, "pod", "crash_loop")
	require.NotNil(t, crashEvent, "Should emit crash_loop event")
	assert.Equal(t, "CrashLoopBackOff", crashEvent.K8sData.Reason)
	assert.NotNil(t, crashEvent.ContainerData)
	assert.Equal(t, "app", crashEvent.ContainerData.ContainerName)
	assert.Equal(t, int32(5), crashEvent.ContainerData.RestartCount)

	// Test 3: Node not ready
	oldNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-1",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	newNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-1",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:    corev1.NodeReady,
					Status:  corev1.ConditionFalse,
					Message: "kubelet stopped posting node status",
				},
			},
		},
	}

	mockEmitter.events = make([]*domain.ObserverEvent, 0) // Reset
	service.handleNodeUpdate(oldNode, newNode)
	waitForEvents()

	// Verify node not ready event emitted
	require.GreaterOrEqual(t, len(mockEmitter.events), 1, "Should emit node event")
	nodeEvent := findEvent(mockEmitter.events, "node", "not_ready")
	require.NotNil(t, nodeEvent, "Should emit not_ready event")
	assert.Equal(t, "worker-1", nodeEvent.K8sData.ResourceName)

	// Test 4: Service type change
	oldService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-service",
			Namespace: "production",
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "10.0.1.5",
		},
	}

	newService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-service",
			Namespace: "production",
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeLoadBalancer,
			ClusterIP: "10.0.1.5",
		},
	}

	mockEmitter.events = make([]*domain.ObserverEvent, 0) // Reset
	service.handleServiceUpdate(oldService, newService)
	waitForEvents()

	// Verify service type change event emitted
	require.GreaterOrEqual(t, len(mockEmitter.events), 1, "Should emit service event")
	serviceEvent := findEvent(mockEmitter.events, "service", "type_changed")
	require.NotNil(t, serviceEvent, "Should emit type_changed event")
	assert.Contains(t, serviceEvent.K8sData.Message, "ClusterIP -> LoadBalancer")

	// Cleanup
	cancel()
	close(service.eventBuffer)
	service.workerWG.Wait()
}

// mockEventEmitter captures emitted events for testing
type mockEventEmitter struct {
	events []*domain.ObserverEvent
	mu     sync.Mutex
}

func (m *mockEventEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockEventEmitter) Close() error {
	return nil
}

func (m *mockEventEmitter) Name() string {
	return "mock-emitter"
}

func (m *mockEventEmitter) IsCritical() bool {
	return false
}

// findEvent searches for an event by type and subtype
func findEvent(events []*domain.ObserverEvent, eventType, subtype string) *domain.ObserverEvent {
	for _, evt := range events {
		if evt.Type == eventType && evt.Subtype == subtype {
			return evt
		}
	}
	return nil
}
