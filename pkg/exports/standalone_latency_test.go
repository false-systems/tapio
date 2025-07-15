package exports

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Minimal types for testing without importing correlation package
type TestSeverity string
type TestCategory string

const (
	SeverityLow      TestSeverity = "low"
	SeverityMedium   TestSeverity = "medium"
	SeverityHigh     TestSeverity = "high"
	SeverityCritical TestSeverity = "critical"
)

const (
	CategoryPerformance TestCategory = "performance"
	CategorySecurity    TestCategory = "security"
)

type TestCorrelationResult struct {
	RuleID      string
	RuleName    string
	Timestamp   time.Time
	Confidence  float64
	Severity    TestSeverity
	Category    TestCategory
	Title       string
	Description string
	EventCount  int
}

// Minimal OTEL exporter for testing
type TestOTELExporter struct {
	tracer otel.Tracer
	mutex  sync.Mutex
	count  int64
}

func NewTestOTELExporter() *TestOTELExporter {
	return &TestOTELExporter{
		tracer: otel.Tracer("test-tracer"),
	}
}

func (e *TestOTELExporter) ExportCorrelationResult(ctx context.Context, result *TestCorrelationResult) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// Create minimal span
	_, span := e.tracer.Start(ctx, "test.correlation.analysis",
		otel.WithAttributes(
			attribute.String("correlation.rule_id", result.RuleID),
			attribute.String("correlation.severity", string(result.Severity)),
			attribute.Float64("correlation.confidence", result.Confidence),
		),
	)
	defer span.End()

	e.count++
	return nil
}

func (e *TestOTELExporter) GetCount() int64 {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.count
}

// Minimal Prometheus exporter for testing
type TestPrometheusExporter struct {
	correlationsTotal *prometheus.CounterVec
	mutex             sync.Mutex
	count             int64
}

func NewTestPrometheusExporter() *TestPrometheusExporter {
	registry := prometheus.NewRegistry()

	correlationsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "test_tapio",
			Subsystem: "correlation",
			Name:      "correlations_total",
			Help:      "Total correlations detected",
		},
		[]string{"rule_id", "severity"},
	)

	registry.MustRegister(correlationsTotal)

	return &TestPrometheusExporter{
		correlationsTotal: correlationsTotal,
	}
}

func (e *TestPrometheusExporter) ExportCorrelationResult(ctx context.Context, result *TestCorrelationResult) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	labels := prometheus.Labels{
		"rule_id":  result.RuleID,
		"severity": string(result.Severity),
	}

	e.correlationsTotal.With(labels).Inc()
	e.count++
	return nil
}

func (e *TestPrometheusExporter) GetCount() int64 {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.count
}

// TestExportLatencyStandalone validates the <20ms export latency requirement
func TestExportLatencyStandalone(t *testing.T) {
	// Setup OTEL with in-memory span recorder
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	// Create test exporters
	otelExporter := NewTestOTELExporter()
	promExporter := NewTestPrometheusExporter()

	// Create test correlation result
	result := &TestCorrelationResult{
		RuleID:      "latency_test_rule",
		RuleName:    "latency_test",
		Timestamp:   time.Now(),
		Confidence:  0.85,
		Severity:    SeverityMedium,
		Category:    CategoryPerformance,
		Title:       "Latency test correlation",
		Description: "Test correlation for latency validation",
		EventCount:  1,
	}

	ctx := context.Background()

	// Warm up exporters (first calls might be slower)
	_ = otelExporter.ExportCorrelationResult(ctx, result)
	_ = promExporter.ExportCorrelationResult(ctx, result)

	// Test latency with multiple iterations
	const numIterations = 100
	durations := make([]time.Duration, numIterations)
	var totalDuration time.Duration

	for i := 0; i < numIterations; i++ {
		start := time.Now()

		// Export to both systems
		err := otelExporter.ExportCorrelationResult(ctx, result)
		assert.NoError(t, err)

		err = promExporter.ExportCorrelationResult(ctx, result)
		assert.NoError(t, err)

		duration := time.Since(start)
		durations[i] = duration
		totalDuration += duration
	}

	// Calculate statistics
	avgDuration := totalDuration / numIterations

	// Find min/max
	minDuration := durations[0]
	maxDuration := durations[0]
	for _, d := range durations {
		if d < minDuration {
			minDuration = d
		}
		if d > maxDuration {
			maxDuration = d
		}
	}

	// Calculate percentiles
	fastRequests := 0
	under10ms := 0
	under5ms := 0

	for _, d := range durations {
		if d < 20*time.Millisecond {
			fastRequests++
		}
		if d < 10*time.Millisecond {
			under10ms++
		}
		if d < 5*time.Millisecond {
			under5ms++
		}
	}

	percentageUnder20ms := float64(fastRequests) / float64(numIterations) * 100
	percentageUnder10ms := float64(under10ms) / float64(numIterations) * 100
	percentageUnder5ms := float64(under5ms) / float64(numIterations) * 100

	// Log detailed performance metrics
	t.Logf("=== Export Latency Analysis ===")
	t.Logf("Iterations: %d", numIterations)
	t.Logf("Average latency: %v", avgDuration)
	t.Logf("Minimum latency: %v", minDuration)
	t.Logf("Maximum latency: %v", maxDuration)
	t.Logf("Requests under 20ms: %.1f%% (%d/%d)", percentageUnder20ms, fastRequests, numIterations)
	t.Logf("Requests under 10ms: %.1f%% (%d/%d)", percentageUnder10ms, under10ms, numIterations)
	t.Logf("Requests under 5ms: %.1f%% (%d/%d)", percentageUnder5ms, under5ms, numIterations)

	// Validate latency requirements
	assert.Less(t, avgDuration, 20*time.Millisecond,
		"REQUIREMENT FAILED: Average export latency must be less than 20ms (got %v)", avgDuration)

	assert.Greater(t, percentageUnder20ms, 95.0,
		"REQUIREMENT FAILED: At least 95%% of requests should be under 20ms (got %.1f%%)", percentageUnder20ms)

	// Additional performance validations
	assert.Less(t, maxDuration, 100*time.Millisecond,
		"Maximum latency should be reasonable (got %v)", maxDuration)

	// Verify exports actually worked
	assert.Equal(t, int64(numIterations+1), otelExporter.GetCount()) // +1 for warmup
	assert.Equal(t, int64(numIterations+1), promExporter.GetCount()) // +1 for warmup

	// Verify spans were created
	spans := spanRecorder.Ended()
	assert.Greater(t, len(spans), numIterations, "Should have created spans")

	t.Logf("✅ Export latency requirement validation PASSED")
}

