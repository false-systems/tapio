package cni

import (
	"testing"

	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
)

func TestCNIAdapter_GetCollectorID(t *testing.T) {
	adapter := NewCNIAdapter()
	if adapter.GetCollectorID() != "cni-collector" {
		t.Errorf("Expected collector ID 'cni-collector', got '%s'", adapter.GetCollectorID())
	}
}

func TestCNIAdapter_GetTracerName(t *testing.T) {
	adapter := NewCNIAdapter()
	if adapter.GetTracerName() != "tapio.cni.collector" {
		t.Errorf("Expected tracer name 'tapio.cni.collector', got '%s'", adapter.GetTracerName())
	}
}

func TestCNIAdapter_GetBatchIDPrefix(t *testing.T) {
	adapter := NewCNIAdapter()
	if adapter.GetBatchIDPrefix() != "cni-batch" {
		t.Errorf("Expected batch ID prefix 'cni-batch', got '%s'", adapter.GetBatchIDPrefix())
	}
}

func TestCNIAdapter_MapEventType(t *testing.T) {
	adapter := NewCNIAdapter()

	testCases := []struct {
		input    domain.EventType
		expected pb.EventType
	}{
		{domain.EventTypeNetwork, pb.EventType_EVENT_TYPE_NETWORK},
		{domain.EventTypeKubernetes, pb.EventType_EVENT_TYPE_KUBERNETES},
		{domain.EventTypeSystem, pb.EventType_EVENT_TYPE_SYSCALL},
		{domain.EventTypeProcess, pb.EventType_EVENT_TYPE_PROCESS},
		{domain.EventTypeService, pb.EventType_EVENT_TYPE_HTTP},
		{domain.EventTypeMemory, pb.EventType_EVENT_TYPE_RESOURCE_USAGE},
		{domain.EventTypeCPU, pb.EventType_EVENT_TYPE_RESOURCE_USAGE},
		{domain.EventTypeDisk, pb.EventType_EVENT_TYPE_FILE_SYSTEM},
		{"unknown", pb.EventType_EVENT_TYPE_NETWORK}, // default
	}

	for _, tc := range testCases {
		result := adapter.MapEventType(tc.input)
		if result != tc.expected {
			t.Errorf("MapEventType(%s): expected %s, got %s", tc.input, tc.expected, result)
		}
	}
}

func TestCNIAdapter_ExtractMessage(t *testing.T) {
	adapter := NewCNIAdapter()

	testCases := []struct {
		name     string
		event    *domain.UnifiedEvent
		expected string
	}{
		{
			name: "kubernetes message",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeNetwork,
				Source: "cni-collector",
				Kubernetes: &domain.KubernetesData{
					Message: "Pod network configured",
				},
			},
			expected: "Pod network configured",
		},
		{
			name: "application message",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeNetwork,
				Source: "cni-collector",
				Application: &domain.ApplicationData{
					Message: "CNI plugin executed",
				},
			},
			expected: "CNI plugin executed",
		},
		{
			name: "semantic narrative",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeNetwork,
				Source: "cni-collector",
				Semantic: &domain.SemanticContext{
					Narrative: "Network interface created",
				},
			},
			expected: "Network interface created",
		},
		{
			name: "network traffic message",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeNetwork,
				Source: "cni-collector",
				Network: &domain.NetworkData{
					Protocol: "TCP",
					SourceIP: "10.0.0.1",
					DestIP:   "10.0.0.2",
				},
			},
			expected: "Network traffic: TCP from 10.0.0.1 to 10.0.0.2",
		},
		{
			name: "network direction message",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeNetwork,
				Source: "cni-collector",
				Network: &domain.NetworkData{
					Direction: "ingress",
				},
			},
			expected: "Network ingress traffic on interface",
		},
		{
			name: "fallback message",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeNetwork,
				Source: "cni-collector",
			},
			expected: "CNI event network from cni-collector",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := adapter.ExtractMessage(tc.event)
			if result != tc.expected {
				t.Errorf("Expected message '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestCNIAdapter_CreateEventContext(t *testing.T) {
	adapter := NewCNIAdapter()

	// Test with nil entity
	event := &domain.UnifiedEvent{
		Type:   domain.EventTypeNetwork,
		Source: "cni-collector",
	}

	result := adapter.CreateEventContext(event)
	if result != nil {
		t.Errorf("Expected nil context for event without entity, got %+v", result)
	}

	// Test with entity and network data
	event = &domain.UnifiedEvent{
		Type:   domain.EventTypeNetwork,
		Source: "cni-collector",
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"cluster":   "test-cluster",
				"node":      "worker-1",
				"interface": "eth0",
			},
		},
		Network: &domain.NetworkData{
			InterfaceName: "veth123",
		},
	}

	result = adapter.CreateEventContext(event)
	if result == nil {
		t.Fatal("Expected non-nil context for event with entity")
	}

	if result.Cluster != "test-cluster" {
		t.Errorf("Expected cluster 'test-cluster', got '%s'", result.Cluster)
	}

	if result.Node != "worker-1" {
		t.Errorf("Expected node 'worker-1', got '%s'", result.Node)
	}

	if result.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", result.Namespace)
	}

	if result.Pod != "test-pod" {
		t.Errorf("Expected pod 'test-pod', got '%s'", result.Pod)
	}

	// Should use network interface name from NetworkData
	if result.Service != "veth123" {
		t.Errorf("Expected service 'veth123', got '%s'", result.Service)
	}
}

