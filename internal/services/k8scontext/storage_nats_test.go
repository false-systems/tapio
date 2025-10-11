package k8scontext

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestStorePodMetadata_Success verifies pod metadata is stored in NATS KV
func TestStorePodMetadata_Success(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-abc123",
			Namespace: "default",
			Labels:    map[string]string{"app": "nginx"},
		},
		Status: corev1.PodStatus{
			PodIP:  "10.244.1.5",
			HostIP: "192.168.1.10",
		},
	}

	err := service.storePodMetadata(pod)
	require.NoError(t, err, "Should store pod metadata without error")

	// Verify data was written to KV
	entry, err := mockKV.Get("pod.ip.10.244.1.5")
	require.NoError(t, err, "Should retrieve stored pod metadata")

	var storedPod PodInfo
	err = json.Unmarshal(entry.Value(), &storedPod)
	require.NoError(t, err, "Should unmarshal stored pod info")

	assert.Equal(t, "nginx-abc123", storedPod.Name)
	assert.Equal(t, "default", storedPod.Namespace)
	assert.Equal(t, "10.244.1.5", storedPod.PodIP)
	assert.Equal(t, "192.168.1.10", storedPod.HostIP)
	assert.Equal(t, "nginx", storedPod.Labels["app"])
}

// TestStorePodMetadata_SkipWithoutIP verifies pods without IP are skipped
func TestStorePodMetadata_SkipWithoutIP(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "", // No IP assigned
		},
	}

	err := service.storePodMetadata(pod)
	require.NoError(t, err, "Should return no error for skipped pod")

	// Verify nothing was written
	assert.Equal(t, 0, len(mockKV.data), "Should not write pod without IP")
}

// TestDeletePodMetadata_Success verifies pod metadata is deleted from NATS KV
func TestDeletePodMetadata_Success(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	// Pre-populate KV with pod data
	podInfo := PodInfo{
		Name:      "test-pod",
		Namespace: "default",
		PodIP:     "10.0.1.5",
		HostIP:    "192.168.1.1",
		Labels:    map[string]string{"app": "test"},
	}
	data, _ := json.Marshal(podInfo)
	mockKV.Put("pod.ip.10.0.1.5", data)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "10.0.1.5",
		},
	}

	err := service.deletePodMetadata(pod)
	require.NoError(t, err, "Should delete pod metadata without error")

	// Verify data was deleted
	_, err = mockKV.Get("pod.ip.10.0.1.5")
	assert.Error(t, err, "Should not find deleted pod metadata")
}

// TestDeletePodMetadata_SkipWithoutIP verifies pods without IP are skipped
func TestDeletePodMetadata_SkipWithoutIP(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-without-ip",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "", // No IP
		},
	}

	err := service.deletePodMetadata(pod)
	require.NoError(t, err, "Should return no error for skipped pod")
}

// TestStoreServiceMetadata_Success verifies service metadata is stored in NATS KV
func TestStoreServiceMetadata_Success(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

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

	err := service.storeServiceMetadata(svc)
	require.NoError(t, err, "Should store service metadata without error")

	// Verify data was written to KV
	entry, err := mockKV.Get("service.ip.10.96.0.1")
	require.NoError(t, err, "Should retrieve stored service metadata")

	var storedSvc ServiceInfo
	err = json.Unmarshal(entry.Value(), &storedSvc)
	require.NoError(t, err, "Should unmarshal stored service info")

	assert.Equal(t, "kubernetes", storedSvc.Name)
	assert.Equal(t, "default", storedSvc.Namespace)
	assert.Equal(t, "10.96.0.1", storedSvc.ClusterIP)
	assert.Equal(t, "ClusterIP", storedSvc.Type)
}

// TestStoreServiceMetadata_SkipHeadless verifies headless services are skipped
func TestStoreServiceMetadata_SkipHeadless(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headless-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None", // Headless service
		},
	}

	err := service.storeServiceMetadata(svc)
	require.NoError(t, err, "Should return no error for skipped service")

	// Verify nothing was written
	assert.Equal(t, 0, len(mockKV.data), "Should not write headless service")
}

// TestDeleteServiceMetadata_Success verifies service metadata is deleted from NATS KV
func TestDeleteServiceMetadata_Success(t *testing.T) {
	mockKV := newMockKV()
	service := &Service{kv: mockKV}

	// Pre-populate KV with service data
	serviceInfo := ServiceInfo{
		Name:      "test-svc",
		Namespace: "default",
		ClusterIP: "10.96.5.10",
		Type:      "ClusterIP",
		Labels:    map[string]string{"app": "test"},
	}
	data, _ := json.Marshal(serviceInfo)
	mockKV.Put("service.ip.10.96.5.10", data)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.5.10",
		},
	}

	err := service.deleteServiceMetadata(svc)
	require.NoError(t, err, "Should delete service metadata without error")

	// Verify data was deleted
	_, err = mockKV.Get("service.ip.10.96.5.10")
	assert.Error(t, err, "Should not find deleted service metadata")
}
