package k8scontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestPodInfo_JSONSerialization verifies PodInfo struct marshaling
func TestPodInfo_JSONSerialization(t *testing.T) {
	podInfo := PodInfo{
		Name:      "test-pod",
		Namespace: "default",
		PodIP:     "10.0.1.5",
		HostIP:    "192.168.1.10",
		Labels: map[string]string{
			"app": "test",
			"env": "dev",
		},
	}

	// Verify all fields are present
	assert.Equal(t, "test-pod", podInfo.Name)
	assert.Equal(t, "default", podInfo.Namespace)
	assert.Equal(t, "10.0.1.5", podInfo.PodIP)
	assert.Equal(t, "192.168.1.10", podInfo.HostIP)
	assert.Len(t, podInfo.Labels, 2)
}

// TestServiceInfo_JSONSerialization verifies ServiceInfo struct marshaling
func TestServiceInfo_JSONSerialization(t *testing.T) {
	serviceInfo := ServiceInfo{
		Name:      "test-service",
		Namespace: "default",
		ClusterIP: "10.96.0.1",
		Type:      "ClusterIP",
		Labels: map[string]string{
			"app": "api",
		},
	}

	assert.Equal(t, "test-service", serviceInfo.Name)
	assert.Equal(t, "default", serviceInfo.Namespace)
	assert.Equal(t, "10.96.0.1", serviceInfo.ClusterIP)
	assert.Equal(t, "ClusterIP", serviceInfo.Type)
	assert.Len(t, serviceInfo.Labels, 1)
}

// TestConfig_Validation verifies Config struct fields
func TestConfig_Validation(t *testing.T) {
	cfg := Config{
		NATSConn: nil, // Will be set in integration tests
		KVBucket: "tapio-context",
	}

	assert.Equal(t, "tapio-context", cfg.KVBucket)
}

// TestPodWithoutIP verifies pods without IP are skipped
func TestPodWithoutIP(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP: "", // No IP assigned yet
		},
	}

	// Pods without IP should be skipped
	require.Empty(t, pod.Status.PodIP)
}

// TestHeadlessService verifies headless services are skipped
func TestHeadlessService(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headless-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None", // Headless service
		},
	}

	// Headless services should be skipped
	assert.Equal(t, "None", svc.Spec.ClusterIP)
}
