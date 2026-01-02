package k8scontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTransformPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "uid-123",
			Name:      "nginx-abc123",
			Namespace: "default",
			Labels: map[string]string{
				"app": "nginx",
			},
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: "nginx-7d8f9", Controller: boolPtr(true)},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{
				{
					Name:  "nginx",
					Image: "nginx:1.21",
					Env: []corev1.EnvVar{
						{Name: "OTEL_SERVICE_NAME", Value: "my-nginx"},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			PodIP:  "10.0.1.5",
			HostIP: "192.168.1.10",
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:        "nginx",
					ContainerID: "containerd://abc123def456",
					Image:       "nginx:1.21",
				},
			},
		},
	}

	meta := TransformPod(pod)

	require.NotNil(t, meta)
	assert.Equal(t, "uid-123", meta.UID)
	assert.Equal(t, "nginx-abc123", meta.Name)
	assert.Equal(t, "default", meta.Namespace)
	assert.Equal(t, "node-1", meta.NodeName)
	assert.Equal(t, "10.0.1.5", meta.PodIP)
	assert.Equal(t, "192.168.1.10", meta.HostIP)
	assert.Equal(t, "Deployment", meta.OwnerKind)
	assert.Equal(t, "nginx", meta.OwnerName)
	assert.Equal(t, "my-nginx", meta.OTELServiceName)

	require.Len(t, meta.Containers, 1)
	assert.Equal(t, "nginx", meta.Containers[0].Name)
	assert.Equal(t, "abc123def456", meta.Containers[0].ContainerID) // prefix stripped
}

func TestTransformPod_HostNetwork(t *testing.T) {
	// Host-networked pods should have empty PodIP (to avoid wrong IP lookups)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:  "uid-1",
			Name: "hostnet-pod",
		},
		Status: corev1.PodStatus{
			PodIP:  "192.168.1.10",
			HostIP: "192.168.1.10", // Same as PodIP = host network
		},
	}

	meta := TransformPod(pod)
	assert.Empty(t, meta.PodIP) // Should be empty for host network
}

func TestTransformService(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "svc-123",
			Name:      "nginx-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "10.96.0.10",
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			},
			Selector: map[string]string{"app": "nginx"},
		},
	}

	meta := TransformService(svc)

	require.NotNil(t, meta)
	assert.Equal(t, "svc-123", meta.UID)
	assert.Equal(t, "nginx-svc", meta.Name)
	assert.Equal(t, "10.96.0.10", meta.ClusterIP)
	assert.Equal(t, "ClusterIP", meta.Type)
	require.Len(t, meta.Ports, 1)
	assert.Equal(t, int32(80), meta.Ports[0].Port)
}

func TestStripContainerIDPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"containerd://abc123", "abc123"},
		{"docker://def456", "def456"},
		{"cri-o://ghi789", "ghi789"},
		{"abc123", "abc123"}, // no prefix
		{"", ""},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, StripContainerIDPrefix(tt.input))
	}
}
