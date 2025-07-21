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
				if !strings.HasPrefix(event.ID, "cni-") {
					t.Errorf("expected event ID to start with 'cni-', got %s", event.ID)
				}
				if event.Source != string(domain.SourceCNI) {
					t.Errorf("expected source %s, got %s", domain.SourceCNI, event.Source)
				}
				if event.Type != domain.EventTypeNetwork {
					t.Errorf("expected type %s, got %s", domain.EventTypeNetwork, event.Type)
				}

				// Verify Semantic context
				if event.Semantic == nil {
					t.Fatal("Semantic context should not be nil")
				}
				sem := event.Semantic
				if sem.Intent != "pod-network-attached" {
					t.Errorf("expected intent 'pod-network-attached', got %s", sem.Intent)
				}
				if sem.Category != "lifecycle" {
					t.Errorf("expected category 'lifecycle', got %s", sem.Category)
				}
				if len(sem.Tags) == 0 {
					t.Error("expected semantic tags to be present")
				}
				if sem.Narrative == "" {
					t.Error("expected narrative to be present")
				}
				if sem.Confidence <= 0 || sem.Confidence > 1 {
					t.Errorf("expected confidence between 0 and 1, got %f", sem.Confidence)
				}

				// Verify Entity context
				if event.Entity == nil {
					t.Fatal("Entity context should not be nil")
				}
				entity := event.Entity
				if entity.Type != "Pod" {
					t.Errorf("expected entity type 'Pod', got %s", entity.Type)
				}
				if entity.Name != "nginx-deployment-abc123" {
					t.Errorf("expected entity name 'nginx-deployment-abc123', got %s", entity.Name)
				}
				if entity.Namespace != "default" {
					t.Errorf("expected namespace 'default', got %s", entity.Namespace)
				}
				if entity.UID != "pod-uid-123" {
					t.Errorf("expected UID 'pod-uid-123', got %s", entity.UID)
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
				if k8s.EventType != "Normal" {
					t.Errorf("expected event type 'Normal', got %s", k8s.EventType)
				}
				if k8s.Reason != "NetworkAttached" {
					t.Errorf("expected reason 'NetworkAttached', got %s", k8s.Reason)
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
				if net.Headers["cni_plugin"] != "cilium" {
					t.Errorf("expected plugin cilium, got %s", net.Headers["cni_plugin"])
				}
				if net.Direction != "ingress" {
					t.Errorf("expected direction 'ingress', got %s", net.Direction)
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
				impact := event.Impact
				if impact.Severity != "info" {
					t.Errorf("expected severity 'info', got %s", impact.Severity)
				}
				if impact.BusinessImpact < 0 || impact.BusinessImpact > 1 {
					t.Errorf("expected business impact between 0 and 1, got %f", impact.BusinessImpact)
				}
				if len(impact.AffectedServices) == 0 {
					t.Error("expected affected services to be present")
				}
				if impact.AffectedUsers != 0 {
					t.Errorf("expected 0 affected users for successful operation, got %d", impact.AffectedUsers)
				}
			},
		},
		{
			name: "failed DEL operation in production",
			rawEvent: core.CNIRawEvent{
				ID:           "test-del-fail",
				Timestamp:    time.Now(),
				Source:       "test",
				Operation:    core.CNIOperationDel,
				PluginName:   "calico",
				Success:      false,
				ErrorMessage: "failed to delete interface",
				Duration:     5 * time.Second,
				PodName:      "payment-service-xyz",
				PodNamespace: "production",
				Labels: map[string]string{
					"service": "payment",
				},
			},
			validate: func(t *testing.T, event *domain.UnifiedEvent, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				// Verify semantic context for failure
				if event.Semantic == nil || event.Semantic.Intent != "network-cleanup-failed" {
					t.Error("expected intent 'network-cleanup-failed'")
				}
				if event.Semantic.Category != "reliability" {
					t.Errorf("expected category 'reliability', got %s", event.Semantic.Category)
				}

				// Verify Kubernetes context for failure
				if event.Kubernetes == nil || event.Kubernetes.EventType != "Warning" {
					t.Error("expected Kubernetes event type 'Warning'")
				}
				if event.Kubernetes.Reason != "NetworkCleanupFailed" {
					t.Errorf("expected reason 'NetworkCleanupFailed', got %s", event.Kubernetes.Reason)
				}

				// Verify severity in impact context
				if event.Impact == nil || event.Impact.Severity != "high" {
					t.Errorf("expected high severity for production failure, got %s", event.Impact.Severity)
				}
				if !event.Impact.CustomerFacing {
					t.Error("expected customer-facing to be true for production namespace")
				}
				if !event.Impact.RevenueImpacting {
					t.Error("expected revenue-impacting to be true for payment service")
				}
				if !event.Impact.SLOImpact {
					t.Error("expected SLO impact for high severity production failure")
				}
			},
		},
		{
			name: "slow operation in kube-system",
			rawEvent: core.CNIRawEvent{
				ID:           "test-slow-op",
				Timestamp:    time.Now(),
				Source:       "test",
				Operation:    core.CNIOperationAdd,
				PluginName:   "flannel",
				Success:      true,
				Duration:     15 * time.Second,
				PodName:      "kube-dns-abc",
				PodNamespace: "kube-system",
				AssignedIP:   "10.96.0.10",
			},
			validate: func(t *testing.T, event *domain.UnifiedEvent, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				// Verify severity for slow operation
				if event.Impact == nil || event.Impact.Severity != "warning" {
					t.Errorf("expected warning severity for slow operation, got %s", event.Impact.Severity)
				}

				// Verify semantic tags include slow-operation
				found := false
				for _, tag := range event.Semantic.Tags {
					if tag == "system-critical" {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected 'system-critical' tag for kube-system namespace")
				}

				// Verify affected services include system services
				found = false
				for _, svc := range event.Impact.AffectedServices {
					if strings.Contains(svc, "kubernetes-networking") {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected kubernetes-networking in affected services")
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

				// Semantic context should still be present
				if event.Semantic == nil {
					t.Fatal("Semantic context should not be nil")
				}
				if event.Semantic.Intent != "network-health-verified" {
					t.Errorf("expected intent 'network-health-verified', got %s", event.Semantic.Intent)
				}

				// Entity context should be present but minimal
				if event.Entity == nil {
					t.Fatal("Entity context should not be nil")
				}

				// Optional contexts should be nil for minimal event
				if event.Kubernetes != nil {
					t.Error("Kubernetes context should be nil for minimal event")
				}
				if event.Network != nil {
					t.Error("Network context should be nil for minimal event")
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

func TestCNIEventProcessor_SemanticIntent(t *testing.T) {
	processor := &cniEventProcessor{}

	tests := []struct {
		name     string
		raw      core.CNIRawEvent
		expected string
	}{
		{
			name: "successful ADD with IP",
			raw: core.CNIRawEvent{
				Operation:  core.CNIOperationAdd,
				Success:    true,
				AssignedIP: "10.244.1.10",
			},
			expected: "pod-network-attached",
		},
		{
			name: "successful ADD without IP",
			raw: core.CNIRawEvent{
				Operation: core.CNIOperationAdd,
				Success:   true,
			},
			expected: "network-interface-created",
		},
		{
			name: "failed ADD",
			raw: core.CNIRawEvent{
				Operation: core.CNIOperationAdd,
				Success:   false,
			},
			expected: "network-setup-failed",
		},
		{
			name: "successful DEL",
			raw: core.CNIRawEvent{
				Operation: core.CNIOperationDel,
				Success:   true,
			},
			expected: "pod-network-detached",
		},
		{
			name: "failed CHECK",
			raw: core.CNIRawEvent{
				Operation: core.CNIOperationCheck,
				Success:   false,
			},
			expected: "network-connectivity-lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.determineSemanticIntent(tt.raw)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestCNIEventProcessor_BusinessImpact(t *testing.T) {
	processor := &cniEventProcessor{}

	tests := []struct {
		name     string
		raw      core.CNIRawEvent
		severity string
		minScore float64
		maxScore float64
	}{
		{
			name: "critical failure in production",
			raw: core.CNIRawEvent{
				PodNamespace: "production",
				Success:      false,
			},
			severity: "critical",
			minScore: 0.9,
			maxScore: 1.0,
		},
		{
			name: "normal operation in default namespace",
			raw: core.CNIRawEvent{
				PodNamespace: "default",
				Success:      true,
			},
			severity: "info",
			minScore: 0.1,
			maxScore: 0.6,
		},
		{
			name: "failure in kube-system",
			raw: core.CNIRawEvent{
				PodNamespace: "kube-system",
				Success:      false,
			},
			severity: "warning",
			minScore: 0.6,
			maxScore: 0.8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := processor.calculateBusinessImpact(tt.raw, tt.severity)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("expected score between %f and %f, got %f", tt.minScore, tt.maxScore, score)
			}
		})
	}
}

func TestCNIEventProcessor_CustomerFacing(t *testing.T) {
	processor := &cniEventProcessor{}

	tests := []struct {
		name     string
		raw      core.CNIRawEvent
		expected bool
	}{
		{
			name: "production namespace",
			raw: core.CNIRawEvent{
				PodNamespace: "production",
			},
			expected: true,
		},
		{
			name: "kube-system namespace",
			raw: core.CNIRawEvent{
				PodNamespace: "kube-system",
			},
			expected: false,
		},
		{
			name: "frontend tier label",
			raw: core.CNIRawEvent{
				PodNamespace: "staging",
				Labels: map[string]string{
					"tier": "frontend",
				},
			},
			expected: true,
		},
		{
			name: "customer-facing label",
			raw: core.CNIRawEvent{
				PodNamespace: "dev",
				Labels: map[string]string{
					"customer-facing": "true",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.isCustomerFacing(tt.raw)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
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

func TestCNIEventProcessor_TraceContextExtraction(t *testing.T) {
	processor := &cniEventProcessor{}

	tests := []struct {
		name         string
		annotations  map[string]string
		expectTrace  bool
		expectedID   string
		expectedSpan string
	}{
		{
			name: "OpenTelemetry annotations",
			annotations: map[string]string{
				"opentelemetry.io/trace-id": "abc123def456789",
				"opentelemetry.io/span-id":  "span987654321",
			},
			expectTrace:  true,
			expectedID:   "abc123def456789",
			expectedSpan: "span987654321",
		},
		{
			name: "Jaeger annotations",
			annotations: map[string]string{
				"jaeger.trace-id": "jaeger-trace-123",
				"jaeger.span-id":  "jaeger-span-456",
			},
			expectTrace:  true,
			expectedID:   "jaeger-trace-123",
			expectedSpan: "jaeger-span-456",
		},
		{
			name: "Simple trace headers",
			annotations: map[string]string{
				"trace-id": "simple-trace-id",
				"span-id":  "simple-span-id",
			},
			expectTrace:  true,
			expectedID:   "simple-trace-id",
			expectedSpan: "simple-span-id",
		},
		{
			name:        "No trace annotations",
			annotations: map[string]string{},
			expectTrace: false,
		},
		{
			name: "Empty trace values",
			annotations: map[string]string{
				"trace-id": "",
				"span-id":  "",
			},
			expectTrace: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawEvent := core.CNIRawEvent{
				Annotations: tt.annotations,
			}

			traceCtx := processor.extractTraceContext(rawEvent)

			if tt.expectTrace {
				if traceCtx == nil {
					t.Fatal("expected trace context but got nil")
				}
				if traceCtx.TraceID != tt.expectedID {
					t.Errorf("expected trace ID %s, got %s", tt.expectedID, traceCtx.TraceID)
				}
				if traceCtx.SpanID != tt.expectedSpan {
					t.Errorf("expected span ID %s, got %s", tt.expectedSpan, traceCtx.SpanID)
				}
			} else {
				if traceCtx != nil {
					t.Errorf("expected no trace context but got %+v", traceCtx)
				}
			}
		})
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
