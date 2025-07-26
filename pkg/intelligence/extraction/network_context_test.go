package extraction

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNetworkContextExtraction(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	// Create test pods with IPs
	frontendPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "frontend-pod",
			Namespace: "production",
			Labels:    map[string]string{"app": "frontend"},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.244.1.10",
		},
	}

	apiPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-pod",
			Namespace: "production",
			Labels:    map[string]string{"app": "api"},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-2",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.244.2.20",
		},
	}

	// Create test service
	apiService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-service",
			Namespace: "production",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.1.100",
			Selector:  map[string]string{"app": "api"},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	// Create test endpoints
	apiEndpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-service",
			Namespace: "production",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{IP: "10.244.2.20"}, // api-pod IP
				},
				Ports: []corev1.EndpointPort{
					{Port: 8080},
				},
			},
		},
	}

	// Create fake client
	k8sClient := fake.NewSimpleClientset(frontendPod, apiPod, apiService, apiEndpoints)

	// Create extractor
	extractor, err := NewK8sContextExtractor(k8sClient, logger)
	require.NoError(t, err)

	// Wait for cache to sync
	time.Sleep(100 * time.Millisecond)

	tests := []struct {
		name      string
		event     *domain.UnifiedEvent
		checkFunc func(t *testing.T, event *domain.UnifiedEvent)
	}{
		{
			name: "Pod-to-Service network call",
			event: &domain.UnifiedEvent{
				ID:        "net-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Network: &domain.NetworkData{
					Protocol:   "TCP",
					SourceIP:   "10.244.1.10", // frontend pod
					SourcePort: 45678,
					DestIP:     "10.96.1.100", // api service
					DestPort:   8080,
					Direction:  "egress",
				},
			},
			checkFunc: func(t *testing.T, event *domain.UnifiedEvent) {
				require.NotNil(t, event.K8sContext)
				assert.Equal(t, "frontend-pod", event.K8sContext.Name)
				assert.Equal(t, "production", event.K8sContext.Namespace)

				// Check correlation hints
				assert.Contains(t, event.CorrelationHints, "source_pod:production/frontend-pod")
				assert.Contains(t, event.CorrelationHints, "service:production/api-service:8080")
				assert.Contains(t, event.CorrelationHints, "backend_pod:production/api-pod")
			},
		},
		{
			name: "External to internal traffic",
			event: &domain.UnifiedEvent{
				ID:        "net-2",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Network: &domain.NetworkData{
					Protocol:   "TCP",
					SourceIP:   "1.2.3.4", // External IP
					SourcePort: 12345,
					DestIP:     "10.244.2.20", // api pod
					DestPort:   8080,
					Direction:  "ingress",
				},
			},
			checkFunc: func(t *testing.T, event *domain.UnifiedEvent) {
				require.NotNil(t, event.K8sContext)
				assert.Equal(t, "api-pod", event.K8sContext.Name)
				assert.Contains(t, event.CorrelationHints, "dest_pod:production/api-pod")
				// Source should not be correlated as it's external
				assert.NotContains(t, event.CorrelationHints, "source_pod:")
			},
		},
		{
			name: "High latency detection",
			event: &domain.UnifiedEvent{
				ID:        "net-3",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Network: &domain.NetworkData{
					Protocol: "TCP",
					SourceIP: "10.244.1.10",
					DestIP:   "10.244.2.20",
					DestPort: 3306,      // MySQL port
					Latency:  200000000, // 200ms
				},
			},
			checkFunc: func(t *testing.T, event *domain.UnifiedEvent) {
				assert.Contains(t, event.CorrelationHints, "high_latency")
				assert.Contains(t, event.CorrelationHints, "mysql")
			},
		},
		{
			name: "HTTP error correlation",
			event: &domain.UnifiedEvent{
				ID:        "net-4",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Network: &domain.NetworkData{
					Protocol:   "HTTP",
					SourceIP:   "10.244.1.10",
					DestIP:     "10.96.1.100",
					DestPort:   8080,
					Method:     "POST",
					Path:       "/api/v1/orders",
					StatusCode: 503,
				},
			},
			checkFunc: func(t *testing.T, event *domain.UnifiedEvent) {
				assert.Contains(t, event.CorrelationHints, "server_error")
				assert.Contains(t, event.CorrelationHints, "http")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := extractor.Process(ctx, tt.event)
			assert.NoError(t, err)
			tt.checkFunc(t, tt.event)
		})
	}
}

func TestIsExternalIP(t *testing.T) {
	tests := []struct {
		ip       string
		external bool
	}{
		// Internal IPs
		{"10.0.0.1", false},
		{"10.244.1.10", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"127.0.0.1", false},
		{"::1", false},
		{"fd00::1", false},

		// External IPs
		{"8.8.8.8", true},
		{"1.2.3.4", true},
		{"54.123.45.67", true},
		{"2001:4860:4860::8888", true},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			result := isExternalIP(tt.ip)
			assert.Equal(t, tt.external, result, "IP %s external check", tt.ip)
		})
	}
}

func TestWellKnownPorts(t *testing.T) {
	tests := []struct {
		port     uint16
		expected []string
	}{
		{80, []string{"http"}},
		{443, []string{"https"}},
		{3306, []string{"mysql"}},
		{5432, []string{"postgres"}},
		{6379, []string{"redis"}},
		{9092, []string{"kafka"}},
		{6443, []string{"kube-apiserver"}},
		{10250, []string{"kubelet"}},
		{9999, []string{}}, // Unknown port
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("port_%d", tt.port), func(t *testing.T) {
			hints := getWellKnownPortHints(tt.port)
			assert.Equal(t, tt.expected, hints)
		})
	}
}
