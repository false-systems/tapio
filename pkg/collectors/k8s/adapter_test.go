package k8s

import (
	"testing"

	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
)

func TestK8sAdapter_GetCollectorID(t *testing.T) {
	adapter := NewK8sAdapter()
	if adapter.GetCollectorID() != "k8s-collector" {
		t.Errorf("Expected collector ID 'k8s-collector', got '%s'", adapter.GetCollectorID())
	}
}

func TestK8sAdapter_GetTracerName(t *testing.T) {
	adapter := NewK8sAdapter()
	if adapter.GetTracerName() != "tapio.k8s.collector" {
		t.Errorf("Expected tracer name 'tapio.k8s.collector', got '%s'", adapter.GetTracerName())
	}
}

func TestK8sAdapter_GetBatchIDPrefix(t *testing.T) {
	adapter := NewK8sAdapter()
	if adapter.GetBatchIDPrefix() != "k8s-batch" {
		t.Errorf("Expected batch ID prefix 'k8s-batch', got '%s'", adapter.GetBatchIDPrefix())
	}
}

func TestK8sAdapter_MapEventType(t *testing.T) {
	adapter := NewK8sAdapter()

	testCases := []struct {
		input    domain.EventType
		expected pb.EventType
	}{
		{domain.EventTypeKubernetes, pb.EventType_EVENT_TYPE_KUBERNETES},
		{domain.EventTypeSystem, pb.EventType_EVENT_TYPE_SYSCALL},
		{domain.EventTypeNetwork, pb.EventType_EVENT_TYPE_NETWORK},
		{domain.EventTypeProcess, pb.EventType_EVENT_TYPE_PROCESS},
		{domain.EventTypeMemory, pb.EventType_EVENT_TYPE_RESOURCE_USAGE},
		{domain.EventTypeCPU, pb.EventType_EVENT_TYPE_RESOURCE_USAGE},
		{domain.EventTypeDisk, pb.EventType_EVENT_TYPE_FILE_SYSTEM},
		{domain.EventTypeService, pb.EventType_EVENT_TYPE_HTTP},
		{"unknown", pb.EventType_EVENT_TYPE_KUBERNETES}, // default
	}

	for _, tc := range testCases {
		result := adapter.MapEventType(tc.input)
		if result != tc.expected {
			t.Errorf("MapEventType(%s): expected %s, got %s", tc.input, tc.expected, result)
		}
	}
}

func TestK8sAdapter_MapSourceType(t *testing.T) {
	adapter := NewK8sAdapter()

	testCases := []struct {
		input    domain.SourceType
		expected pb.SourceType
	}{
		{domain.SourceK8s, pb.SourceType_SOURCE_TYPE_KUBERNETES_API},
		{domain.SourceEBPF, pb.SourceType_SOURCE_TYPE_EBPF},
		{domain.SourceSystemd, pb.SourceType_SOURCE_TYPE_JOURNALD},
		{domain.SourceCNI, pb.SourceType_SOURCE_TYPE_KUBERNETES_API},
		{"unknown", pb.SourceType_SOURCE_TYPE_KUBERNETES_API}, // default
	}

	for _, tc := range testCases {
		result := adapter.MapSourceType(tc.input)
		if result != tc.expected {
			t.Errorf("MapSourceType(%s): expected %s, got %s", tc.input, tc.expected, result)
		}
	}
}

func TestK8sAdapter_ExtractMessage(t *testing.T) {
	adapter := NewK8sAdapter()

	testCases := []struct {
		name     string
		event    *domain.UnifiedEvent
		expected string
	}{
		{
			name: "kubernetes message",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeKubernetes,
				Source: "k8s-collector",
				Kubernetes: &domain.KubernetesData{
					Message: "Pod created successfully",
				},
			},
			expected: "Pod created successfully",
		},
		{
			name: "application message",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeKubernetes,
				Source: "k8s-collector",
				Application: &domain.ApplicationData{
					Message: "Application log message",
				},
			},
			expected: "Application log message",
		},
		{
			name: "semantic narrative",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeKubernetes,
				Source: "k8s-collector",
				Semantic: &domain.SemanticContext{
					Narrative: "Semantic description of event",
				},
			},
			expected: "Semantic description of event",
		},
		{
			name: "fallback message",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeKubernetes,
				Source: "k8s-collector",
			},
			expected: "Event kubernetes from k8s-collector",
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

func TestK8sAdapter_CreateEventContext(t *testing.T) {
	adapter := NewK8sAdapter()

	// Test with nil entity
	event := &domain.UnifiedEvent{
		Type:   domain.EventTypeKubernetes,
		Source: "k8s-collector",
	}
	
	result := adapter.CreateEventContext(event)
	if result != nil {
		t.Errorf("Expected nil context for event without entity, got %+v", result)
	}

	// Test with entity
	event = &domain.UnifiedEvent{
		Type:   domain.EventTypeKubernetes,
		Source: "k8s-collector",
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"cluster": "test-cluster",
				"node":    "worker-1",
				"app":     "test-app",
			},
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

	if len(result.Labels) != 3 {
		t.Errorf("Expected 3 labels, got %d", len(result.Labels))
	}
}

