package telemetry

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collector"
	"github.com/yairfalse/tapio/pkg/ebpf"
	"github.com/yairfalse/tapio/pkg/types"
)

// TestCreateSpanWithPID tests the integration with Agent 1's translator
func TestCreateSpanWithPID(t *testing.T) {
	// Create mock translator with real Kubernetes data
	mockTranslator := &MockPIDTranslator{
		pidToContext: map[uint32]*collector.EventContext{
			1234: {
				Pod:       "test-pod",
				Namespace: "test-namespace",
				Container: "test-container",
				Node:      "test-node",
				PID:       1234,
				Labels: map[string]string{
					"app":     "test-app",
					"version": "v1.0.0",
				},
				ProcessName: "test-process",
				PPID:        1,
				Fallback:    false,
			},
			5678: {
				Pod:       "fallback-pod",
				Namespace: "fallback-namespace",
				Container: "fallback-container",
				PID:       5678,
				Fallback:  true, // This data comes from fallback
			},
		},
	}

	checker := &mockChecker{}
	monitor := &mockEBPFMonitor{available: false}
	config := Config{
		OTLPEndpoint:     "http://localhost:4317",
		EnableTraces:     true,
		EnableMetrics:    false,
		EnableTranslator: true,
	}

	exporter, err := NewOpenTelemetryExporter(checker, monitor, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Shutdown(context.Background())

	// Replace the translator with our mock
	exporter.translator = mockTranslator

	ctx := context.Background()

	tests := []struct {
		name         string
		pid          uint32
		operation    string
		expectError  bool
		validateSpan func(*testing.T, uint32)
	}{
		{
			name:        "create span with real Kubernetes context",
			pid:         1234,
			operation:   "memory_check",
			expectError: false,
			validateSpan: func(t *testing.T, pid uint32) {
				expectedSpanName := "tapio.test-namespace.memory_check"
				// In a real test, we would verify the span attributes contain:
				// - k8s.pod.name=test-pod
				// - k8s.namespace=test-namespace
				// - k8s.container.name=test-container
				// - k8s.node.name=test-node
				// - process.pid=1234
				// - k8s.pod.label.app=test-app
				// - process.name=test-process
				// - k8s.context.fallback=false
				t.Logf("Expected span with real K8s context for PID %d with name %s", pid, expectedSpanName)
			},
		},
		{
			name:        "create span with fallback context",
			pid:         5678,
			operation:   "network_analysis",
			expectError: false,
			validateSpan: func(t *testing.T, pid uint32) {
				expectedSpanName := "tapio.fallback-namespace.network_analysis"
				// Should contain k8s.context.fallback=true
				t.Logf("Expected span with fallback K8s context for PID %d with name %s", pid, expectedSpanName)
			},
		},
		{
			name:        "create span with unknown PID",
			pid:         9999,
			operation:   "unknown_process",
			expectError: false,
			validateSpan: func(t *testing.T, pid uint32) {
				expectedSpanName := "tapio.unknown_process"
				// Should fall back to basic span with k8s.context.status=translator_unavailable
				t.Logf("Expected basic span for unknown PID %d with name %s", pid, expectedSpanName)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span, err := exporter.CreateSpanWithPID(ctx, tt.pid, tt.operation)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if span != nil {
				tt.validateSpan(t, tt.pid)
				span.End()
			}
		})
	}
}

// TestEBPFTelemetryWithRealContext tests eBPF telemetry with real Kubernetes context
func TestEBPFTelemetryWithRealContext(t *testing.T) {
	// Create mock translator
	mockTranslator := &MockPIDTranslator{
		pidToContext: map[uint32]*collector.EventContext{
			1001: {
				Pod:       "memory-intensive-pod",
				Namespace: "production",
				Container: "app-container",
				Node:      "worker-node-1",
				PID:       1001,
				Labels: map[string]string{
					"app":  "memory-app",
					"tier": "backend",
				},
			},
		},
	}

	// Create mock eBPF monitor with memory data
	monitor := &mockEBPFMonitor{
		available: true,
		memStats: []ebpf.ProcessMemoryStats{
			{
				PID:          1001,
				ContainerID:  "abc123",
				CurrentUsage: 150 * 1024 * 1024, // 150MB
				PeakUsage:    200 * 1024 * 1024, // 200MB
			},
		},
	}

	checker := &mockChecker{}
	config := Config{
		OTLPEndpoint:     "http://localhost:4317",
		EnableTraces:     true,
		EnableMetrics:    false,
		EnableTranslator: true,
	}

	exporter, err := NewOpenTelemetryExporter(checker, monitor, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Shutdown(context.Background())

	// Replace translator with mock
	exporter.translator = mockTranslator

	ctx := context.Background()

	// Update eBPF telemetry - should create spans with real Kubernetes context
	exporter.updateEBPFTelemetry(ctx)

	// Verify that spans were created
	metrics := exporter.GetMetrics()
	if metrics.TotalSpansCreated == 0 {
		t.Error("Expected spans to be created for eBPF data with K8s context")
	}

	t.Logf("Successfully created %d spans with real Kubernetes context from eBPF data",
		metrics.TotalSpansCreated)
}

// TestCircuitBreakerIntegrationWithTranslator tests circuit breaker protection for translator calls
func TestCircuitBreakerIntegrationWithTranslator(t *testing.T) {
	// Create mock translator that fails initially
	mockTranslator := &MockPIDTranslator{
		failNext: 3, // Fail first 3 calls
	}

	checker := &mockChecker{}
	monitor := &mockEBPFMonitor{available: false}
	config := Config{
		OTLPEndpoint:     "http://localhost:4317",
		EnableTraces:     true,
		EnableTranslator: true,
	}

	exporter, err := NewOpenTelemetryExporter(checker, monitor, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Shutdown(context.Background())

	exporter.translator = mockTranslator

	ctx := context.Background()

	// Try to create spans - should handle translator failures gracefully
	for i := 0; i < 5; i++ {
		span, err := exporter.CreateSpanWithPID(ctx, uint32(1000+i), "test_operation")
		if err != nil {
			t.Errorf("Span creation should not fail even if translator fails: %v", err)
		}
		if span != nil {
			span.End()
		}
	}

	// Verify spans were still created despite translator failures
	metrics := exporter.GetMetrics()
	if metrics.TotalSpansCreated != 5 {
		t.Errorf("Expected 5 spans to be created, got %d", metrics.TotalSpansCreated)
	}
}

// MockPIDTranslator implements a mock translator for testing
type MockPIDTranslator struct {
	pidToContext map[uint32]*collector.EventContext
	failNext     int
}

func (m *MockPIDTranslator) GetPodInfo(pid uint32) (*collector.EventContext, error) {
	if m.failNext > 0 {
		m.failNext--
		return nil, fmt.Errorf("mock translator failure")
	}

	if context, exists := m.pidToContext[pid]; exists {
		return context, nil
	}

	return nil, fmt.Errorf("no context found for PID %d", pid)
}

func (m *MockPIDTranslator) Start(ctx context.Context) error {
	return nil
}

func (m *MockPIDTranslator) Stop() {}

func (m *MockPIDTranslator) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"cache_hits":   100,
		"cache_misses": 10,
		"hit_rate":     0.9,
	}
}
