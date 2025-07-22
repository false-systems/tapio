package systemd

import (
	"testing"

	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
)

func TestSystemdAdapter_GetCollectorID(t *testing.T) {
	adapter := NewSystemdAdapter()
	if adapter.GetCollectorID() != "systemd-collector" {
		t.Errorf("Expected collector ID 'systemd-collector', got '%s'", adapter.GetCollectorID())
	}
}

func TestSystemdAdapter_GetTracerName(t *testing.T) {
	adapter := NewSystemdAdapter()
	if adapter.GetTracerName() != "tapio.systemd.collector" {
		t.Errorf("Expected tracer name 'tapio.systemd.collector', got '%s'", adapter.GetTracerName())
	}
}

func TestSystemdAdapter_GetBatchIDPrefix(t *testing.T) {
	adapter := NewSystemdAdapter()
	if adapter.GetBatchIDPrefix() != "systemd-batch" {
		t.Errorf("Expected batch ID prefix 'systemd-batch', got '%s'", adapter.GetBatchIDPrefix())
	}
}

func TestSystemdAdapter_MapEventType(t *testing.T) {
	adapter := NewSystemdAdapter()

	testCases := []struct {
		input    domain.EventType
		expected pb.EventType
	}{
		{domain.EventTypeSystem, pb.EventType_EVENT_TYPE_SYSCALL},
		{domain.EventTypeProcess, pb.EventType_EVENT_TYPE_PROCESS},
		{domain.EventTypeService, pb.EventType_EVENT_TYPE_HTTP},
		{domain.EventTypeKubernetes, pb.EventType_EVENT_TYPE_KUBERNETES},
		{domain.EventTypeNetwork, pb.EventType_EVENT_TYPE_NETWORK},
		{domain.EventTypeMemory, pb.EventType_EVENT_TYPE_RESOURCE_USAGE},
		{domain.EventTypeCPU, pb.EventType_EVENT_TYPE_RESOURCE_USAGE},
		{domain.EventTypeDisk, pb.EventType_EVENT_TYPE_FILE_SYSTEM},
		{"unknown", pb.EventType_EVENT_TYPE_SYSCALL}, // default
	}

	for _, tc := range testCases {
		result := adapter.MapEventType(tc.input)
		if result != tc.expected {
			t.Errorf("MapEventType(%s): expected %s, got %s", tc.input, tc.expected, result)
		}
	}
}

func TestSystemdAdapter_ExtractMessage(t *testing.T) {
	adapter := NewSystemdAdapter()

	testCases := []struct {
		name     string
		event    *domain.UnifiedEvent
		expected string
	}{
		{
			name: "application message",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeSystem,
				Source: "systemd-collector",
				Application: &domain.ApplicationData{
					Message: "Service started successfully",
				},
			},
			expected: "Service started successfully",
		},
		{
			name: "semantic narrative",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeSystem,
				Source: "systemd-collector",
				Semantic: &domain.SemanticContext{
					Narrative: "System service state change",
				},
			},
			expected: "System service state change",
		},
		{
			name: "kubernetes message",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeSystem,
				Source: "systemd-collector",
				Kubernetes: &domain.KubernetesData{
					Message: "Container service event",
				},
			},
			expected: "Container service event",
		},
		{
			name: "kernel syscall message",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeSystem,
				Source: "systemd-collector",
				Kernel: &domain.KernelData{
					Syscall: "execve",
					PID:     1234,
				},
			},
			expected: "System call: execve (PID: 1234)",
		},
		{
			name: "kernel process message",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeSystem,
				Source: "systemd-collector",
				Kernel: &domain.KernelData{
					Comm: "nginx",
					PID:  5678,
				},
			},
			expected: "Process: nginx (PID: 5678)",
		},
		{
			name: "fallback message",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeSystem,
				Source: "systemd-collector",
			},
			expected: "Systemd event system from systemd-collector",
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

