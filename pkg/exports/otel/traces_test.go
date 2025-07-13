package otel

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTraceExporter_ExportCorrelationResult(t *testing.T) {
	// Setup in-memory span recorder for testing
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	config := &TraceConfig{
		ServiceName:       "test-service",
		ServiceVersion:    "1.0.0",
		TracerName:        "test-tracer",
		MaxSpansPerTrace:  10,
		ExportTimeout:     5 * time.Second,
		IncludeFullEvents: true,
		IncludeMetadata:   true,
		SampleRate:        1.0,
	}

	exporter := NewTraceExporter(config)
	require.NotNil(t, exporter)

	// Create test correlation result
	result := &correlation.Result{
		RuleID:      "test-rule-001",
		RuleName:    "test_memory_leak_detection",
		Timestamp:   time.Now(),
		Confidence:  0.85,
		Severity:    correlation.SeverityHigh,
		Category:    correlation.CategoryPerformance,
		Title:       "Memory leak detected in test application",
		Description: "Test description for correlation result",
		Impact:      "Test impact analysis",
		Evidence: correlation.Evidence{
			Events: []correlation.Event{
				{
					ID:        "event-001",
					Timestamp: time.Now().Add(-5 * time.Minute),
					Source:    correlation.SourceEBPF,
					Type:      "memory_allocation",
					Entity: correlation.Entity{
						Type:      "pod",
						Name:      "test-pod-123",
						Namespace: "test-namespace",
						Node:      "test-node-1",
						UID:       "test-uid-123",
					},
					Attributes: map[string]interface{}{
						"memory_usage": 1024000,
						"process_id":   12345,
						"container":    "test-container",
					},
					Labels: map[string]string{
						"app":     "test-app",
						"version": "1.0.0",
					},
				},
			},
			Metrics: map[string]float64{
				"memory_growth_rate": 0.15,
				"cpu_usage":          0.75,
			},
			Timeline: []correlation.TimelineEntry{
				{
					Timestamp:   time.Now().Add(-10 * time.Minute),
					Description: "Memory usage started increasing",
					Source:      "ebpf",
				},
				{
					Timestamp:   time.Now().Add(-5 * time.Minute),
					Description: "Memory leak pattern detected",
					Source:      "correlation-engine",
				},
			},
		},
		Recommendations: []string{
			"Investigate memory allocation patterns",
			"Consider restarting the affected pod",
		},
		Actions: []correlation.Action{
			{
				Type:     "restart",
				Target:   "pod/test-pod-123",
				Priority: "high",
				Parameters: map[string]string{
					"namespace": "test-namespace",
					"reason":    "memory_leak_mitigation",
				},
			},
		},
	}

	ctx := context.Background()

	// Test export
	err := exporter.ExportCorrelationResult(ctx, result)
	assert.NoError(t, err)

	// Verify spans were created
	spans := spanRecorder.Ended()
	assert.Greater(t, len(spans), 0, "Expected at least one span to be created")

	// Find root correlation span
	var rootSpan *tracetest.SpanStub
	for _, span := range spans {
		if span.Name == "tapio.correlation.analysis" {
			rootSpan = &span
			break
		}
	}

	require.NotNil(t, rootSpan, "Root correlation span should be created")

	// Verify root span attributes
	attrs := rootSpan.Attributes
	assert.Equal(t, "test-rule-001", getStringAttribute(attrs, "correlation.rule_id"))
	assert.Equal(t, "test_memory_leak_detection", getStringAttribute(attrs, "correlation.rule_name"))
	assert.Equal(t, "high", getStringAttribute(attrs, "correlation.severity"))
	assert.Equal(t, "performance", getStringAttribute(attrs, "correlation.category"))
	assert.Equal(t, 0.85, getFloat64Attribute(attrs, "correlation.confidence"))

	// Verify evidence spans were created
	evidenceSpanCount := 0
	timelineSpanCount := 0
	for _, span := range spans {
		if span.Name == "tapio.correlation.evidence.event" {
			evidenceSpanCount++
		}
		if span.Name == "tapio.correlation.evidence.timeline" {
			timelineSpanCount++
		}
	}

	assert.Equal(t, 1, evidenceSpanCount, "Expected one evidence event span")
	assert.Equal(t, 2, timelineSpanCount, "Expected two timeline spans")
}

func TestTraceExporter_ExportBatch(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	config := DefaultTraceConfig()
	exporter := NewTraceExporter(config)

	// Create multiple test results
	results := []*correlation.Result{
		createTestResult("rule-001", "critical"),
		createTestResult("rule-002", "high"),
		createTestResult("rule-003", "medium"),
	}

	ctx := context.Background()
	err := exporter.ExportBatch(ctx, results)
	assert.NoError(t, err)

	// Verify batch span was created
	spans := spanRecorder.Ended()
	batchSpanFound := false
	correlationSpanCount := 0

	for _, span := range spans {
		if span.Name == "tapio.correlation.batch" {
			batchSpanFound = true
			// Verify batch attributes
			attrs := span.Attributes
			assert.Equal(t, int64(3), getInt64Attribute(attrs, "batch.size"))
		}
		if span.Name == "tapio.correlation.analysis" {
			correlationSpanCount++
		}
	}

	assert.True(t, batchSpanFound, "Batch span should be created")
	assert.Equal(t, 3, correlationSpanCount, "Expected three correlation spans")
}

