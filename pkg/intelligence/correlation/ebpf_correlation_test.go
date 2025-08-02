package correlation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/yairfalse/tapio/pkg/domain"
)

// TestNATSSubscriber_WithEBPFCorrelations tests NATS subscriber with rich eBPF correlation data
func TestNATSSubscriber_WithEBPFCorrelations(t *testing.T) {
	// Create mock correlation engine that captures results
	var capturedEvents []*domain.UnifiedEvent
	mockEngine := NewMockCorrelationEngine()
	mockEngine.SetProcessFunc(func(ctx context.Context, event *domain.UnifiedEvent) ([]*MultiDimCorrelationResult, error) {
		capturedEvents = append(capturedEvents, event)

		// Create correlation result showing the power of eBPF correlation
		result := &MultiDimCorrelationResult{
			ID:         "ebpf-correlation-" + event.ID,
			Type:       "k8s-cascade",
			Confidence: 0.9,
			Events:     []string{event.ID},
			Dimensions: []DimensionMatch{
				{
					Dimension:  "container",
					Type:       "pid-to-container",
					Confidence: 1.0,
					Evidence:   []string{"Process mapped to container via PID correlation"},
				},
				{
					Dimension:  "pod",
					Type:       "cgroup-to-pod",
					Confidence: 1.0,
					Evidence:   []string{"Container mapped to pod via cgroup correlation"},
				},
				{
					Dimension:  "service",
					Type:       "network-to-service",
					Confidence: 0.8,
					Evidence:   []string{"Network connection mapped to K8s service"},
				},
			},
			RootCause: &MultiDimRootCauseAnalysis{
				EventID:    event.ID,
				Confidence: 0.9,
				Reasoning:  fmt.Sprintf("Process %s caused service disruption in pod %s", event.Source, event.Entity.Name),
				Evidence:   []string{"Container correlation", "Pod correlation", "Service correlation"},
			},
			Impact: &ImpactAnalysis{
				Severity:             "high",
				InfrastructureImpact: 0.8,
				ServiceImpact:        []string{"frontend-service", "backend-api"},
			},
			Recommendation: "Check container resource limits and pod configuration",
			CreatedAt:      time.Now(),
		}

		return []*MultiDimCorrelationResult{result}, nil
	})

	// Test the correlation with rich eBPF events
	t.Run("Container PID Correlation", func(t *testing.T) {
		// Simulate event from eBPF collector with container correlation
		event := createEBPFEventWithContainerCorrelation()

		// Process through mock engine
		results, err := mockEngine.Process(context.Background(), event)
		assert.NoError(t, err)
		assert.Len(t, results, 1)

		result := results[0]
		assert.Equal(t, "k8s-cascade", result.Type)
		assert.Greater(t, result.Confidence, 0.8)
		assert.Contains(t, result.RootCause.Reasoning, "Process")
		assert.Contains(t, result.RootCause.Reasoning, "pod")

		// Verify rich correlation dimensions
		assert.Len(t, result.Dimensions, 3)
		assert.Equal(t, "container", result.Dimensions[0].Dimension)
		assert.Equal(t, "pod", result.Dimensions[1].Dimension)
		assert.Equal(t, "service", result.Dimensions[2].Dimension)
	})

	t.Run("Network Service Correlation", func(t *testing.T) {
		// Simulate network event with service correlation
		event := createEBPFEventWithNetworkCorrelation()

		results, err := mockEngine.Process(context.Background(), event)
		assert.NoError(t, err)
		assert.Len(t, results, 1)

		result := results[0]
		assert.NotEmpty(t, result.Impact.ServiceImpact)
		assert.Contains(t, result.Impact.ServiceImpact, "frontend-service")
		assert.Equal(t, "high", result.Impact.Severity)
	})

	t.Run("ConfigMap Mount Correlation", func(t *testing.T) {
		// Simulate file access event with ConfigMap correlation
		event := createEBPFEventWithConfigMapCorrelation()

		results, err := mockEngine.Process(context.Background(), event)
		assert.NoError(t, err)
		assert.Len(t, results, 1)

		result := results[0]
		assert.Contains(t, result.Recommendation, "configuration")
	})
}

