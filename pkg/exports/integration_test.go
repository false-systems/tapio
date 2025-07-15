package exports

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/exports/otel"
	"github.com/yairfalse/tapio/pkg/exports/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestOTELPrometheusIntegration tests the integration between OTEL and Prometheus exporters
func TestOTELPrometheusIntegration(t *testing.T) {
	// Setup OTEL exporter with in-memory recorder
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	otelConfig := &otel.ExporterConfig{
		ServiceName:    "integration-test",
		ServiceVersion: "1.0.0",
		OTLPEndpoint:   "http://localhost:4318/v1/traces", // Not actually used in test
		SamplingRate:   1.0,
	}

	otelExporter, err := otel.NewExporter(otelConfig)
	require.NoError(t, err)

	// Setup Prometheus exporter
	promConfig := &prometheus.ExporterConfig{
		ListenAddress: ":0", // Use random port for testing
		MetricsPath:   "/metrics",
	}

	promExporter, err := prometheus.NewExporter(promConfig)
	require.NoError(t, err)

	// Create test correlation result
	result := &correlation.Result{
		RuleID:      "integration_test_rule",
		RuleName:    "integration_memory_leak_detection",
		Timestamp:   time.Now(),
		Confidence:  0.9,
		Severity:    correlation.SeverityHigh,
		Category:    correlation.CategoryPerformance,
		Title:       "Integration test memory leak",
		Description: "Memory leak detected during integration test",
		Evidence: correlation.Evidence{
			Events: []correlation.Event{
				{
					ID:        "integration-event-001",
					Timestamp: time.Now().Add(-5 * time.Minute),
					Source:    correlation.SourceEBPF,
					Type:      "memory_allocation",
					Entity: correlation.Entity{
						Type:      "pod",
						Name:      "integration-test-pod",
						Namespace: "test-namespace",
						Node:      "test-node",
					},
					Attributes: map[string]interface{}{
						"memory_usage": 2048000,
						"growth_rate":  0.15,
					},
				},
			},
			Metrics: map[string]float64{
				"memory_leak_confidence": 0.9,
				"cpu_correlation":        0.7,
			},
			Timeline: []correlation.TimelineEntry{
				{
					Timestamp:   time.Now().Add(-10 * time.Minute),
					Description: "Memory usage started increasing",
					Source:      "ebpf",
				},
			},
		},
		Recommendations: []string{
			"Investigate memory allocation patterns",
			"Consider implementing memory limits",
		},
		Actions: []correlation.Action{
			{
				Type:     "alert",
				Target:   "monitoring-system",
				Priority: "high",
			},
		},
	}

	ctx := context.Background()

	// Export to both systems simultaneously
	start := time.Now()

	// Export to OTEL
	err = otelExporter.ExportCorrelationResult(ctx, result)
	assert.NoError(t, err)

	// Export to Prometheus
	err = promExporter.ExportCorrelationResult(ctx, result)
	assert.NoError(t, err)

	exportDuration := time.Since(start)

	// Verify export latency is within requirements (<20ms)
	assert.Less(t, exportDuration, 20*time.Millisecond, "Export should complete within 20ms")

	// Verify OTEL traces were created
	spans := spanRecorder.Ended()
	assert.Greater(t, len(spans), 0, "Should have created at least one span")

	// Find correlation span
	var correlationSpan *tracetest.SpanStub
	for _, span := range spans {
		if span.Name == "tapio.correlation.analysis" {
			correlationSpan = &span
			break
		}
	}

	require.NotNil(t, correlationSpan, "Should have created correlation span")
	assert.Equal(t, "integration_test_rule", getStringAttribute(correlationSpan.Attributes, "correlation.rule_id"))

	// Verify Prometheus metrics were recorded
	registry := promExporter.GetRegistry()
	metricFamilies, err := registry.Gather()
	require.NoError(t, err)

	foundCorrelationMetric := false
	for _, mf := range metricFamilies {
		if *mf.Name == "tapio_correlation_correlations_total" {
			foundCorrelationMetric = true
			assert.Equal(t, 1, len(mf.Metric))
			assert.Equal(t, float64(1), *mf.Metric[0].Counter.Value)
		}
	}

	assert.True(t, foundCorrelationMetric, "Should have recorded correlation metric")
}

// TestConcurrentExport tests concurrent export to both OTEL and Prometheus
func TestConcurrentExport(t *testing.T) {
	// Setup exporters
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	otelExporter, err := otel.NewExporter(&otel.ExporterConfig{
		ServiceName:  "concurrent-test",
		SamplingRate: 1.0,
	})
	require.NoError(t, err)

	promExporter, err := prometheus.NewExporter(&prometheus.ExporterConfig{
		ListenAddress: ":0",
	})
	require.NoError(t, err)

	const numConcurrentExports = 50
	const numResults = 5

	// Create test results
	results := make([]*correlation.Result, numResults)
	for i := 0; i < numResults; i++ {
		results[i] = createTestResult("concurrent-rule", correlation.SeverityMedium, i)
	}

	ctx := context.Background()
	start := time.Now()

	// Export concurrently
	done := make(chan error, numConcurrentExports*2) // *2 for both exporters

	for i := 0; i < numConcurrentExports; i++ {
		// OTEL export goroutine
		go func(resultIndex int) {
			result := results[resultIndex%numResults]
			err := otelExporter.ExportCorrelationResult(ctx, result)
			done <- err
		}(i)

		// Prometheus export goroutine
		go func(resultIndex int) {
			result := results[resultIndex%numResults]
			err := promExporter.ExportCorrelationResult(ctx, result)
			done <- err
		}(i)
	}

	// Wait for all exports to complete
	errors := 0
	for i := 0; i < numConcurrentExports*2; i++ {
		if err := <-done; err != nil {
			errors++
			t.Logf("Export error: %v", err)
		}
	}

	totalDuration := time.Since(start)

	// Verify results
	assert.Equal(t, 0, errors, "All exports should succeed")
	assert.Less(t, totalDuration, 5*time.Second, "Concurrent exports should complete quickly")

	// Verify some spans were created
	spans := spanRecorder.Ended()
	assert.Greater(t, len(spans), 0, "Should have created spans")

	// Verify some metrics were recorded
	registry := promExporter.GetRegistry()
	metricFamilies, err := registry.Gather()
	require.NoError(t, err)
	assert.Greater(t, len(metricFamilies), 0, "Should have recorded metrics")
}

