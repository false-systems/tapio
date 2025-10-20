package base

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func TestNetworkAttributes(t *testing.T) {
	tests := []struct {
		name     string
		data     *domain.NetworkEventData
		expected []attribute.KeyValue
	}{
		{
			name: "complete TCP connection",
			data: &domain.NetworkEventData{
				Protocol: "tcp",
				SrcIP:    "10.0.1.5",
				DstIP:    "10.0.2.8",
				SrcPort:  44320,
				DstPort:  80,
				Duration: 1500000, // 1.5ms
			},
			expected: []attribute.KeyValue{
				semconv.NetworkProtocolName("tcp"),
				semconv.NetworkPeerAddress("10.0.2.8"),
				semconv.NetworkPeerPort(80),
				semconv.NetworkLocalAddress("10.0.1.5"),
				semconv.NetworkLocalPort(44320),
				attribute.Int64("network.connection.duration_ns", 1500000),
			},
		},
		{
			name: "HTTP request with metadata",
			data: &domain.NetworkEventData{
				Protocol:       "http",
				HTTPMethod:     "POST",
				HTTPPath:       "/api/users",
				HTTPStatusCode: 201,
				BytesSent:      512,
				BytesReceived:  2048,
			},
			expected: []attribute.KeyValue{
				semconv.NetworkProtocolName("http"),
				attribute.String("http.request.method", "POST"),
				semconv.URLPath("/api/users"),
				attribute.Int("http.response.status_code", 201),
				attribute.Int64("network.io.bytes_sent", 512),
				attribute.Int64("network.io.bytes_received", 2048),
			},
		},
		{
			name:     "nil data",
			data:     nil,
			expected: nil,
		},
		{
			name:     "empty data",
			data:     &domain.NetworkEventData{},
			expected: []attribute.KeyValue{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := NetworkAttributes(tt.data)
			assert.Equal(t, tt.expected, attrs)
		})
	}
}

func TestProcessAttributes(t *testing.T) {
	tests := []struct {
		name     string
		data     *domain.ProcessEventData
		expected []attribute.KeyValue
	}{
		{
			name: "complete process info",
			data: &domain.ProcessEventData{
				PID:         1234,
				PPID:        1,
				ProcessName: "nginx",
				CommandLine: "/usr/sbin/nginx -g daemon off;",
			},
			expected: []attribute.KeyValue{
				semconv.ProcessPID(1234),
				semconv.ProcessParentPID(1),
				semconv.ProcessExecutableName("nginx"),
				semconv.ProcessCommand("/usr/sbin/nginx -g daemon off;"),
			},
		},
		{
			name: "minimal process info",
			data: &domain.ProcessEventData{
				PID: 5678,
			},
			expected: []attribute.KeyValue{
				semconv.ProcessPID(5678),
			},
		},
		{
			name:     "nil data",
			data:     nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := ProcessAttributes(tt.data)
			assert.Equal(t, tt.expected, attrs)
		})
	}
}

func TestK8sAttributes(t *testing.T) {
	tests := []struct {
		name           string
		clusterID      string
		namespace      string
		podName        string
		deploymentName string
		expected       []attribute.KeyValue
	}{
		{
			name:           "complete K8s context",
			clusterID:      "prod-us-east",
			namespace:      "payments",
			podName:        "nginx-7d8f-abc123",
			deploymentName: "nginx",
			expected: []attribute.KeyValue{
				semconv.K8SClusterName("prod-us-east"),
				semconv.K8SNamespaceName("payments"),
				semconv.K8SPodName("nginx-7d8f-abc123"),
				semconv.K8SDeploymentName("nginx"),
			},
		},
		{
			name:      "partial K8s context",
			clusterID: "staging",
			namespace: "default",
			expected: []attribute.KeyValue{
				semconv.K8SClusterName("staging"),
				semconv.K8SNamespaceName("default"),
			},
		},
		{
			name:     "empty K8s context",
			expected: []attribute.KeyValue{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := K8sAttributes(tt.clusterID, tt.namespace, tt.podName, tt.deploymentName)
			assert.Equal(t, tt.expected, attrs)
		})
	}
}