func TestTraceExporter_ShouldSample(t *testing.T) {
	config := &TraceConfig{
		SampleRate: 0.5,
	}
	exporter := NewTraceExporter(config)

	tests := []struct {
		name     string
		severity correlation.Severity
		expected bool
	}{
		{
			name:     "critical severity always sampled",
			severity: correlation.SeverityCritical,
			expected: true,
		},
		{
			name:     "high severity always sampled",
			severity: correlation.SeverityHigh,
			expected: true,
		},
		{
			name:     "medium severity follows sampling rate",
			severity: correlation.SeverityMedium,
			expected: false, // Will vary based on random sampling
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &correlation.Result{
				Severity: tt.severity,
			}

			shouldSample := exporter.ShouldSample(result)

			if tt.severity == correlation.SeverityCritical || tt.severity == correlation.SeverityHigh {
				assert.True(t, shouldSample, "Critical and high severity should always be sampled")
			}
			// For medium/low severity, we can't predict the random sampling result
		})
	}
}

func TestTraceExporter_MaxSpansPerTrace(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	config := &TraceConfig{
		MaxSpansPerTrace: 2, // Limit to 2 spans for testing
		IncludeFullEvents: true,
	}
	exporter := NewTraceExporter(config)

	// Create result with many events (more than MaxSpansPerTrace)
	result := &correlation.Result{
		RuleID:   "test-rule",
		RuleName: "test-rule-name",
		Severity: correlation.SeverityMedium,
		Category: correlation.CategoryPerformance,
		Evidence: correlation.Evidence{
			Events: []correlation.Event{
				createTestEvent("event-001"),
				createTestEvent("event-002"),
				createTestEvent("event-003"), // This should be truncated
				createTestEvent("event-004"), // This should be truncated
			},
		},
	}

	ctx := context.Background()
	err := exporter.ExportCorrelationResult(ctx, result)
	assert.NoError(t, err)

	// Count evidence event spans
	spans := spanRecorder.Ended()
	evidenceSpanCount := 0
	for _, span := range spans {
		if span.Name == "tapio.correlation.evidence.event" {
			evidenceSpanCount++
		}
	}

	assert.Equal(t, 2, evidenceSpanCount, "Should respect MaxSpansPerTrace limit")
}

func TestDefaultTraceConfig(t *testing.T) {
	config := DefaultTraceConfig()

	assert.Equal(t, "tapio-correlation", config.ServiceName)
	assert.Equal(t, "1.0.0", config.ServiceVersion)
	assert.Equal(t, "tapio-exports", config.TracerName)
	assert.Equal(t, 100, config.MaxSpansPerTrace)
	assert.Equal(t, 5*time.Second, config.ExportTimeout)
	assert.True(t, config.IncludeFullEvents)
	assert.True(t, config.IncludeMetadata)
	assert.Equal(t, 1.0, config.SampleRate)
}

// Benchmark tests
func BenchmarkTraceExporter_ExportCorrelationResult(b *testing.B) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	config := DefaultTraceConfig()
	exporter := NewTraceExporter(config)

	result := createTestResult("benchmark-rule", "medium")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := exporter.ExportCorrelationResult(ctx, result)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTraceExporter_ExportBatch(b *testing.B) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	config := DefaultTraceConfig()
	exporter := NewTraceExporter(config)

	results := make([]*correlation.Result, 10)
	for i := 0; i < 10; i++ {
		results[i] = createTestResult("batch-rule", "medium")
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := exporter.ExportBatch(ctx, results)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Helper functions

func createTestResult(ruleID, severity string) *correlation.Result {
	return &correlation.Result{
		RuleID:      ruleID,
		RuleName:    ruleID + "_detection",
		Timestamp:   time.Now(),
		Confidence:  0.8,
		Severity:    correlation.Severity(severity),
		Category:    correlation.CategoryPerformance,
		Title:       "Test correlation result",
		Description: "Test description",
		Evidence: correlation.Evidence{
			Events: []correlation.Event{
				createTestEvent("test-event"),
			},
			Metrics: map[string]float64{
				"test_metric": 123.45,
			},
		},
	}
}

func createTestEvent(eventID string) correlation.Event {
	return correlation.Event{
		ID:        eventID,
		Timestamp: time.Now(),
		Source:    correlation.SourceEBPF,
		Type:      "test_event",
		Entity: correlation.Entity{
			Type:      "pod",
			Name:      "test-pod",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
		Attributes: map[string]interface{}{
			"test_attr": "test_value",
		},
		Labels: map[string]string{
			"test_label": "test_value",
		},
	}
}

// Attribute helper functions
func getStringAttribute(attrs map[string]interface{}, key string) string {
	if val, ok := attrs[key]; ok {
		if strVal, ok := val.(string); ok {
			return strVal
		}
	}
	return ""
}

func getFloat64Attribute(attrs map[string]interface{}, key string) float64 {
	if val, ok := attrs[key]; ok {
		if floatVal, ok := val.(float64); ok {
			return floatVal
		}
	}
	return 0
}

func getInt64Attribute(attrs map[string]interface{}, key string) int64 {
	if val, ok := attrs[key]; ok {
		if intVal, ok := val.(int64); ok {
			return intVal
		}
	}
	return 0
}