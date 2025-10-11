package k8scontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestHandlePodAdd_InvalidType tests type assertion failure
func TestHandlePodAdd_InvalidType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type
	service.handlePodAdd("not a pod")

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandlePodUpdate_InvalidOldType tests old object type assertion failure
func TestHandlePodUpdate_InvalidOldType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type for old object
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Status:     corev1.PodStatus{PodIP: "10.244.1.10"},
	}
	service.handlePodUpdate("not a pod", pod)

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandlePodUpdate_InvalidNewType tests new object type assertion failure
func TestHandlePodUpdate_InvalidNewType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type for new object
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Status:     corev1.PodStatus{PodIP: "10.244.1.10"},
	}
	service.handlePodUpdate(pod, "not a pod")

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandlePodDelete_InvalidType tests type assertion failure
func TestHandlePodDelete_InvalidType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type
	service.handlePodDelete("not a pod")

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandleServiceAdd_InvalidType tests type assertion failure
func TestHandleServiceAdd_InvalidType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type
	service.handleServiceAdd("not a service")

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandleServiceUpdate_InvalidOldType tests old object type assertion failure
func TestHandleServiceUpdate_InvalidOldType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type for old object
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       corev1.ServiceSpec{ClusterIP: "10.96.0.1"},
	}
	service.handleServiceUpdate("not a service", svc)

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandleServiceUpdate_InvalidNewType tests new object type assertion failure
func TestHandleServiceUpdate_InvalidNewType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type for new object
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       corev1.ServiceSpec{ClusterIP: "10.96.0.1"},
	}
	service.handleServiceUpdate(svc, "not a service")

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandleServiceDelete_InvalidType tests type assertion failure
func TestHandleServiceDelete_InvalidType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type
	service.handleServiceDelete("not a service")

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandleDeploymentAdd_InvalidType tests type assertion failure
func TestHandleDeploymentAdd_InvalidType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type
	service.handleDeploymentAdd("not a deployment")

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandleDeploymentUpdate_InvalidOldType tests old object type assertion failure
func TestHandleDeploymentUpdate_InvalidOldType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type for old object
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
	service.handleDeploymentUpdate("not a deployment", dep)

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandleDeploymentUpdate_InvalidNewType tests new object type assertion failure
func TestHandleDeploymentUpdate_InvalidNewType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type for new object
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
	service.handleDeploymentUpdate(dep, "not a deployment")

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandleDeploymentDelete_InvalidType tests type assertion failure
func TestHandleDeploymentDelete_InvalidType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type
	service.handleDeploymentDelete("not a deployment")

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandleNodeAdd_InvalidType tests type assertion failure
func TestHandleNodeAdd_InvalidType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type
	service.handleNodeAdd("not a node")

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandleNodeUpdate_InvalidOldType tests old object type assertion failure
func TestHandleNodeUpdate_InvalidOldType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type for old object
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
	}
	service.handleNodeUpdate("not a node", node)

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandleNodeUpdate_InvalidNewType tests new object type assertion failure
func TestHandleNodeUpdate_InvalidNewType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type for new object
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
	}
	service.handleNodeUpdate(node, "not a node")

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}

// TestHandleNodeDelete_InvalidType tests type assertion failure
func TestHandleNodeDelete_InvalidType(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	// Pass wrong type
	service.handleNodeDelete("not a node")

	// Should not crash, no events enqueued
	waitForEvents()
	assert.Equal(t, 0, mockKV.len())
}