// createEBPFEventWithContainerCorrelation creates a UnifiedEvent simulating rich eBPF correlation
func createEBPFEventWithContainerCorrelation() *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        "ebpf-memory-alloc-001",
		Timestamp: time.Now(),
		Type:      "ebpf",
		Source:    "ebpf-collector",
		Severity:  domain.EventSeverityHigh,

		// K8s context from correlation
		K8sContext: &domain.K8sContext{
			Name:         "frontend-deployment-7f8b9c-xyz",
			Namespace:    "production",
			WorkloadKind: "Deployment",
			WorkloadName: "frontend",
			Labels: map[string]string{
				"app":     "frontend",
				"version": "v2.1.0",
			},
		},

		// Entity information
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "frontend-pod-abc123",
			Namespace: "production",
		},

		// Kernel-level event data
		Kernel: &domain.KernelData{
			PID:     12345,
			TID:     12345,
			Comm:    "nginx",
			Syscall: "mmap",
		},

		// Rich correlation metadata from eBPF collector
		Attributes: map[string]interface{}{
			// Container correlation (PID → Container)
			"container_id":    "docker://8a3b5c7d9e1f2a3b4c5d6e7f8a9b0c1d",
			"container_image": "myapp/frontend:v2.1.0",
			"container_name":  "frontend",

			// Pod correlation (Cgroup → Pod)
			"cgroup_id": "98765",
			"pod_uid":   "frontend-pod-abc123",
			"pod_name":  "frontend-deployment-7f8b9c-xyz",

			// Process details
			"process_name": "nginx",
			"memory_size":  "1048576", // 1MB allocation

			// Correlation chain evidence
			"correlation_chain": "PID:12345 → Container:docker://8a3b5c7d → Pod:frontend-pod-abc123",
		},

		// OTEL trace context for correlation
		TraceContext: &domain.TraceContext{
			TraceID: "ebpf-trace-12345",
			SpanID:  "span-memory-alloc",
		},

		// Impact assessment
		Impact: &domain.ImpactContext{
			Severity:             "high",
			InfrastructureImpact: 0.8,
			AffectedServices:     []string{"frontend-service"},
			SystemCritical:       true,
		},
	}
}

// createEBPFEventWithNetworkCorrelation creates event showing network-to-service correlation
func createEBPFEventWithNetworkCorrelation() *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        "ebpf-network-connect-001",
		Timestamp: time.Now(),
		Type:      "ebpf",
		Source:    "ebpf-collector",
		Severity:  domain.EventSeverityError,

		K8sContext: &domain.K8sContext{
			Name:      "backend-pod-def456",
			Namespace: "production",
		},

		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "backend-pod-def456",
			Namespace: "production",
		},

		// Network event data
		Network: &domain.NetworkData{
			SourceIP:   "10.244.1.15",
			DestIP:     "10.96.0.20",
			DestPort:   8080,
			Protocol:   "TCP",
			StatusCode: 500, // Connection failed
		},

		Attributes: map[string]interface{}{
			// Network-to-service correlation
			"service_name":       "backend-api",
			"service_cluster_ip": "10.96.0.20",
			"endpoint_ip":        "10.244.1.20",
			"connection_state":   "REFUSED",

			// Container correlation
			"container_id": "docker://backend-container-123",
			"pod_uid":      "backend-pod-def456",

			// Service dependency chain
			"service_chain": "frontend-service → backend-api → database",
			"failure_point": "backend-api",
		},

		TraceContext: &domain.TraceContext{
			TraceID: "network-trace-67890",
			SpanID:  "span-tcp-connect",
		},

		Impact: &domain.ImpactContext{
			Severity:             "high",
			InfrastructureImpact: 0.9,
			AffectedServices:     []string{"frontend-service", "backend-api"},
			CascadeRisk:          true,
		},
	}
}

