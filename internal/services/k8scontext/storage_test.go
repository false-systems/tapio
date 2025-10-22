package k8scontext

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestToPodInfo_ValidPod verifies transformation of K8s Pod to PodInfo
func TestToPodInfo_ValidPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-abc123",
			Namespace: "default",
			Labels: map[string]string{
				"app": "nginx",
				"env": "prod",
			},
		},
		Status: corev1.PodStatus{
			PodIP:  "10.244.1.5",
			HostIP: "192.168.1.10",
		},
	}

	podInfo := toPodInfo(pod)

	assert.Equal(t, "nginx-abc123", podInfo.Name)
	assert.Equal(t, "default", podInfo.Namespace)
	assert.Equal(t, "10.244.1.5", podInfo.PodIP)
	assert.Equal(t, "192.168.1.10", podInfo.HostIP)
	assert.Equal(t, 2, len(podInfo.Labels))
	assert.Equal(t, "nginx", podInfo.Labels["app"])
	assert.Equal(t, "prod", podInfo.Labels["env"])
}

// TestToPodInfo_NilLabels verifies nil labels are handled
func TestToPodInfo_NilLabels(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels:    nil, // nil labels
		},
		Status: corev1.PodStatus{
			PodIP:  "10.0.1.5",
			HostIP: "192.168.1.1",
		},
	}

	podInfo := toPodInfo(pod)

	assert.NotNil(t, podInfo.Labels, "Labels should be initialized to empty map, not nil")
	assert.Equal(t, 0, len(podInfo.Labels))
}

// TestToPodInfo_EmptyPodIP verifies pod without IP
func TestToPodInfo_EmptyPodIP(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			PodIP:  "", // No IP assigned yet
			HostIP: "192.168.1.1",
		},
	}

	podInfo := toPodInfo(pod)

	assert.Equal(t, "", podInfo.PodIP, "Should preserve empty PodIP")
}

// TestToServiceInfo_ValidService verifies transformation of K8s Service to ServiceInfo
func TestToServiceInfo_ValidService(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubernetes",
			Namespace: "default",
			Labels: map[string]string{
				"component": "apiserver",
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.1",
			Type:      corev1.ServiceTypeClusterIP,
		},
	}

	serviceInfo := toServiceInfo(service)

	assert.Equal(t, "kubernetes", serviceInfo.Name)
	assert.Equal(t, "default", serviceInfo.Namespace)
	assert.Equal(t, "10.96.0.1", serviceInfo.ClusterIP)
	assert.Equal(t, "ClusterIP", serviceInfo.Type)
	assert.Equal(t, 1, len(serviceInfo.Labels))
	assert.Equal(t, "apiserver", serviceInfo.Labels["component"])
}

// TestToServiceInfo_LoadBalancer verifies LoadBalancer service type
func TestToServiceInfo_LoadBalancer(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-gateway",
			Namespace: "production",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.5.10",
			Type:      corev1.ServiceTypeLoadBalancer,
		},
	}

	serviceInfo := toServiceInfo(service)

	assert.Equal(t, "LoadBalancer", serviceInfo.Type)
}

// TestToServiceInfo_HeadlessService verifies headless service (ClusterIP=None)
func TestToServiceInfo_HeadlessService(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headless-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None", // Headless service
			Type:      corev1.ServiceTypeClusterIP,
		},
	}

	serviceInfo := toServiceInfo(service)

	assert.Equal(t, "None", serviceInfo.ClusterIP, "Should preserve 'None' for headless services")
}

// TestMakePodKey verifies NATS KV key generation for pods
func TestMakePodKey(t *testing.T) {
	key := makePodKey("10.244.1.5")
	assert.Equal(t, "pod.ip.10.244.1.5", key)
}

// TestMakeServiceKey verifies NATS KV key generation for services
func TestMakeServiceKey(t *testing.T) {
	key := makeServiceKey("10.96.0.1")
	assert.Equal(t, "service.ip.10.96.0.1", key)
}

// TestShouldSkipPod verifies pod skip logic
func TestShouldSkipPod(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected bool
		reason   string
	}{
		{
			name: "Valid pod with IP",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{PodIP: "10.244.1.5"},
			},
			expected: false,
			reason:   "Should NOT skip pod with IP",
		},
		{
			name: "Pod without IP",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{PodIP: ""},
			},
			expected: true,
			reason:   "Should skip pod without IP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldSkipPod(tt.pod)
			assert.Equal(t, tt.expected, result, tt.reason)
		})
	}
}