// TestBatchVsSingleExport compares batch vs single export performance
func TestBatchVsSingleExport(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	otelExporter, err := otel.NewExporter(&otel.ExporterConfig{
		ServiceName:  "batch-test",
		SamplingRate: 1.0,
	})
	require.NoError(t, err)

	promExporter, err := prometheus.NewExporter(&prometheus.ExporterConfig{
		ListenAddress: ":0",
	})
	require.NoError(t, err)

	const numResults = 10
	results := make([]*correlation.Result, numResults)
	for i := 0; i < numResults; i++ {
		results[i] = createTestResult("batch-rule", correlation.SeverityLow, i)
	}

	ctx := context.Background()

	// Test single exports
	start := time.Now()
	for _, result := range results {
		err := otelExporter.ExportCorrelationResult(ctx, result)
		assert.NoError(t, err)

		err = promExporter.ExportCorrelationResult(ctx, result)
		assert.NoError(t, err)
	}
	singleExportDuration := time.Since(start)

	// Reset for batch test
	spanRecorder = tracetest.NewSpanRecorder()
	tracerProvider = trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	// Test batch export (OTEL supports this, Prometheus doesn't)
	start = time.Now()
	err = otelExporter.ExportCorrelationBatch(ctx, results)
	assert.NoError(t, err)

	// Prometheus batch is just individual exports
	err = promExporter.ExportCorrelationBatch(ctx, results)
	assert.NoError(t, err)

	batchExportDuration := time.Since(start)

	t.Logf("Single export duration: %v", singleExportDuration)
	t.Logf("Batch export duration: %v", batchExportDuration)

	// Batch should be faster or similar (not significantly slower)
	assert.LessOrEqual(t, batchExportDuration, singleExportDuration*2,
		"Batch export should not be significantly slower than individual exports")
}

// TestErrorHandling tests error scenarios
func TestErrorHandling(t *testing.T) {
	// Test with invalid configuration
	_, err := otel.NewExporter(&otel.ExporterConfig{
		ServiceName:  "", // Invalid empty service name
		OTLPEndpoint: "invalid-url",
	})
	// Note: The actual error depends on implementation - this tests error path exists
	assert.Error(t, err)

	// Test export with nil result
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	otelExporter, err := otel.NewExporter(&otel.ExporterConfig{
		ServiceName:  "error-test",
		SamplingRate: 1.0,
	})
	require.NoError(t, err)

	// Test with context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result := createTestResult("error-rule", correlation.SeverityMedium, 0)
	err = otelExporter.ExportCorrelationResult(ctx, result)
	// Should handle cancelled context gracefully
	// Specific error handling depends on implementation
}

// TestExportLatency specifically tests the <20ms requirement
func TestExportLatency(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	otelExporter, err := otel.NewExporter(&otel.ExporterConfig{
		ServiceName:  "latency-test",
		SamplingRate: 1.0,
	})
	require.NoError(t, err)

	promExporter, err := prometheus.NewExporter(&prometheus.ExporterConfig{
		ListenAddress: ":0",
	})
	require.NoError(t, err)

	result := createTestResult("latency-rule", correlation.SeverityMedium, 0)
	ctx := context.Background()

	// Test multiple exports to get average latency
	const numTests = 100
	var totalDuration time.Duration

	for i := 0; i < numTests; i++ {
		start := time.Now()

		err = otelExporter.ExportCorrelationResult(ctx, result)
		assert.NoError(t, err)

		err = promExporter.ExportCorrelationResult(ctx, result)
		assert.NoError(t, err)

		totalDuration += time.Since(start)
	}

	avgDuration := totalDuration / numTests
	t.Logf("Average export duration: %v", avgDuration)

	// Verify latency requirement
	assert.Less(t, avgDuration, 20*time.Millisecond,
		"Average export latency should be less than 20ms")
}

// Helper functions

func createTestResult(ruleID string, severity correlation.Severity, index int) *correlation.Result {
	return &correlation.Result{
		RuleID:      ruleID,
		RuleName:    ruleID + "_detection",
		Timestamp:   time.Now(),
		Confidence:  0.7 + float64(index%3)*0.1, // Vary confidence
		Severity:    severity,
		Category:    correlation.CategoryPerformance,
		Title:       "Test correlation result",
		Description: "Test description for correlation",
		Evidence: correlation.Evidence{
			Events: []correlation.Event{
				{
					ID:        "test-event-" + ruleID,
					Timestamp: time.Now().Add(-time.Duration(index) * time.Minute),
					Source:    correlation.SourceEBPF,
					Type:      "test_event",
					Entity: correlation.Entity{
						Type:      "pod",
						Name:      "test-pod-" + ruleID,
						Namespace: "test-namespace",
					},
				},
			},
		},
	}
}

func getStringAttribute(attrs map[string]interface{}, key string) string {
	if val, ok := attrs[key]; ok {
		if strVal, ok := val.(string); ok {
			return strVal
		}
	}
	return ""
}