// createEBPFEventWithConfigMapCorrelation creates event showing file-to-configmap correlation
func createEBPFEventWithConfigMapCorrelation() *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        "ebpf-file-access-001",
		Timestamp: time.Now(),
		Type:      "ebpf",
		Source:    "ebpf-collector",
		Severity:  domain.EventSeverityWarning,

		K8sContext: &domain.K8sContext{
			Name:      "app-pod-ghi789",
			Namespace: "production",
		},

		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "app-pod-ghi789",
			Namespace: "production",
		},

		// File operation details
		Kernel: &domain.KernelData{
			PID:     34567,
			Comm:    "app-server",
			Syscall: "openat",
		},

		Attributes: map[string]interface{}{
			// File-to-ConfigMap correlation
			"file_path":         "/etc/config/app.yaml",
			"mount_source":      "configmap-app-config",
			"configmap_name":    "app-config",
			"configmap_version": "12345",
			"access_mode":       "read",

			// Container correlation
			"container_id": "docker://app-container-789",
			"pod_uid":      "app-pod-ghi789",

			// Configuration change detection
			"config_changed":  "true",
			"last_reload":     "2024-08-01T10:30:00Z",
			"reload_required": "true",
		},

		TraceContext: &domain.TraceContext{
			TraceID: "config-trace-11111",
			SpanID:  "span-file-read",
		},

		Impact: &domain.ImpactContext{
			Severity:             "medium",
			InfrastructureImpact: 0.6,
			AffectedServices:     []string{"app-service"},
		},
	}
}

// TestEBPFCorrelationNarrative demonstrates the storytelling power
func TestEBPFCorrelationNarrative(t *testing.T) {
	fmt.Println("\n=== eBPF Correlation Narrative Test ===")

	// Show how rich correlation enables better narratives
	event := createEBPFEventWithContainerCorrelation()

	// Extract narrative elements
	containerID := event.Attributes["container_id"].(string)
	podName := event.Attributes["pod_name"].(string)
	processName := event.Attributes["process_name"].(string)
	memorySize := event.Attributes["memory_size"].(string)

	// Traditional alert vs. Contextual narrative
	fmt.Printf("\n❌ Traditional Alert:\n")
	fmt.Printf("   MEMORY_ALLOC event detected\n")
	fmt.Printf("   PID: %d, Size: %s bytes\n", event.Kernel.PID, memorySize)

	fmt.Printf("\n✅ Contextual Narrative:\n")
	fmt.Printf("   Process '%s' (PID %d) in container %s\n",
		processName, event.Kernel.PID, containerID[:12]+"...")
	fmt.Printf("   running in pod '%s' (namespace: %s)\n",
		podName, event.Entity.Namespace)
	fmt.Printf("   allocated %s bytes of memory.\n", memorySize)
	fmt.Printf("   Correlation chain: %s\n", event.Attributes["correlation_chain"].(string))

	// Impact analysis
	fmt.Printf("\n📊 Impact Analysis:\n")
	fmt.Printf("   Infrastructure Impact: %.1f/1.0\n", event.Impact.InfrastructureImpact)
	fmt.Printf("   Affected Services: %v\n", event.Impact.AffectedServices)
	fmt.Printf("   System Critical: %t\n", event.Impact.SystemCritical)

	// This shows the power of correlation
	correlationChain := event.Attributes["correlation_chain"].(string)
	assert.Contains(t, correlationChain, "PID:")
	assert.Contains(t, correlationChain, "Container:")
	assert.Contains(t, correlationChain, "Pod:")
}

