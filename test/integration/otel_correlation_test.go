package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/simple"
	"github.com/yairfalse/tapio/pkg/telemetry"
)

// TestCorrelationTracingIntegration tests the enhanced OTEL correlation tracing
func TestCorrelationTracingIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create in-memory span exporter for testing
	spanRecorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))

	// Create test checker
	checker := &simple.MockChecker{}

	// Create OTEL exporter config
	config := telemetry.Config{
		ServiceName:    "tapio-test",
		ServiceVersion: "1.0.0",
		EnableTraces:   true,
		EnableMetrics:  false, // Disable metrics for testing
	}

	// Create exporter
	exporter, err := telemetry.NewOpenTelemetryExporter(checker, nil, config)
	require.NoError(t, err)

	// Override tracer provider with test provider
	exporter.SetTracerProvider(tp)

	ctx := context.Background()

	t.Run("correlation_analysis_tracing", func(t *testing.T) {
		// Create test events
		events := []correlation.Event{
			{
				Source:      correlation.SourceKubernetes,
				Type:        "pod_crash_loop",
				Timestamp:   time.Now(),
				Confidence:  0.9,
				Description: "Pod crashing repeatedly",
				PID:         1234,
			},
			{
				Source:      correlation.SourceEBPF,
				Type:        "memory_pressure",
				Timestamp:   time.Now().Add(-1 * time.Minute),
				Confidence:  0.85,
				Description: "High memory usage detected",
				PID:         1234,
			},
			{
				Source:      correlation.SourceSystemd,
				Type:        "service_failure",
				Timestamp:   time.Now().Add(-30 * time.Second),
				Confidence:  0.8,
				Description: "Systemd service failed",
				PID:         1235,
			},
		}

		// Test correlation analysis tracing
		correlationID := "test-correlation-001"
		ctx, rootSpan := exporter.GetCorrelationTracer().TraceCorrelationAnalysis(ctx, correlationID, events)
		defer rootSpan.End()

		// Verify span was created
		assert.NotNil(t, rootSpan)

		// Test multi-layer correlation
		layers := []telemetry.LayerAnalysis{
			{
				Name:         "kubernetes",
				Target:       "test-pod",
				AnalysisType: "pod_health",
				Findings: []correlation.Finding{
					{Type: "pod_crash_loop", Confidence: 0.9, Description: "Pod crashing"},
				},
				Duration:   100 * time.Millisecond,
				DataPoints: 10,
				Accuracy:   0.9,
			},
			{
				Name:         "ebpf",
				Target:       "process-1234",
				AnalysisType: "memory_analysis",
				Findings: []correlation.Finding{
					{Type: "memory_pressure", Confidence: 0.85, Description: "High memory"},
				},
				Duration:   50 * time.Millisecond,
				DataPoints: 20,
				Accuracy:   0.85,
			},
		}

		_, layerSpan := exporter.GetCorrelationTracer().TraceMultiLayerCorrelation(ctx, correlationID, layers)
		layerSpan.End()

		// Give spans time to be recorded
		time.Sleep(100 * time.Millisecond)

		// Verify spans were recorded
		spans := spanRecorder.Ended()
		assert.GreaterOrEqual(t, len(spans), 2, "Should have at least 2 spans")

		// Verify span names
		spanNames := make(map[string]bool)
		for _, span := range spans {
			spanNames[span.Name()] = true
		}
		assert.True(t, spanNames["tapio.correlation.analysis"])
		assert.True(t, spanNames["tapio.correlation.multi_layer"])
	})

	t.Run("timeline_visualization_tracing", func(t *testing.T) {
		spanRecorder.Reset()

		events := []correlation.Event{
			{
				Source:      correlation.SourceKubernetes,
				Type:        "deployment_update",
				Timestamp:   time.Now().Add(-5 * time.Minute),
				Confidence:  0.95,
				Description: "Deployment updated",
			},
			{
				Source:      correlation.SourceKubernetes,
				Type:        "pod_restart",
				Timestamp:   time.Now().Add(-3 * time.Minute),
				Confidence:  0.8,
				Description: "Pod restarted",
			},
			{
				Source:      correlation.SourceNetwork,
				Type:        "connection_timeout",
				Timestamp:   time.Now().Add(-1 * time.Minute),
				Confidence:  0.7,
				Description: "Network timeout",
			},
		}

		// Test timeline visualization
		_, timelineSpan := exporter.GetCorrelationTracer().TraceTimelineVisualization(
			ctx, "timeline-001", events, 10*time.Minute)
		timelineSpan.End()

		// Test heatmap visualization
		_, heatmapSpan := exporter.GetCorrelationTracer().TraceTimelineHeatmap(
			ctx, events, 1*time.Minute)
		heatmapSpan.End()

		// Test event flow
		_, flowSpan := exporter.GetCorrelationTracer().TraceEventFlow(
			ctx, events, "sequential")
		flowSpan.End()

		time.Sleep(100 * time.Millisecond)

		spans := spanRecorder.Ended()
		assert.GreaterOrEqual(t, len(spans), 3, "Should have at least 3 timeline spans")

		// Verify timeline span names
		spanNames := make(map[string]bool)
		for _, span := range spans {
			spanNames[span.Name()] = true
		}
		assert.True(t, spanNames["tapio.timeline.visualization"])
		assert.True(t, spanNames["tapio.timeline.heatmap"])
		assert.True(t, spanNames["tapio.timeline.event_flow"])
	})

	t.Run("root_cause_analysis_tracing", func(t *testing.T) {
		spanRecorder.Reset()

		findings := []correlation.Finding{
			{
				Type:         "memory_pressure",
				Confidence:   0.9,
				Description:  "High memory usage",
				ResourceName: "test-pod",
			},
			{
				Type:         "memory_pressure",
				Confidence:   0.85,
				Description:  "Memory limit reached",
				ResourceName: "test-pod",
			},
			{
				Type:         "oom_kill",
				Confidence:   0.95,
				Description:  "Process killed by OOM",
				ResourceName: "test-pod",
			},
		}

		events := []correlation.Event{
			{
				Source:     correlation.SourceEBPF,
				Type:       "memory_spike",
				Timestamp:  time.Now().Add(-2 * time.Minute),
				Confidence: 0.9,
			},
		}

		// Test root cause chain analysis
		_, rootCauseSpan := exporter.GetCorrelationTracer().TraceRootCauseChain(
			ctx, findings, events)
		rootCauseSpan.End()

		// Test root cause propagation
		rootCause := telemetry.RootCauseCandidate{
			Type:         "memory_pressure",
			Confidence:   0.9,
			ImpactRadius: 3,
			Severity:     "high",
		}
		systemState := map[string]interface{}{
			"pod_count":   10,
			"node_memory": "80%",
		}

		_, propagationSpan := exporter.GetCorrelationTracer().TraceRootCausePropagation(
			ctx, rootCause, systemState)
		propagationSpan.End()

		time.Sleep(100 * time.Millisecond)

		spans := spanRecorder.Ended()
		assert.GreaterOrEqual(t, len(spans), 2, "Should have at least 2 root cause spans")

		// Verify root cause span names
		spanNames := make(map[string]bool)
		for _, span := range spans {
			spanNames[span.Name()] = true
		}
		assert.True(t, spanNames["tapio.rootcause.chain_analysis"])
		assert.True(t, spanNames["tapio.rootcause.propagation"])
	})
}