func TestCNIAdapter_ExtractAttributes(t *testing.T) {
	adapter := NewCNIAdapter()

	event := &domain.UnifiedEvent{
		Type:   domain.EventTypeNetwork,
		Source: "cni-collector",
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid-123",
		},
		Network: &domain.NetworkData{
			Protocol:       "TCP",
			SourceIP:       "10.0.0.1",
			DestIP:         "10.0.0.2",
			SourcePort:     8080,
			DestPort:       80,
			Direction:      "egress",
			StatusCode:     200,
			BytesSent:      1024,
			BytesRecv:      512,
			Latency:        1000000, // 1ms in nanoseconds
			Method:         "GET",
			Path:           "/api/v1/health",
			InterfaceName:  "veth123",
			VirtualNetwork: "overlay1",
			ContainerID:    "container-abc123",
			Headers: map[string]string{
				"user-agent": "test-client",
			},
		},
		Kubernetes: &domain.KubernetesData{
			EventType:       "Normal",
			Reason:          "NetworkConfigured",
			Object:          "pod/test-pod",
			ObjectKind:      "Pod",
			Action:          "MODIFIED",
			APIVersion:      "v1",
			ResourceVersion: "12345",
			ClusterName:     "test-cluster",
		},
		Application: &domain.ApplicationData{
			Level:     "info",
			Logger:    "cni-plugin",
			ErrorType: "network",
			UserID:    "user-123",
			Custom: map[string]interface{}{
				"plugin_name": "calico",
			},
		},
		Kernel: &domain.KernelData{
			Syscall: "socket",
			PID:     1234,
			Comm:    "cni-process",
		},
	}

	attributes := adapter.ExtractAttributes(event)

	// Check entity attributes
	if attributes["entity.uid"] != "pod-uid-123" {
		t.Errorf("Expected entity.uid 'pod-uid-123', got '%s'", attributes["entity.uid"])
	}

	if attributes["entity.type"] != "pod" {
		t.Errorf("Expected entity.type 'pod', got '%s'", attributes["entity.type"])
	}

	// Check network attributes (CNI-specific focus)
	if attributes["network.protocol"] != "TCP" {
		t.Errorf("Expected network.protocol 'TCP', got '%s'", attributes["network.protocol"])
	}

	if attributes["network.source_ip"] != "10.0.0.1" {
		t.Errorf("Expected network.source_ip '10.0.0.1', got '%s'", attributes["network.source_ip"])
	}

	if attributes["network.dest_ip"] != "10.0.0.2" {
		t.Errorf("Expected network.dest_ip '10.0.0.2', got '%s'", attributes["network.dest_ip"])
	}

	if attributes["network.source_port"] != "8080" {
		t.Errorf("Expected network.source_port '8080', got '%s'", attributes["network.source_port"])
	}

	if attributes["network.dest_port"] != "80" {
		t.Errorf("Expected network.dest_port '80', got '%s'", attributes["network.dest_port"])
	}

	if attributes["network.direction"] != "egress" {
		t.Errorf("Expected network.direction 'egress', got '%s'", attributes["network.direction"])
	}

	if attributes["network.status_code"] != "200" {
		t.Errorf("Expected network.status_code '200', got '%s'", attributes["network.status_code"])
	}

	if attributes["network.bytes_sent"] != "1024" {
		t.Errorf("Expected network.bytes_sent '1024', got '%s'", attributes["network.bytes_sent"])
	}

	if attributes["network.bytes_recv"] != "512" {
		t.Errorf("Expected network.bytes_recv '512', got '%s'", attributes["network.bytes_recv"])
	}

	if attributes["network.latency_ns"] != "1000000" {
		t.Errorf("Expected network.latency_ns '1000000', got '%s'", attributes["network.latency_ns"])
	}

	if attributes["network.method"] != "GET" {
		t.Errorf("Expected network.method 'GET', got '%s'", attributes["network.method"])
	}

	if attributes["network.path"] != "/api/v1/health" {
		t.Errorf("Expected network.path '/api/v1/health', got '%s'", attributes["network.path"])
	}

	if attributes["network.interface"] != "veth123" {
		t.Errorf("Expected network.interface 'veth123', got '%s'", attributes["network.interface"])
	}

	if attributes["network.virtual_network"] != "overlay1" {
		t.Errorf("Expected network.virtual_network 'overlay1', got '%s'", attributes["network.virtual_network"])
	}

	if attributes["network.container_id"] != "container-abc123" {
		t.Errorf("Expected network.container_id 'container-abc123', got '%s'", attributes["network.container_id"])
	}

	if attributes["network.header.user-agent"] != "test-client" {
		t.Errorf("Expected network.header.user-agent 'test-client', got '%s'", attributes["network.header.user-agent"])
	}

	// Check Kubernetes attributes
	if attributes["k8s.event_type"] != "Normal" {
		t.Errorf("Expected k8s.event_type 'Normal', got '%s'", attributes["k8s.event_type"])
	}

	if attributes["k8s.cluster_name"] != "test-cluster" {
		t.Errorf("Expected k8s.cluster_name 'test-cluster', got '%s'", attributes["k8s.cluster_name"])
	}

	// Check application attributes
	if attributes["app.level"] != "info" {
		t.Errorf("Expected app.level 'info', got '%s'", attributes["app.level"])
	}

	if attributes["app.plugin_name"] != "calico" {
		t.Errorf("Expected app.plugin_name 'calico', got '%s'", attributes["app.plugin_name"])
	}

	// Check kernel attributes
	if attributes["kernel.syscall"] != "socket" {
		t.Errorf("Expected kernel.syscall 'socket', got '%s'", attributes["kernel.syscall"])
	}

	if attributes["kernel.pid"] != "1234" {
		t.Errorf("Expected kernel.pid '1234', got '%s'", attributes["kernel.pid"])
	}

	if attributes["kernel.comm"] != "cni-process" {
		t.Errorf("Expected kernel.comm 'cni-process', got '%s'", attributes["kernel.comm"])
	}
}

func TestCNIAdapter_ClientCreation(t *testing.T) {
	// Test NewTapioGRPCClient function
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create CNI Tapio client: %v", err)
	}
	defer client.Close()

	stats := client.GetStatistics()
	if stats["collector_id"].(string) != "cni-collector" {
		t.Errorf("Expected collector ID 'cni-collector', got '%s'", stats["collector_id"].(string))
	}

	if stats["server_addr"].(string) != "localhost:8080" {
		t.Errorf("Expected server address 'localhost:8080', got '%s'", stats["server_addr"].(string))
	}
}
