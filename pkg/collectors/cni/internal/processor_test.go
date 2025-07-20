package internal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestCNIEventProcessor_ProcessEvent(t *testing.T) {
	processor := newCNIEventProcessor()
	ctx := context.Background()

	tests := []struct {
		name     string
		rawEvent core.CNIRawEvent
		validate func(t *testing.T, event *domain.UnifiedEvent, err error)
	}{
		{
			name: "successful ADD operation with IP allocation",
			rawEvent: core.CNIRawEvent{
				ID:            "test-add-1",
				Timestamp:     time.Now(),
				Source:        "test",
				Operation:     core.CNIOperationAdd,
				PluginName:    "cilium",
				Success:       true,
				Duration:      100 * time.Millisecond,
				PodName:       "nginx-deployment-abc123",
				PodUID:        "pod-uid-123",
				PodNamespace:  "default",
				ContainerID:   "container-123",
				AssignedIP:    "10.244.1.10",
				Subnet:        "10.244.1.0/24",
				Gateway:       "10.244.1.1",
				InterfaceName: "eth0",
				NodeName:      "worker-node-1",
				ClusterName:   "test-cluster",
				Labels: map[string]string{
					"app":     "nginx",
					"version": "1.20",
				},
				Annotations: map[string]string{
					"trace-id": "abc123def456",
				},
			},
			validate: func(t *testing.T, event *domain.UnifiedEvent, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if event == nil {
					t.Fatal("event is nil")
				}

				// Verify basic event properties
				if event.ID == "" {
					t.Error("event ID should not be empty")
				}
				if event.Source != string(domain.SourceCNI) {
					t.Errorf("expected source %s, got %s", domain.SourceCNI, event.Source)
				}
				if event.Type != domain.EventTypeNetwork {
					t.Errorf("expected type %s, got %s", domain.EventTypeNetwork, event.Type)
				}

				// Verify Kubernetes context
				if event.Kubernetes == nil {
					t.Fatal("Kubernetes context should not be nil")
				}
				k8s := event.Kubernetes
				if !strings.Contains(k8s.Object, "nginx-deployment-abc123") {
					t.Errorf("expected pod name nginx-deployment-abc123 in object %s", k8s.Object)
				}
				if k8s.ObjectKind != "Pod" {
					t.Errorf("expected object kind Pod, got %s", k8s.ObjectKind)
				}

				// Verify Network context
				if event.Network == nil {
					t.Fatal("Network context should not be nil")
				}
				net := event.Network
				if net.SourceIP != "10.244.1.10" {
					t.Errorf("expected IP 10.244.1.10, got %s", net.SourceIP)
				}
				if net.Headers["subnet"] != "10.244.1.0/24" {
					t.Errorf("expected subnet 10.244.1.0/24, got %s", net.Headers["subnet"])
				}
				if net.Headers["cni-plugin"] != "cilium" {
					t.Errorf("expected plugin cilium, got %s", net.Headers["cni-plugin"])
				}

				// Verify Application context
				if event.Application == nil {
					t.Fatal("Application context should not be nil")
				}
				app := event.Application
				if !strings.Contains(app.Message, "nginx") {
					t.Errorf("expected app message to contain nginx, got %s", app.Message)
				}
				if app.Custom["node_name"] != "worker-node-1" {
					t.Errorf("expected node worker-node-1, got %v", app.Custom["node_name"])
				}

				// Verify Trace context
				if event.TraceContext == nil {
					t.Fatal("Trace context should not be nil")
				}
				trace := event.TraceContext
				if trace.TraceID != "abc123def456" {
					t.Errorf("expected trace ID abc123def456, got %s", trace.TraceID)
				}

				// Verify Impact context
				if event.Impact == nil {
					t.Fatal("Impact context should not be nil")
				}
				if event.Impact.Severity == "" {
					t.Error("expected severity to be set")
				}
			},
		},
		{
			name: "failed DEL operation",
			rawEvent: core.CNIRawEvent{
				ID:           "test-del-fail",
				Timestamp:    time.Now(),
				Source:       "test",
				Operation:    core.CNIOperationDel,
				PluginName:   "calico",
				Success:      false,
				ErrorMessage: "failed to delete interface",
				Duration:     5 * time.Second,
				PodName:      "failed-pod",
			},
			validate: func(t *testing.T, event *domain.UnifiedEvent, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				// Verify error handling in application context
				if event.Application == nil || event.Application.Level != "error" {
					t.Error("expected error level in application context")
				}

				// Verify severity in impact context
				if event.Impact == nil || event.Impact.Severity != "error" {
					t.Error("expected error severity in impact context")
				}

				// Verify message is in raw data
				if !strings.Contains(string(event.RawData), "failed") {
					t.Errorf("expected failure message in raw data, got %s", string(event.RawData))
				}
			},
		},
		{
			name: "minimal event with just operation",
			rawEvent: core.CNIRawEvent{
				ID:        "minimal-event",
				Timestamp: time.Now(),
				Source:    "test",
				Operation: core.CNIOperationCheck,
				Success:   true,
			},
			validate: func(t *testing.T, event *domain.UnifiedEvent, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				// Should still create valid event
				if event.ID == "" {
					t.Error("event ID should not be empty")
				}
				if event.Source != string(domain.SourceCNI) {
					t.Errorf("expected source %s, got %s", domain.SourceCNI, event.Source)
				}

				// Optional contexts should be nil for minimal event
				if event.Kubernetes != nil {
					t.Error("Kubernetes context should be nil for minimal event")
				}
				if event.Application != nil {
					t.Error("Application context should be nil for minimal event")
				}

				// Impact context should still be present
				if event.Impact == nil {
					t.Error("Impact context should be present")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := processor.ProcessEvent(ctx, tt.rawEvent)
			tt.validate(t, event, err)
		})
	}
}

func TestCNIEventProcessor_EventTypeMapping(t *testing.T) {
	processor := &cniEventProcessor{}

	tests := []struct {
		name     string
		cniType  core.CNIEventType
		expected domain.EventType
	}{
		{"IP allocation", core.CNIEventTypeIPAllocation, domain.EventTypeNetwork},
		{"IP deallocation", core.CNIEventTypeIPDeallocation, domain.EventTypeNetwork},
		{"Interface setup", core.CNIEventTypeInterfaceSetup, domain.EventTypeNetwork},
		{"Interface teardown", core.CNIEventTypeInterfaceTeardown, domain.EventTypeNetwork},
		{"Policy apply", core.CNIEventTypePolicyApply, domain.EventTypeSystem},
		{"Error", core.CNIEventTypeError, domain.EventTypeSystem},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.mapEventTypeToDomain(tt.cniType)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestCNIEventProcessor_SeverityMapping(t *testing.T) {
	processor := &cniEventProcessor{}

	tests := []struct {
		name     string
		cniSev   core.CNISeverity
		expected string
	}{
		{"Info", core.CNISeverityInfo, "info"},
		{"Warning", core.CNISeverityWarning, "warning"},
		{"Error", core.CNISeverityError, "error"},
		{"Critical", core.CNISeverityCritical, "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.mapSeverityToDomain(tt.cniSev)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestCNIEventProcessor_ApplicationNameExtraction(t *testing.T) {
	tests := []struct {
		name     string
		podName  string
		labels   map[string]string
		expected string
	}{
		{
			name:    "standard Kubernetes app label",
			podName: "nginx-deployment-abc123",
			labels: map[string]string{
				"app.kubernetes.io/name": "nginx",
			},
			expected: "nginx",
		},
		{
			name:    "simple app label",
			podName: "redis-master-xyz789",
			labels: map[string]string{
				"app": "redis",
			},
			expected: "redis",
		},
		{
			name:     "extract from pod name with deployment suffix",
			podName:  "webapp-deployment-5c6c8d7f9-abc123",
			labels:   map[string]string{},
			expected: "webapp",
		},
		{
			name:     "extract from pod name with single suffix",
			podName:  "database-abc123",
			labels:   map[string]string{},
			expected: "database",
		},
		{
			name:     "simple pod name",
			podName:  "simple-pod",
			labels:   map[string]string{},
			expected: "simple-pod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractApplicationName(tt.podName, tt.labels)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestCNIEventProcessor_MessageGeneration(t *testing.T) {
	processor := &cniEventProcessor{}

	tests := []struct {
		name      string
		rawEvent  core.CNIRawEvent
		eventType core.CNIEventType
		contains  []string
	}{
		{
			name: "successful IP allocation",
			rawEvent: core.CNIRawEvent{
				Success:    true,
				AssignedIP: "10.244.1.10",
				PodName:    "nginx-pod",
				PluginName: "cilium",
			},
			eventType: core.CNIEventTypeIPAllocation,
			contains:  []string{"IP", "10.244.1.10", "allocated", "nginx-pod", "cilium"},
		},
		{
			name: "failed operation",
			rawEvent: core.CNIRawEvent{
				Success:      false,
				PodName:      "failed-pod",
				Operation:    core.CNIOperationAdd,
				ErrorMessage: "timeout waiting for response",
			},
			eventType: core.CNIEventTypeError,
			contains:  []string{"failed", "failed-pod", "timeout waiting for response"},
		},
		{
			name: "interface setup",
			rawEvent: core.CNIRawEvent{
				Success:       true,
				InterfaceName: "eth0",
				PodName:       "web-pod",
			},
			eventType: core.CNIEventTypeInterfaceSetup,
			contains:  []string{"interface", "eth0", "configured", "web-pod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := processor.generateMessage(tt.rawEvent, tt.eventType)
			for _, expected := range tt.contains {
				if !contains(message, expected) {
					t.Errorf("expected message to contain '%s', got: %s", expected, message)
				}
			}
		})
	}
}

// Benchmark tests
func BenchmarkCNIEventProcessor_ProcessEvent(b *testing.B) {
	processor := newCNIEventProcessor()
	ctx := context.Background()

	testEvent := core.CNIRawEvent{
		ID:          "bench-event",
		Timestamp:   time.Now(),
		Source:      "bench",
		Operation:   core.CNIOperationAdd,
		PluginName:  "cilium",
		Success:     true,
		Duration:    100 * time.Millisecond,
		PodName:     "bench-pod",
		PodUID:      "bench-uid",
		AssignedIP:  "10.244.1.10",
		Subnet:      "10.244.1.0/24",
		Gateway:     "10.244.1.1",
		NodeName:    "bench-node",
		ClusterName: "bench-cluster",
		Labels: map[string]string{
			"app": "benchmark",
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := processor.ProcessEvent(ctx, testEvent)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (len(substr) == 0 || s != "" &&
		len(s) >= len(substr) && s[0:len(substr)] == substr ||
		len(s) > len(substr) && contains(s[1:], substr))
}
