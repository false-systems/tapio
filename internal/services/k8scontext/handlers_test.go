package k8scontext

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// newTestService creates a service with worker for testing
func newTestService(t *testing.T, kv *mockKV) (*Service, context.CancelFunc) {
	service := &Service{
		kv:          kv,
		eventBuffer: make(chan func() error, 10),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	// Start worker
	ctx, cancel := context.WithCancel(context.Background())
	go service.processEvents(ctx)

	return service, cancel
}

// waitForEvents waits for async event processing to complete
func waitForEvents() {
	time.Sleep(50 * time.Millisecond)
}

// TestHandlePodAdd verifies pod addition stores metadata in KV
func TestHandlePodAdd(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "test"},
		},
		Status: corev1.PodStatus{
			PodIP:  "10.244.1.5",
			HostIP: "192.168.1.10",
		},
	}

	service.handlePodAdd(pod)
	waitForEvents()

	// Verify pod was stored
	entry, err := mockKV.Get("pod.ip.10.244.1.5")
	require.NoError(t, err, "Pod should be stored in KV")
	assert.NotNil(t, entry)
}

// TestHandlePodAdd_WithoutIP verifies pods without IP are skipped
func TestHandlePodAdd_WithoutIP(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "", // No IP yet
		},
	}

	service.handlePodAdd(pod)
	waitForEvents()

	// Verify nothing was stored
	assert.Equal(t, 0, len(mockKV.data), "Pod without IP should not be stored")
}

// TestHandlePodUpdate verifies pod updates refresh metadata
func TestHandlePodUpdate(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "old"},
		},
		Status: corev1.PodStatus{
			PodIP: "10.244.1.5",
		},
	}

	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "new"},
		},
		Status: corev1.PodStatus{
			PodIP: "10.244.1.5",
		},
	}

	service.handlePodUpdate(oldPod, newPod)
	waitForEvents()

	// Verify updated pod metadata was stored
	entry, err := mockKV.Get("pod.ip.10.244.1.5")
	require.NoError(t, err)
	assert.NotNil(t, entry)
}

// TestHandlePodUpdate_IPChange verifies IP changes delete old entry
func TestHandlePodUpdate_IPChange(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "10.244.1.5",
		},
	}

	// Pre-populate KV with old IP
	service.storePodMetadata(oldPod)

	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "10.244.1.6", // IP changed
		},
	}

	service.handlePodUpdate(oldPod, newPod)
	waitForEvents()

	// Verify old IP was deleted
	_, err := mockKV.Get("pod.ip.10.244.1.5")
	assert.Error(t, err, "Old IP should be deleted")

	// Verify new IP was stored
	entry, err := mockKV.Get("pod.ip.10.244.1.6")
	require.NoError(t, err, "New IP should be stored")
	assert.NotNil(t, entry)
}

// TestHandlePodDelete verifies pod deletion removes metadata
func TestHandlePodDelete(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "10.244.1.5",
		},
	}

	// Pre-populate KV
	service.storePodMetadata(pod)
	require.Equal(t, 1, len(mockKV.data))

	service.handlePodDelete(pod)
	waitForEvents()

	// Verify pod was deleted
	_, err := mockKV.Get("pod.ip.10.244.1.5")
	assert.Error(t, err, "Pod should be deleted from KV")
	assert.Equal(t, 0, len(mockKV.data))
}

// TestHandlePodDelete_WithoutIP verifies deletes without IP are skipped
func TestHandlePodDelete_WithoutIP(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "", // No IP
		},
	}

	service.handlePodDelete(pod)
	waitForEvents()

	// Should not error or panic
	assert.Equal(t, 0, len(mockKV.data))
}

// TestHandleServiceAdd verifies service addition stores metadata in KV
func TestHandleServiceAdd(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubernetes",
			Namespace: "default",
			Labels:    map[string]string{"component": "apiserver"},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.1",
			Type:      corev1.ServiceTypeClusterIP,
		},
	}

	service.handleServiceAdd(svc)
	waitForEvents()

	// Verify service was stored
	entry, err := mockKV.Get("service.ip.10.96.0.1")
	require.NoError(t, err, "Service should be stored in KV")
	assert.NotNil(t, entry)
}

// TestHandleServiceAdd_Headless verifies headless services are skipped
func TestHandleServiceAdd_Headless(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headless",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
		},
	}

	service.handleServiceAdd(svc)
	waitForEvents()

	// Verify nothing was stored
	assert.Equal(t, 0, len(mockKV.data), "Headless service should not be stored")
}

// TestHandleServiceUpdate verifies service updates refresh metadata
func TestHandleServiceUpdate(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	oldSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "default",
			Labels:    map[string]string{"version": "v1"},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.1",
		},
	}

	newSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "default",
			Labels:    map[string]string{"version": "v2"},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.1",
		},
	}

	service.handleServiceUpdate(oldSvc, newSvc)
	waitForEvents()

	// Verify updated service metadata was stored
	entry, err := mockKV.Get("service.ip.10.96.0.1")
	require.NoError(t, err)
	assert.NotNil(t, entry)
}

// TestHandleServiceUpdate_ClusterIPChange verifies ClusterIP changes delete old entry
func TestHandleServiceUpdate_ClusterIPChange(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	oldSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.1",
		},
	}

	// Pre-populate KV with old ClusterIP
	service.storeServiceMetadata(oldSvc)

	newSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.2", // ClusterIP changed
		},
	}

	service.handleServiceUpdate(oldSvc, newSvc)
	waitForEvents()

	// Verify old ClusterIP was deleted
	_, err := mockKV.Get("service.ip.10.96.0.1")
	assert.Error(t, err, "Old ClusterIP should be deleted")

	// Verify new ClusterIP was stored
	entry, err := mockKV.Get("service.ip.10.96.0.2")
	require.NoError(t, err, "New ClusterIP should be stored")
	assert.NotNil(t, entry)
}

// TestHandleServiceDelete verifies service deletion removes metadata
func TestHandleServiceDelete(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.1",
		},
	}

	// Pre-populate KV
	service.storeServiceMetadata(svc)
	require.Equal(t, 1, len(mockKV.data))

	service.handleServiceDelete(svc)
	waitForEvents()

	// Verify service was deleted
	_, err := mockKV.Get("service.ip.10.96.0.1")
	assert.Error(t, err, "Service should be deleted from KV")
	assert.Equal(t, 0, len(mockKV.data))
}

// TestHandleServiceDelete_Headless verifies headless service deletes are skipped
func TestHandleServiceDelete_Headless(t *testing.T) {
	mockKV := newMockKV()
	service, cancel := newTestService(t, mockKV)
	defer cancel()

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headless",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
		},
	}

	service.handleServiceDelete(svc)
	waitForEvents()

	// Should not error or panic
	assert.Equal(t, 0, len(mockKV.data))
}
