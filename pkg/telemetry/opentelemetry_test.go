package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/falseyair/tapio/pkg/ebpf"
	"github.com/falseyair/tapio/pkg/resilience"
	"github.com/falseyair/tapio/pkg/types"
)

// Mock checker implementation for testing
type mockChecker struct {
	mu             sync.Mutex
	failNextN      int
	delayResponse  time.Duration
	returnError    bool
	problems       []types.Problem
	checkCallCount int
}

func (m *mockChecker) Check(ctx context.Context, req *types.CheckRequest) (*types.CheckResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.checkCallCount++

	if m.delayResponse > 0 {
		time.Sleep(m.delayResponse)
	}

	if m.returnError {
		return nil, fmt.Errorf("mock check error")
	}

	if m.failNextN > 0 {
		m.failNextN--
		return nil, fmt.Errorf("intentional failure %d", m.failNextN)
	}

	return &types.CheckResult{
		Problems: m.problems,
		Summary: types.Summary{
			TotalPods:    3,
			HealthyPods:  2,
			WarningPods:  1,
			CriticalPods: 0,
		},
	}, nil
}

func (m *mockChecker) setProblems(problems []types.Problem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.problems = problems
}

func (m *mockChecker) setFailNextN(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNextN = n
}

func (m *mockChecker) getCheckCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.checkCallCount
}

// Mock eBPF monitor for testing
type mockEBPFMonitor struct {
	available     bool
	memStats      []ebpf.ProcessMemoryStats
	returnError   bool
	predictions   map[uint32]*ebpf.OOMPrediction
}

func (m *mockEBPFMonitor) IsAvailable() bool {
	return m.available
}

func (m *mockEBPFMonitor) GetMemoryStats() ([]ebpf.ProcessMemoryStats, error) {
	if m.returnError {
		return nil, fmt.Errorf("mock eBPF error")
	}
	return m.memStats, nil
}

func (m *mockEBPFMonitor) GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*ebpf.OOMPrediction, error) {
	if m.returnError {
		return nil, fmt.Errorf("mock prediction error")
	}
	return m.predictions, nil
}