func TestEventDomainAttribute(t *testing.T) {
	tests := []struct {
		eventType      string
		expectedDomain string
	}{
		{"tcp_connect", "network"},
		{"udp_packet", "network"},
		{"http_request", "network"},
		{"dns_query", "network"},
		{"connection_timeout", "network"},
		{"oom_kill", "kernel"},
		{"syscall_open", "kernel"},
		{"signal_term", "kernel"},
		{"container_start", "container"},
		{"docker_stop", "container"},
		{"pod_created", "kubernetes"},
		{"deployment_scaled", "kubernetes"},
		{"service_updated", "kubernetes"},
		{"node_ready", "kubernetes"},
		{"process_exit", "process"},
		{"exec_failed", "process"},
		{"unknown_event", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			attr := EventDomainAttribute(tt.eventType)
			assert.Equal(t, "event.domain", string(attr.Key))
			assert.Equal(t, tt.expectedDomain, attr.Value.AsString())
		})
	}
}

func TestErrorTypeAttribute(t *testing.T) {
	tests := []struct {
		eventType     string
		expectedError string
	}{
		{"oom_kill", "out_of_memory"},
		{"connection_timeout", "network_error"},
		{"connection_refused", "network_error"},
		{"pod_crash_loop", "crash"},
		{"container_crash", "crash"},
		{"deployment_failed", "deployment_error"},
		{"http_5xx", "http_server_error"},
		{"http_4xx", "http_client_error"},
		{"unknown_error", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			attr := ErrorTypeAttribute(tt.eventType)
			assert.Equal(t, "error.type", string(attr.Key))
			assert.Equal(t, tt.expectedError, attr.Value.AsString())
		})
	}
}

func TestIsErrorEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    *domain.ObserverEvent
		expected bool
	}{
		{
			name: "OOM kill event",
			event: &domain.ObserverEvent{
				Type: "oom_kill",
			},
			expected: true,
		},
		{
			name: "connection timeout",
			event: &domain.ObserverEvent{
				Type: "connection_timeout",
			},
			expected: true,
		},
		{
			name: "HTTP 500 error",
			event: &domain.ObserverEvent{
				Type: "http_request",
				NetworkData: &domain.NetworkEventData{
					HTTPStatusCode: 500,
				},
			},
			expected: true,
		},
		{
			name: "HTTP 503 error",
			event: &domain.ObserverEvent{
				Type: "http_request",
				NetworkData: &domain.NetworkEventData{
					HTTPStatusCode: 503,
				},
			},
			expected: true,
		},
		{
			name: "HTTP 200 success",
			event: &domain.ObserverEvent{
				Type: "http_request",
				NetworkData: &domain.NetworkEventData{
					HTTPStatusCode: 200,
				},
			},
			expected: false,
		},
		{
			name: "normal TCP connection",
			event: &domain.ObserverEvent{
				Type: "tcp_connect",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsErrorEvent(tt.event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetEventDomain(t *testing.T) {
	// Test internal function
	tests := []struct {
		eventType string
		want      string
	}{
		{"tcp_connect", "network"},
		{"udp_send", "network"},
		{"http_get", "network"},
		{"dns_resolve", "network"},
		{"oom_kill", "kernel"},
		{"syscall_read", "kernel"},
		{"container_start", "container"},
		{"pod_delete", "kubernetes"},
		{"process_spawn", "process"},
		{"random_event", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			got := getEventDomain(tt.eventType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCategorizeError(t *testing.T) {
	// Test internal function
	tests := []struct {
		eventType string
		want      string
	}{
		{"oom_kill", "out_of_memory"},
		{"connection_timeout", "network_error"},
		{"pod_crash_loop", "crash"},
		{"http_5xx", "http_server_error"},
		{"other_error", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			got := categorizeError(tt.eventType)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Benchmark tests
func BenchmarkNetworkAttributes(b *testing.B) {
	data := &domain.NetworkEventData{
		Protocol: "tcp",
		SrcIP:    "10.0.1.5",
		DstIP:    "10.0.2.8",
		SrcPort:  44320,
		DstPort:  80,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NetworkAttributes(data) // Ignore: benchmark return value
	}
}

func BenchmarkEventDomainAttribute(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EventDomainAttribute("tcp_connect") // Ignore: benchmark return value
	}
}