// TestShouldSkipService verifies service skip logic
func TestShouldSkipService(t *testing.T) {
	tests := []struct {
		name     string
		service  *corev1.Service
		expected bool
		reason   string
	}{
		{
			name: "Valid service with ClusterIP",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.1"},
			},
			expected: false,
			reason:   "Should NOT skip service with ClusterIP",
		},
		{
			name: "Headless service (None)",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{ClusterIP: "None"},
			},
			expected: true,
			reason:   "Should skip headless service",
		},
		{
			name: "Service without ClusterIP",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{ClusterIP: ""},
			},
			expected: true,
			reason:   "Should skip service without ClusterIP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldSkipService(tt.service)
			assert.Equal(t, tt.expected, result, tt.reason)
		})
	}
}

// TestSerializePodInfo verifies PodInfo JSON serialization
func TestSerializePodInfo(t *testing.T) {
	podInfo := PodInfo{
		Name:      "test-pod",
		Namespace: "default",
		PodIP:     "10.0.1.5",
		HostIP:    "192.168.1.1",
		Labels:    map[string]string{"app": "test"},
	}

	data, err := serializePodInfo(podInfo)
	require.NoError(t, err, "Should serialize PodInfo without error")

	// Verify can unmarshal back
	var decoded PodInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "Should unmarshal serialized PodInfo")
	assert.Equal(t, podInfo, decoded)
}

// TestSerializeServiceInfo verifies ServiceInfo JSON serialization
func TestSerializeServiceInfo(t *testing.T) {
	serviceInfo := ServiceInfo{
		Name:      "test-svc",
		Namespace: "default",
		ClusterIP: "10.96.0.1",
		Type:      "ClusterIP",
		Labels:    map[string]string{"app": "test"},
	}

	data, err := serializeServiceInfo(serviceInfo)
	require.NoError(t, err, "Should serialize ServiceInfo without error")

	// Verify can unmarshal back
	var decoded ServiceInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "Should unmarshal serialized ServiceInfo")
	assert.Equal(t, serviceInfo, decoded)
}

// TestMultiIndexKeyGeneration verifies all multi-index key helpers
func TestMultiIndexKeyGeneration(t *testing.T) {
	tests := []struct {
		name     string
		function func() string
		expected string
	}{
		{
			name:     "pod by IP key",
			function: func() string { return makePodByIPKey("10.0.1.42") },
			expected: "pod.ip.10.0.1.42",
		},
		{
			name:     "pod by UID key",
			function: func() string { return makePodByUIDKey("abc-123-def-456") },
			expected: "pod.uid.abc-123-def-456",
		},
		{
			name:     "pod by name key",
			function: func() string { return makePodByNameKey("default", "my-pod") },
			expected: "pod.name.default.my-pod",
		},
		{
			name:     "pod by name key with complex namespace",
			function: func() string { return makePodByNameKey("kube-system", "coredns-abc123") },
			expected: "pod.name.kube-system.coredns-abc123",
		},
		{
			name:     "pod name cache key",
			function: func() string { return makePodNameKey("default", "my-pod") },
			expected: "default/my-pod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.function()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestToPodInfo_WithOTELAttributes verifies pre-computed OTEL attributes
func TestToPodInfo_WithOTELAttributes(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "abc-123",
			Labels: map[string]string{
				"app": "nginx",
			},
			Annotations: map[string]string{
				"resource.opentelemetry.io/service.name": "my-service",
			},
		},
		Status: corev1.PodStatus{
			PodIP:  "10.0.1.42",
			HostIP: "192.168.1.100",
		},
	}

	podInfo := toPodInfo(pod)

	// Verify OTEL attributes were pre-computed (Beyla pattern)
	require.NotNil(t, podInfo.OTELAttributes)
	assert.Equal(t, "my-service", podInfo.OTELAttributes["service.name"])
}

// TestToPodInfo_EnvVarPriority verifies Beyla priority cascade
func TestToPodInfo_EnvVarPriority(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "label-service",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Env: []corev1.EnvVar{
						{Name: "OTEL_SERVICE_NAME", Value: "env-service"},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			PodIP: "10.0.1.42",
		},
	}

	podInfo := toPodInfo(pod)

	// Env var should override label (Beyla priority: env vars > annotations > labels)
	assert.Equal(t, "env-service", podInfo.OTELAttributes["service.name"])
}