func TestSystemdAdapter_CreateEventContext(t *testing.T) {
	adapter := NewSystemdAdapter()

	// Test with nil entity
	event := &domain.UnifiedEvent{
		Type:   domain.EventTypeSystem,
		Source: "systemd-collector",
	}
	
	result := adapter.CreateEventContext(event)
	if result != nil {
		t.Errorf("Expected nil context for event without entity, got %+v", result)
	}

	// Test with entity
	event = &domain.UnifiedEvent{
		Type:   domain.EventTypeSystem,
		Source: "systemd-collector",
		Entity: &domain.EntityContext{
			Type:      "service",
			Name:      "nginx.service",
			Namespace: "system",
			Labels: map[string]string{
				"hostname": "server1",
				"unit":     "nginx.service",
			},
		},
	}

	result = adapter.CreateEventContext(event)
	if result == nil {
		t.Fatal("Expected non-nil context for event with entity")
	}

	if result.Node != "server1" {
		t.Errorf("Expected node 'server1', got '%s'", result.Node)
	}

	if result.Service != "nginx.service" {
		t.Errorf("Expected service 'nginx.service', got '%s'", result.Service)
	}

	if result.Namespace != "system" {
		t.Errorf("Expected namespace 'system', got '%s'", result.Namespace)
	}
}

func TestSystemdAdapter_ExtractAttributes(t *testing.T) {
	adapter := NewSystemdAdapter()

	event := &domain.UnifiedEvent{
		Type:   domain.EventTypeSystem,
		Source: "systemd-collector",
		Entity: &domain.EntityContext{
			Type:      "service",
			Name:      "nginx.service",
			Namespace: "system",
			UID:       "service-uid-123",
		},
		Kernel: &domain.KernelData{
			Syscall:    "execve",
			PID:        1234,
			TID:        1234,
			UID:        0,
			GID:        0,
			Comm:       "nginx",
			ReturnCode: 0,
			CPUCore:    2,
			Args: map[string]string{
				"filename": "/usr/sbin/nginx",
			},
		},
		Application: &domain.ApplicationData{
			Level:     "info",
			Logger:    "systemd",
			ErrorType: "startup",
			Custom: map[string]interface{}{
				"unit_type": "service",
			},
		},
	}

	attributes := adapter.ExtractAttributes(event)

	// Check entity attributes
	if attributes["entity.uid"] != "service-uid-123" {
		t.Errorf("Expected entity.uid 'service-uid-123', got '%s'", attributes["entity.uid"])
	}

	if attributes["entity.type"] != "service" {
		t.Errorf("Expected entity.type 'service', got '%s'", attributes["entity.type"])
	}

	if attributes["entity.name"] != "nginx.service" {
		t.Errorf("Expected entity.name 'nginx.service', got '%s'", attributes["entity.name"])
	}

	// Check kernel attributes
	if attributes["kernel.syscall"] != "execve" {
		t.Errorf("Expected kernel.syscall 'execve', got '%s'", attributes["kernel.syscall"])
	}

	if attributes["kernel.pid"] != "1234" {
		t.Errorf("Expected kernel.pid '1234', got '%s'", attributes["kernel.pid"])
	}

	if attributes["kernel.comm"] != "nginx" {
		t.Errorf("Expected kernel.comm 'nginx', got '%s'", attributes["kernel.comm"])
	}

	if attributes["kernel.arg.filename"] != "/usr/sbin/nginx" {
		t.Errorf("Expected kernel.arg.filename '/usr/sbin/nginx', got '%s'", attributes["kernel.arg.filename"])
	}

	// Check application attributes
	if attributes["app.level"] != "info" {
		t.Errorf("Expected app.level 'info', got '%s'", attributes["app.level"])
	}

	if attributes["app.logger"] != "systemd" {
		t.Errorf("Expected app.logger 'systemd', got '%s'", attributes["app.logger"])
	}

	if attributes["app.unit_type"] != "service" {
		t.Errorf("Expected app.unit_type 'service', got '%s'", attributes["app.unit_type"])
	}
}

func TestSystemdAdapter_ClientCreation(t *testing.T) {
	// Test NewTapioGRPCClient function
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create systemd Tapio client: %v", err)
	}
	defer client.Close()

	stats := client.GetStatistics()
	if stats["collector_id"].(string) != "systemd-collector" {
		t.Errorf("Expected collector ID 'systemd-collector', got '%s'", stats["collector_id"].(string))
	}

	if stats["server_addr"].(string) != "localhost:8080" {
		t.Errorf("Expected server address 'localhost:8080', got '%s'", stats["server_addr"].(string))
	}
}