func TestK8sAdapter_ExtractAttributes(t *testing.T) {
	adapter := NewK8sAdapter()

	event := &domain.UnifiedEvent{
		Type:   domain.EventTypeKubernetes,
		Source: "k8s-collector",
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid-123",
			Labels: map[string]string{
				"app": "test-app",
			},
			Attributes: map[string]string{
				"version": "v1.0",
			},
		},
		Kubernetes: &domain.KubernetesData{
			EventType:  "Normal",
			Reason:     "Created",
			Object:     "pod/test-pod",
			ObjectKind: "Pod",
			Action:     "ADDED",
			APIVersion: "v1",
			Labels: map[string]string{
				"controller": "deployment",
			},
			Annotations: map[string]string{
				"description": "test pod",
			},
		},
		Network: &domain.NetworkData{
			Protocol:   "TCP",
			SourceIP:   "10.0.0.1",
			DestIP:     "10.0.0.2",
			Direction:  "egress",
			StatusCode: 200,
		},
		Application: &domain.ApplicationData{
			Level:     "info",
			Logger:    "test-logger",
			ErrorType: "validation",
			UserID:    "user-123",
			SessionID: "session-456",
			RequestID: "request-789",
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

	if attributes["entity.name"] != "test-pod" {
		t.Errorf("Expected entity.name 'test-pod', got '%s'", attributes["entity.name"])
	}

	if attributes["entity.namespace"] != "default" {
		t.Errorf("Expected entity.namespace 'default', got '%s'", attributes["entity.namespace"])
	}

	if attributes["entity.label.app"] != "test-app" {
		t.Errorf("Expected entity.label.app 'test-app', got '%s'", attributes["entity.label.app"])
	}

	if attributes["entity.version"] != "v1.0" {
		t.Errorf("Expected entity.version 'v1.0', got '%s'", attributes["entity.version"])
	}

	// Check Kubernetes attributes
	if attributes["k8s.event_type"] != "Normal" {
		t.Errorf("Expected k8s.event_type 'Normal', got '%s'", attributes["k8s.event_type"])
	}

	if attributes["k8s.reason"] != "Created" {
		t.Errorf("Expected k8s.reason 'Created', got '%s'", attributes["k8s.reason"])
	}

	if attributes["k8s.object"] != "pod/test-pod" {
		t.Errorf("Expected k8s.object 'pod/test-pod', got '%s'", attributes["k8s.object"])
	}

	if attributes["k8s.object_kind"] != "Pod" {
		t.Errorf("Expected k8s.object_kind 'Pod', got '%s'", attributes["k8s.object_kind"])
	}

	if attributes["k8s.action"] != "ADDED" {
		t.Errorf("Expected k8s.action 'ADDED', got '%s'", attributes["k8s.action"])
	}

	if attributes["k8s.api_version"] != "v1" {
		t.Errorf("Expected k8s.api_version 'v1', got '%s'", attributes["k8s.api_version"])
	}

	if attributes["k8s.label.controller"] != "deployment" {
		t.Errorf("Expected k8s.label.controller 'deployment', got '%s'", attributes["k8s.label.controller"])
	}

	if attributes["k8s.annotation.description"] != "test pod" {
		t.Errorf("Expected k8s.annotation.description 'test pod', got '%s'", attributes["k8s.annotation.description"])
	}

	// Check network attributes
	if attributes["network.protocol"] != "TCP" {
		t.Errorf("Expected network.protocol 'TCP', got '%s'", attributes["network.protocol"])
	}

	if attributes["network.source_ip"] != "10.0.0.1" {
		t.Errorf("Expected network.source_ip '10.0.0.1', got '%s'", attributes["network.source_ip"])
	}

	if attributes["network.dest_ip"] != "10.0.0.2" {
		t.Errorf("Expected network.dest_ip '10.0.0.2', got '%s'", attributes["network.dest_ip"])
	}

	if attributes["network.direction"] != "egress" {
		t.Errorf("Expected network.direction 'egress', got '%s'", attributes["network.direction"])
	}

	if attributes["network.status_code"] != "200" {
		t.Errorf("Expected network.status_code '200', got '%s'", attributes["network.status_code"])
	}

	// Check application attributes
	if attributes["app.level"] != "info" {
		t.Errorf("Expected app.level 'info', got '%s'", attributes["app.level"])
	}

	if attributes["app.logger"] != "test-logger" {
		t.Errorf("Expected app.logger 'test-logger', got '%s'", attributes["app.logger"])
	}

	if attributes["app.error_type"] != "validation" {
		t.Errorf("Expected app.error_type 'validation', got '%s'", attributes["app.error_type"])
	}

	if attributes["app.user_id"] != "user-123" {
		t.Errorf("Expected app.user_id 'user-123', got '%s'", attributes["app.user_id"])
	}

	if attributes["app.session_id"] != "session-456" {
		t.Errorf("Expected app.session_id 'session-456', got '%s'", attributes["app.session_id"])
	}

	if attributes["app.request_id"] != "request-789" {
		t.Errorf("Expected app.request_id 'request-789', got '%s'", attributes["app.request_id"])
	}
}

func TestK8sAdapter_ClientCreation(t *testing.T) {
	// Test NewTapioGRPCClient function
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create K8s Tapio client: %v", err)
	}
	defer client.Close()

	stats := client.GetStatistics()
	if stats["collector_id"].(string) != "k8s-collector" {
		t.Errorf("Expected collector ID 'k8s-collector', got '%s'", stats["collector_id"].(string))
	}

	if stats["server_addr"].(string) != "localhost:8080" {
		t.Errorf("Expected server address 'localhost:8080', got '%s'", stats["server_addr"].(string))
	}
}