// TestOTELExporterHelperMethods tests the helper methods for correlation analysis
func TestOTELExporterHelperMethods(t *testing.T) {
	// Create test checker
	checker := &simple.MockChecker{}

	config := telemetry.Config{
		ServiceName:   "tapio-test",
		EnableTraces:  false, // Don't need actual tracing for helper tests
		EnableMetrics: false,
	}

	exporter, err := telemetry.NewOpenTelemetryExporter(checker, nil, config)
	require.NoError(t, err)

	t.Run("convertFindingsToEvents", func(t *testing.T) {
		findings := []correlation.Finding{
			{Type: "ebpf_memory_leak", Confidence: 0.8, Description: "Memory leak detected"},
			{Type: "systemd_service_failure", Confidence: 0.9, Description: "Service failed"},
			{Type: "network_timeout", Confidence: 0.7, Description: "Network issue"},
		}

		events := exporter.ConvertFindingsToEvents(findings)
		assert.Len(t, events, 3)
		assert.Equal(t, correlation.SourceEBPF, events[0].Source)
		assert.Equal(t, correlation.SourceSystemd, events[1].Source)
		assert.Equal(t, correlation.SourceNetwork, events[2].Source)
	})

	t.Run("identifyCausalRelationships", func(t *testing.T) {
		events := []correlation.Event{
			{
				Source:     correlation.SourceKubernetes,
				Type:       "high_memory_usage",
				Timestamp:  time.Now().Add(-2 * time.Minute),
				Confidence: 0.9,
			},
			{
				Source:     correlation.SourceKubernetes,
				Type:       "memory_limit_reached",
				Timestamp:  time.Now().Add(-1 * time.Minute),
				Confidence: 0.95,
			},
			{
				Source:     correlation.SourceKubernetes,
				Type:       "pod_eviction",
				Timestamp:  time.Now(),
				Confidence: 0.99,
			},
		}

		relationships := exporter.IdentifyCausalRelationships(events)
		assert.Greater(t, len(relationships), 0)

		// Verify causal chain
		if len(relationships) > 0 {
			assert.Equal(t, "triggers", relationships[0].RelationType)
			assert.Greater(t, relationships[0].Confidence, 0.5)
		}
	})

	t.Run("calculateAverageConfidence", func(t *testing.T) {
		findings := []correlation.Finding{
			{Confidence: 0.8},
			{Confidence: 0.9},
			{Confidence: 0.7},
		}

		avg := exporter.CalculateAverageConfidence(findings)
		assert.InDelta(t, 0.8, avg, 0.01)

		// Test empty findings
		emptyAvg := exporter.CalculateAverageConfidence([]correlation.Finding{})
		assert.Equal(t, 0.0, emptyAvg)
	})
}
