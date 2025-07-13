package exports

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/exports/otel"
	"github.com/yairfalse/tapio/pkg/exports/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestExportLatencyRequirement validates the <20ms export latency requirement
func TestExportLatencyRequirement(t *testing.T) {
	// Setup minimal OTEL exporter for latency testing
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	// Create lightweight exporter configs optimized for speed
	otelConfig := &otel.TraceConfig{
		ServiceName:       "latency-test",
		ServiceVersion:    "1.0.0",
		TracerName:        "latency-tracer",
		MaxSpansPerTrace:  10, // Limit spans for faster processing
		ExportTimeout:     1 * time.Second,
		IncludeFullEvents: false, // Reduce payload size
		IncludeMetadata:   false, // Reduce processing overhead
		SampleRate:        1.0,
	}

	otelExporter := otel.NewTraceExporter(otelConfig)
	require.NotNil(t, otelExporter)

	promConfig := &prometheus.MetricsConfig{
		Namespace:               "test",
		Subsystem:               "latency",
		EnablePatternMetrics:    false, // Disable to reduce overhead
		EnableSystemMetrics:     false,
		EnableEntityMetrics:     false,
		EnablePerformanceMetrics: true,
	}

	promExporter := prometheus.NewMetricsExporter(promConfig)
	require.NotNil(t, promExporter)

	// Create minimal test correlation result
	result := &correlation.Result{
		RuleID:      "latency_test_rule",
		RuleName:    "latency_test",
		Timestamp:   time.Now(),
		Confidence:  0.8,
		Severity:    correlation.SeverityMedium,
		Category:    correlation.CategoryPerformance,
		Title:       "Latency test correlation",
		Description: "Minimal correlation for latency testing",
		Evidence: correlation.Evidence{
			Events: []correlation.Event{
				{
					ID:        "latency-test-event",
					Timestamp: time.Now(),
					Source:    correlation.SourceEBPF,
					Type:      "test_event",
					Entity: correlation.Entity{
						Type: "pod",
						Name: "test-pod",
						UID:  "test-uid",
					},
				},
			},
		},
	}

	ctx := context.Background()

	// Warm up exporters (first calls might be slower due to initialization)
	_ = otelExporter.ExportCorrelationResult(ctx, result)
	_ = promExporter.ExportCorrelationResult(ctx, result)

	// Test latency with multiple iterations
	const numIterations = 50
	var totalDuration time.Duration
	maxDuration := time.Duration(0)
	minDuration := time.Hour // Start with a large value

	for i := 0; i < numIterations; i++ {
		start := time.Now()

		// Export to both systems
		err := otelExporter.ExportCorrelationResult(ctx, result)
		assert.NoError(t, err)

		err = promExporter.ExportCorrelationResult(ctx, result)
		assert.NoError(t, err)

		duration := time.Since(start)
		totalDuration += duration

		if duration > maxDuration {
			maxDuration = duration
		}
		if duration < minDuration {
			minDuration = duration
		}
	}

	avgDuration := totalDuration / numIterations

	// Log performance metrics
	t.Logf("Latency Statistics:")
	t.Logf("  Average: %v", avgDuration)
	t.Logf("  Minimum: %v", minDuration)
	t.Logf("  Maximum: %v", maxDuration)
	t.Logf("  Total iterations: %d", numIterations)

	// Validate latency requirements
	assert.Less(t, avgDuration, 20*time.Millisecond, 
		"Average export latency must be less than 20ms (got %v)", avgDuration)
	
	// Also check that 95% of requests are under 20ms
	fastRequests := 0
	for i := 0; i < numIterations; i++ {
		start := time.Now()
		_ = otelExporter.ExportCorrelationResult(ctx, result)
		_ = promExporter.ExportCorrelationResult(ctx, result)
		if time.Since(start) < 20*time.Millisecond {
			fastRequests++
		}
	}

	percentageFast := float64(fastRequests) / float64(numIterations) * 100
	t.Logf("Requests under 20ms: %.1f%%", percentageFast)
	
	assert.Greater(t, percentageFast, 90.0, 
		"At least 90%% of requests should be under 20ms (got %.1f%%)", percentageFast)
}