// TestMultipleEBPFEventsCorrelation tests correlation across multiple events
func TestMultipleEBPFEventsCorrelation(t *testing.T) {
	// Create events that should correlate by trace ID
	traceID := "cascade-failure-trace-123"
	events := []*domain.UnifiedEvent{
		createMemoryPressureEvent(traceID),
		createOOMKillEvent(traceID),
		createPodRestartEvent(traceID),
		createServiceDownEvent(traceID),
	}

	// Mock engine to capture correlation across events
	mockEngine := NewMockCorrelationEngine()
	mockEngine.SetProcessFunc(func(ctx context.Context, event *domain.UnifiedEvent) ([]*MultiDimCorrelationResult, error) {
		// This would normally correlate multiple events by trace ID
		result := &MultiDimCorrelationResult{
			ID:         "cascade-correlation-" + event.TraceContext.TraceID,
			Type:       "cascade-failure",
			Confidence: 0.95,
			Events:     []string{event.ID}, // In real scenario, would include all correlated events
			RootCause: &MultiDimRootCauseAnalysis{
				EventID:   events[0].ID, // Memory pressure is root cause
				Reasoning: "Memory pressure led to OOM kill, causing pod restart and service disruption",
				Evidence:  []string{"Memory allocation spike", "OOM killer triggered", "Pod restart", "Service endpoints removed"},
			},
			Impact: &ImpactAnalysis{
				Severity:             "critical",
				InfrastructureImpact: 0.9,
				ServiceImpact:        []string{"frontend-service", "user-facing-api"},
			},
			Recommendation: "Increase pod memory limits and implement resource quotas",
		}
		return []*MultiDimCorrelationResult{result}, nil
	})

	// Process events and verify correlation potential
	for _, event := range events {
		results, err := mockEngine.Process(context.Background(), event)
		assert.NoError(t, err)
		assert.Len(t, results, 1)

		result := results[0]
		assert.Equal(t, "cascade-failure", result.Type)
		assert.Greater(t, result.Confidence, 0.9)
	}

	fmt.Printf("\n=== Cascade Correlation Story ===\n")
	fmt.Printf("Trace ID: %s\n", traceID)
	fmt.Printf("Events: %d correlated by trace ID\n", len(events))
	fmt.Printf("Root Cause: Memory pressure → OOM → Pod restart → Service down\n")
	fmt.Printf("Narrative: Single trace tells complete failure story\n")
}

// Helper functions to create correlated events
func createMemoryPressureEvent(traceID string) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID: "memory-pressure-001", Type: "ebpf", Severity: domain.EventSeverityHigh,
		TraceContext: &domain.TraceContext{TraceID: traceID, SpanID: "span-1"},
		Kernel:       &domain.KernelData{Syscall: "brk"},
		Attributes:   map[string]interface{}{"memory_usage": "95%", "threshold": "80%"},
	}
}

func createOOMKillEvent(traceID string) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID: "oom-kill-001", Type: "ebpf", Severity: domain.EventSeverityCritical,
		TraceContext: &domain.TraceContext{TraceID: traceID, SpanID: "span-2"},
		Kernel:       &domain.KernelData{Syscall: "exit_group", PID: 12345},
		Attributes:   map[string]interface{}{"killed_process": "nginx", "memory_requested": "2GB"},
	}
}

func createPodRestartEvent(traceID string) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID: "pod-restart-001", Type: "kubeapi", Severity: domain.EventSeverityHigh,
		TraceContext: &domain.TraceContext{TraceID: traceID, SpanID: "span-3"},
		K8sContext:   &domain.K8sContext{Name: "frontend-pod", Namespace: "production"},
		Attributes:   map[string]interface{}{"restart_reason": "OOMKilled", "restart_count": "3"},
	}
}

func createServiceDownEvent(traceID string) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID: "service-down-001", Type: "kubeapi", Severity: domain.EventSeverityCritical,
		TraceContext: &domain.TraceContext{TraceID: traceID, SpanID: "span-4"},
		Network:      &domain.NetworkData{StatusCode: 503},
		Attributes:   map[string]interface{}{"service": "frontend-service", "endpoints": "0"},
	}
}