func TestNewOpenTelemetryExporter(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		expectError    bool
		validateConfig func(*testing.T, *OpenTelemetryExporter)
	}{
		{
			name: "default configuration",
			config: Config{
				OTLPEndpoint:  "http://localhost:4317",
				EnableTraces:  true,
				EnableMetrics: true,
			},
			expectError: false,
			validateConfig: func(t *testing.T, exporter *OpenTelemetryExporter) {
				if exporter.config.ServiceName != "tapio" {
					t.Errorf("Expected default service name 'tapio', got %s", exporter.config.ServiceName)
				}
				if exporter.config.BatchSize != 100 {
					t.Errorf("Expected default batch size 100, got %d", exporter.config.BatchSize)
				}
			},
		},
		{
			name: "custom configuration",
			config: Config{
				ServiceName:    "test-service",
				ServiceVersion: "2.0.0",
				OTLPEndpoint:   "http://localhost:4318",
				BatchSize:      50,
				EnableTraces:   true,
				EnableMetrics:  false,
			},
			expectError: false,
			validateConfig: func(t *testing.T, exporter *OpenTelemetryExporter) {
				if exporter.config.ServiceName != "test-service" {
					t.Errorf("Expected service name 'test-service', got %s", exporter.config.ServiceName)
				}
				if exporter.config.BatchSize != 50 {
					t.Errorf("Expected batch size 50, got %d", exporter.config.BatchSize)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &mockChecker{}
			monitor := &mockEBPFMonitor{available: false}

			exporter, err := NewOpenTelemetryExporter(checker, monitor, tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if exporter == nil {
				t.Fatal("Expected exporter to be created")
			}

			// Validate resilience components
			if exporter.circuitBreaker == nil {
				t.Error("Expected circuit breaker to be initialized")
			}
			if exporter.timeoutManager == nil {
				t.Error("Expected timeout manager to be initialized")
			}
			if exporter.validator == nil {
				t.Error("Expected validator to be initialized")
			}
			if exporter.healthChecker == nil {
				t.Error("Expected health checker to be initialized")
			}

			if tt.validateConfig != nil {
				tt.validateConfig(t, exporter)
			}
		})
	}
}

func TestCircuitBreakerIntegration(t *testing.T) {
	// Create mock OTLP server that fails initially
	failureCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failureCount++
		if failureCount <= 5 { // Fail first 5 requests to trigger circuit breaker
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := &mockChecker{}
	monitor := &mockEBPFMonitor{available: false}
	config := Config{
		OTLPEndpoint:  server.URL,
		EnableTraces:  true,
		EnableMetrics: false,
		BatchSize:     10,
		Insecure:      true,
	}

	exporter, err := NewOpenTelemetryExporter(checker, monitor, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Shutdown(context.Background())

	ctx := context.Background()

	// Create spans that should trigger circuit breaker
	spans := make([]trace.Span, 6)
	for i := 0; i < 6; i++ {
		span, err := exporter.CreateSpan(ctx, fmt.Sprintf("test.span.%d", i))
		if err != nil {
			t.Fatalf("Failed to create span %d: %v", i, err)
		}
		spans[i] = span
	}

	// Try to export spans - should trigger circuit breaker after failures
	err = exporter.ExportSpans(ctx, spans[:3])
	if err == nil {
		t.Error("Expected export to fail and trigger circuit breaker")
	}

	// Verify circuit breaker is open
	if exporter.circuitBreaker.GetState() != resilience.StateOpen {
		t.Errorf("Expected circuit breaker to be open, got %s", exporter.circuitBreaker.GetState())
	}

	// Wait for circuit breaker to transition to half-open
	time.Sleep(31 * time.Second) // Reset timeout is 30s

	// Try again - should succeed after server starts working
	err = exporter.ExportSpans(ctx, spans[3:])
	if err != nil {
		t.Errorf("Expected export to succeed after circuit breaker reset: %v", err)
	}

	// Verify circuit breaker is closed again
	if exporter.circuitBreaker.GetState() != resilience.StateClosed {
		t.Errorf("Expected circuit breaker to be closed after recovery, got %s", exporter.circuitBreaker.GetState())
	}
}

func TestTimeoutManagerIntegration(t *testing.T) {
	// Create slow mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Second) // Longer than timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := &mockChecker{}
	monitor := &mockEBPFMonitor{available: false}
	config := Config{
		OTLPEndpoint:  server.URL,
		EnableTraces:  true,
		EnableMetrics: false,
		BatchSize:     10,
		Insecure:      true,
	}

	exporter, err := NewOpenTelemetryExporter(checker, monitor, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Shutdown(context.Background())

	ctx := context.Background()

	// Create a span
	span, err := exporter.CreateSpan(ctx, "test.timeout.span")
	if err != nil {
		t.Fatalf("Failed to create span: %v", err)
	}

	// Export should timeout and trigger retries
	startTime := time.Now()
	err = exporter.ExportSpans(ctx, []trace.Span{span})
	duration := time.Since(startTime)

	// Should fail due to timeout
	if err == nil {
		t.Error("Expected export to fail due to timeout")
	}

	// Should have retried multiple times (exponential backoff)
	// With 3 retries, should take at least ~7 seconds (100ms + 200ms + 400ms + base timeouts)
	if duration < 7*time.Second {
		t.Errorf("Expected operation to take at least 7 seconds with retries, took %v", duration)
	}

	// Verify timeout manager metrics
	metrics := exporter.timeoutManager.GetMetrics()
	if metrics.TotalTimeouts == 0 {
		t.Error("Expected timeout manager to record timeouts")
	}
	if metrics.TotalRetries == 0 {
		t.Error("Expected timeout manager to record retries")
	}
}

func TestSpanValidation(t *testing.T) {
	checker := &mockChecker{}
	monitor := &mockEBPFMonitor{available: false}
	config := Config{
		OTLPEndpoint:  "http://localhost:4317",
		EnableTraces:  true,
		EnableMetrics: false,
	}

	exporter, err := NewOpenTelemetryExporter(checker, monitor, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Shutdown(context.Background())

	ctx := context.Background()

	tests := []struct {
		name        string
		spanName    string
		expectError bool
	}{
		{
			name:        "valid span name",
			spanName:    "tapio.test_operation",
			expectError: false,
		},
		{
			name:        "invalid span name - no tapio prefix",
			spanName:    "invalid.operation",
			expectError: true,
		},
		{
			name:        "invalid span name - wrong format",
			spanName:    "tapio.Invalid-Name",
			expectError: true,
		},
		{
			name:        "valid span name with dots",
			spanName:    "tapio.analysis.memory_check",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := exporter.CreateSpan(ctx, tt.spanName)

			if tt.expectError && err == nil {
				t.Error("Expected span creation to fail validation")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected span creation to succeed, got error: %v", err)
			}
		})
	}
}

func TestHealthCheckIntegration(t *testing.T) {
	// Create a working OTLP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := &mockChecker{}
	monitor := &mockEBPFMonitor{available: false}
	config := Config{
		OTLPEndpoint:  server.URL,
		EnableTraces:  true,
		EnableMetrics: false,
		Insecure:      true,
	}

	exporter, err := NewOpenTelemetryExporter(checker, monitor, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Shutdown(context.Background())

	ctx := context.Background()

	// Test OTLP endpoint health check
	err = exporter.pingOTLPEndpoint(ctx)
	if err != nil {
		t.Errorf("Expected OTLP endpoint to be healthy: %v", err)
	}

	// Test overall health status
	status := exporter.healthChecker.GetStatus(ctx)
	if status != resilience.HealthStatusHealthy {
		t.Errorf("Expected health status to be healthy, got %s", status)
	}

	// Stop server to simulate unhealthy endpoint
	server.Close()

	// Health check should now fail
	err = exporter.pingOTLPEndpoint(ctx)
	if err == nil {
		t.Error("Expected OTLP endpoint health check to fail after server shutdown")
	}
}

func TestUpdateTelemetryWithProblems(t *testing.T) {
	checker := &mockChecker{}
	monitor := &mockEBPFMonitor{available: false}
	config := Config{
		OTLPEndpoint:  "http://localhost:4317",
		EnableTraces:  true,
		EnableMetrics: false,
	}

	exporter, err := NewOpenTelemetryExporter(checker, monitor, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Shutdown(context.Background())

	// Set up mock problems
	problems := []types.Problem{
		{
			Type:     types.ProblemTypeMemoryLeak,
			Severity: types.SeverityCritical,
			Resource: types.Resource{
				Name:      "test-pod",
				Namespace: "default",
				Kind:      "Pod",
			},
			Prediction: &types.Prediction{
				Confidence:    0.95,
				TimeToFailure: 30 * time.Minute,
			},
		},
		{
			Type:     types.ProblemTypeResourceExhaustion,
			Severity: types.SeverityWarning,
			Resource: types.Resource{
				Name:      "worker-pod",
				Namespace: "kube-system",
				Kind:      "Pod",
			},
		},
	}
	checker.setProblems(problems)

	ctx := context.Background()

	// Update telemetry
	err = exporter.UpdateTelemetry(ctx)
	if err != nil {
		t.Errorf("Failed to update telemetry: %v", err)
	}

	// Verify metrics were updated
	metrics := exporter.GetMetrics()
	if metrics.TotalSpansCreated == 0 {
		t.Error("Expected spans to be created for problems")
	}

	// Verify last update time was set
	if metrics.LastUpdateTime.IsZero() {
		t.Error("Expected last update time to be set")
	}
}

func TestEBPFIntegration(t *testing.T) {
	checker := &mockChecker{}
	
	// Create mock eBPF monitor with sample data
	monitor := &mockEBPFMonitor{
		available: true,
		memStats: []ebpf.ProcessMemoryStats{
			{
				PID:          1234,
				ContainerID:  "test-container",
				CurrentUsage: 100 * 1024 * 1024, // 100MB
				PeakUsage:    120 * 1024 * 1024, // 120MB
				GrowthPattern: []ebpf.MemoryDataPoint{
					{Timestamp: time.Now().Add(-5 * time.Minute), Usage: 80 * 1024 * 1024},
					{Timestamp: time.Now(), Usage: 100 * 1024 * 1024},
				},
			},
		},
		predictions: map[uint32]*ebpf.OOMPrediction{
			1234: {
				PID:          1234,
				TimeToOOM:    15 * time.Minute,
				Confidence:   0.85,
				CurrentUsage: 100 * 1024 * 1024,
				MemoryLimit:  128 * 1024 * 1024,
			},
		},
	}

	config := Config{
		OTLPEndpoint:  "http://localhost:4317",
		EnableTraces:  true,
		EnableMetrics: false,
	}

	exporter, err := NewOpenTelemetryExporter(checker, monitor, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Shutdown(context.Background())

	ctx := context.Background()

	// Update telemetry with eBPF data
	err = exporter.UpdateTelemetry(ctx)
	if err != nil {
		t.Errorf("Failed to update telemetry with eBPF data: %v", err)
	}

	// Verify spans were created for eBPF data
	metrics := exporter.GetMetrics()
	if metrics.TotalSpansCreated == 0 {
		t.Error("Expected spans to be created for eBPF data")
	}
}

func TestConcurrentSpanCreation(t *testing.T) {
	checker := &mockChecker{}
	monitor := &mockEBPFMonitor{available: false}
	config := Config{
		OTLPEndpoint:   "http://localhost:4317",
		EnableTraces:   true,
		EnableMetrics:  false,
		MaxConcurrency: 5,
	}

	exporter, err := NewOpenTelemetryExporter(checker, monitor, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Shutdown(context.Background())

	ctx := context.Background()
	const numGoroutines = 20
	const spansPerGoroutine = 10

	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64
	var mu sync.Mutex

	// Create spans concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			for j := 0; j < spansPerGoroutine; j++ {
				spanName := fmt.Sprintf("tapio.concurrent.worker_%d_span_%d", workerID, j)
				_, err := exporter.CreateSpan(ctx, spanName)
				
				mu.Lock()
				if err != nil {
					errorCount++
				} else {
					successCount++
				}
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	mu.Lock()
	totalExpected := int64(numGoroutines * spansPerGoroutine)
	totalActual := successCount + errorCount
	mu.Unlock()

	if totalActual != totalExpected {
		t.Errorf("Expected %d total operations, got %d", totalExpected, totalActual)
	}

	// Some spans should succeed (bounded executor limits concurrency but doesn't reject)
	if successCount == 0 {
		t.Error("Expected some spans to be created successfully")
	}

	t.Logf("Concurrent test results: %d success, %d errors out of %d total", 
		successCount, errorCount, totalExpected)
}

func TestResourceLimits(t *testing.T) {
	checker := &mockChecker{}
	monitor := &mockEBPFMonitor{available: false}
	config := Config{
		OTLPEndpoint:  "http://localhost:4317",
		EnableTraces:  true,
		EnableMetrics: false,
		BatchSize:     1000, // Large batch size
	}

	exporter, err := NewOpenTelemetryExporter(checker, monitor, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Shutdown(context.Background())

	// Verify resource limits are applied
	if exporter.resourceLimits.MaxMemoryMB != 10 {
		t.Errorf("Expected max memory 10MB, got %d", exporter.resourceLimits.MaxMemoryMB)
	}
	if exporter.resourceLimits.MaxCPUPercent != 5.0 {
		t.Errorf("Expected max CPU 5%%, got %f", exporter.resourceLimits.MaxCPUPercent)
	}
	if exporter.resourceLimits.MaxBatchSize != 19 {
		t.Errorf("Expected max batch size 19 (19Hz), got %d", exporter.resourceLimits.MaxBatchSize)
	}

	// Test that large batches are limited
	ctx := context.Background()
	largeSpanSet := make([]trace.Span, 50) // Larger than limit
	for i := range largeSpanSet {
		span, err := exporter.CreateSpan(ctx, fmt.Sprintf("tapio.test_%d", i))
		if err != nil {
			t.Fatalf("Failed to create span %d: %v", i, err)
		}
		largeSpanSet[i] = span
	}

	// Export should respect batch size limits
	err = exporter.ExportSpans(ctx, largeSpanSet)
	
	// Should not error, but should respect limits internally
	if err != nil {
		t.Errorf("Export should handle large batches gracefully: %v", err)
	}
}

func TestExporterShutdown(t *testing.T) {
	checker := &mockChecker{}
	monitor := &mockEBPFMonitor{available: false}
	config := Config{
		OTLPEndpoint:  "http://localhost:4317",
		EnableTraces:  true,
		EnableMetrics: false,
	}

	exporter, err := NewOpenTelemetryExporter(checker, monitor, config)
	if err != nil {
		t.Fatalf("Failed to create exporter: %v", err)
	}

	ctx := context.Background()

	// Create some active spans
	span1, _ := exporter.CreateSpan(ctx, "tapio.test_span_1")
	span2, _ := exporter.CreateSpan(ctx, "tapio.test_span_2")

	// Verify spans are active
	metrics := exporter.GetMetrics()
	if metrics.ActiveSpansCount != 2 {
		t.Errorf("Expected 2 active spans, got %d", metrics.ActiveSpansCount)
	}

	// Shutdown exporter
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = exporter.Shutdown(shutdownCtx)
	if err != nil {
		t.Errorf("Failed to shutdown exporter: %v", err)
	}

	// Verify spans were ended
	if !span1.SpanContext().TraceID().IsValid() && !span2.SpanContext().TraceID().IsValid() {
		// Spans should still be valid even after shutdown, but should be ended
		// This is a bit hard to test without more sophisticated mocking
	}

	// Verify active spans were cleared
	metricsAfter := exporter.GetMetrics()
	if metricsAfter.ActiveSpansCount != 0 {
		t.Errorf("Expected 0 active spans after shutdown, got %d", metricsAfter.ActiveSpansCount)
	}
}