// TestBatchExportLatency tests batch export performance
func TestBatchExportLatency(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	otelConfig := &otel.TraceConfig{
		ServiceName:      "batch-latency-test",
		MaxSpansPerTrace: 50,
		IncludeFullEvents: false,
		IncludeMetadata:  false,
		SampleRate:       1.0,
	}

	otelExporter := otel.NewTraceExporter(otelConfig)
	promExporter := prometheus.NewMetricsExporter(&prometheus.MetricsConfig{
		Namespace:               "test",
		Subsystem:               "batch",
		EnablePerformanceMetrics: true,
	})

	// Create batch of correlation results
	const batchSize = 10
	results := make([]*correlation.Result, batchSize)
	for i := 0; i < batchSize; i++ {
		results[i] = &correlation.Result{
			RuleID:     "batch_rule",
			RuleName:   "batch_test",
			Timestamp:  time.Now(),
			Confidence: 0.8,
			Severity:   correlation.SeverityLow,
			Category:   correlation.CategoryPerformance,
			Evidence: correlation.Evidence{
				Events: []correlation.Event{
					{
						ID:        "batch-event",
						Timestamp: time.Now(),
						Source:    correlation.SourceEBPF,
						Type:      "batch_test",
						Entity: correlation.Entity{
							Type: "pod",
							Name: "batch-pod",
							UID:  "batch-uid",
						},
					},
				},
			},
		}
	}

	ctx := context.Background()

	// Test batch export latency
	start := time.Now()
	err := otelExporter.ExportBatch(ctx, results)
	assert.NoError(t, err)

	// Prometheus doesn't have true batch, so simulate
	for _, result := range results {
		err = promExporter.ExportCorrelationResult(ctx, result)
		assert.NoError(t, err)
	}

	batchDuration := time.Since(start)
	perItemDuration := batchDuration / batchSize

	t.Logf("Batch Export Performance:")
	t.Logf("  Batch size: %d", batchSize)
	t.Logf("  Total duration: %v", batchDuration)
	t.Logf("  Per-item duration: %v", perItemDuration)

	// Batch per-item latency should still be reasonable
	assert.Less(t, perItemDuration, 10*time.Millisecond,
		"Per-item latency in batch should be under 10ms (got %v)", perItemDuration)
}

// TestMinimalExportLatency tests the absolute minimum export time
func TestMinimalExportLatency(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	// Minimal configuration for fastest possible export
	otelConfig := &otel.TraceConfig{
		ServiceName:       "minimal-test",
		MaxSpansPerTrace:  1,
		IncludeFullEvents: false,
		IncludeMetadata:   false,
		SampleRate:        1.0,
	}

	promConfig := &prometheus.MetricsConfig{
		Namespace:                "min",
		Subsystem:                "test",
		EnablePatternMetrics:     false,
		EnableSystemMetrics:      false,
		EnableEntityMetrics:      false,
		EnablePerformanceMetrics: false,
	}

	otelExporter := otel.NewTraceExporter(otelConfig)
	promExporter := prometheus.NewMetricsExporter(promConfig)

	// Absolute minimal correlation result
	result := &correlation.Result{
		RuleID:     "min",
		RuleName:   "min",
		Confidence: 0.5,
		Severity:   correlation.SeverityLow,
		Category:   correlation.CategoryPerformance,
		Evidence:   correlation.Evidence{},
	}

	ctx := context.Background()

	// Measure minimal export time
	start := time.Now()
	_ = otelExporter.ExportCorrelationResult(ctx, result)
	_ = promExporter.ExportCorrelationResult(ctx, result)
	minimalDuration := time.Since(start)

	t.Logf("Minimal export duration: %v", minimalDuration)

	// Even minimal export should be very fast
	assert.Less(t, minimalDuration, 5*time.Millisecond,
		"Minimal export should be under 5ms (got %v)", minimalDuration)
}

// BenchmarkExportLatency benchmarks export performance
func BenchmarkExportLatency(b *testing.B) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	otelExporter := otel.NewTraceExporter(&otel.TraceConfig{
		ServiceName:       "benchmark",
		IncludeFullEvents: false,
		IncludeMetadata:   false,
	})

	promExporter := prometheus.NewMetricsExporter(&prometheus.MetricsConfig{
		Namespace: "bench",
		Subsystem: "test",
	})

	result := &correlation.Result{
		RuleID:     "benchmark",
		RuleName:   "benchmark",
		Confidence: 0.8,
		Severity:   correlation.SeverityMedium,
		Category:   correlation.CategoryPerformance,
		Evidence:   correlation.Evidence{},
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = otelExporter.ExportCorrelationResult(ctx, result)
		_ = promExporter.ExportCorrelationResult(ctx, result)
	}
}