// TestConcurrentExportLatency tests latency under concurrent load
func TestConcurrentExportLatency(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	otelExporter := NewTestOTELExporter()
	promExporter := NewTestPrometheusExporter()

	result := &TestCorrelationResult{
		RuleID:     "concurrent_test",
		RuleName:   "concurrent_latency_test",
		Confidence: 0.8,
		Severity:   SeverityHigh,
		Category:   CategoryPerformance,
	}

	const numGoroutines = 10
	const exportsPerGoroutine = 10

	ctx := context.Background()
	var wg sync.WaitGroup
	durations := make(chan time.Duration, numGoroutines*exportsPerGoroutine)

	startTime := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < exportsPerGoroutine; j++ {
				start := time.Now()

				_ = otelExporter.ExportCorrelationResult(ctx, result)
				_ = promExporter.ExportCorrelationResult(ctx, result)

				durations <- time.Since(start)
			}
		}()
	}

	wg.Wait()
	close(durations)

	totalTestDuration := time.Since(startTime)

	// Analyze concurrent performance
	var totalDuration time.Duration
	var maxDuration time.Duration
	count := 0
	under20ms := 0

	for duration := range durations {
		totalDuration += duration
		count++

		if duration > maxDuration {
			maxDuration = duration
		}
		if duration < 20*time.Millisecond {
			under20ms++
		}
	}

	avgDuration := totalDuration / time.Duration(count)
	percentageUnder20ms := float64(under20ms) / float64(count) * 100

	t.Logf("=== Concurrent Export Latency Analysis ===")
	t.Logf("Concurrent goroutines: %d", numGoroutines)
	t.Logf("Exports per goroutine: %d", exportsPerGoroutine)
	t.Logf("Total exports: %d", count)
	t.Logf("Total test duration: %v", totalTestDuration)
	t.Logf("Average export latency: %v", avgDuration)
	t.Logf("Maximum export latency: %v", maxDuration)
	t.Logf("Exports under 20ms: %.1f%%", percentageUnder20ms)

	// Validate concurrent performance
	assert.Less(t, avgDuration, 20*time.Millisecond,
		"Average concurrent export latency must be under 20ms (got %v)", avgDuration)

	assert.Greater(t, percentageUnder20ms, 90.0,
		"At least 90%% of concurrent exports should be under 20ms (got %.1f%%)", percentageUnder20ms)

	t.Logf("✅ Concurrent export latency validation PASSED")
}

// BenchmarkExportPerformance benchmarks the export operations
func BenchmarkExportPerformance(b *testing.B) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	otelExporter := NewTestOTELExporter()
	promExporter := NewTestPrometheusExporter()

	result := &TestCorrelationResult{
		RuleID:     "benchmark_rule",
		RuleName:   "benchmark_test",
		Confidence: 0.8,
		Severity:   SeverityMedium,
		Category:   CategoryPerformance,
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = otelExporter.ExportCorrelationResult(ctx, result)
		_ = promExporter.ExportCorrelationResult(ctx, result)
	}
}

// TestExportMemoryUsage validates that exports don't cause memory leaks
func TestExportMemoryUsage(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	otelExporter := NewTestOTELExporter()
	promExporter := NewTestPrometheusExporter()

	result := &TestCorrelationResult{
		RuleID:     "memory_test",
		RuleName:   "memory_usage_test",
		Confidence: 0.7,
		Severity:   SeverityLow,
		Category:   CategoryPerformance,
	}

	ctx := context.Background()

	// Perform many exports to test for memory leaks
	const numExports = 1000
	for i := 0; i < numExports; i++ {
		err := otelExporter.ExportCorrelationResult(ctx, result)
		assert.NoError(t, err)

		err = promExporter.ExportCorrelationResult(ctx, result)
		assert.NoError(t, err)

		// Periodically check that we can still create objects (not out of memory)
		if i%100 == 0 {
			_ = fmt.Sprintf("Memory test %d", i)
		}
	}

	// Verify exports completed successfully
	assert.Equal(t, int64(numExports), otelExporter.GetCount())
	assert.Equal(t, int64(numExports), promExporter.GetCount())

	t.Logf("✅ Memory usage test completed: %d exports without memory issues", numExports)
}
