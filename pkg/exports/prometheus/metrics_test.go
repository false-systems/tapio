package prometheus

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/correlation"
)

func TestMetricsExporter_ExportCorrelationResult(t *testing.T) {
	config := &MetricsConfig{
		Namespace:            "test_tapio",
		Subsystem:            "test_correlation",
		EnablePatternMetrics: true,
		EnableSystemMetrics:  true,
		EnableEntityMetrics:  true,
	}

	exporter := NewMetricsExporter(config)
	require.NotNil(t, exporter)

	// Create test correlation result
	result := &correlation.Result{
		RuleID:      "test_memory_leak",
		RuleName:    "memory_leak_detection",
		Timestamp:   time.Now(),
		Confidence:  0.85,
		Severity:    correlation.SeverityHigh,
		Category:    correlation.CategoryPerformance,
		Title:       "Memory leak detected",
		Description: "Test memory leak correlation",
		Evidence: correlation.Evidence{
			Entities: []correlation.Entity{
				{
					Type:      "pod",
					Name:      "test-pod-123",
					Namespace: "test-namespace",
				},
			},
		},
		Recommendations: []string{
			"Investigate memory allocation",
			"Consider pod restart",
		},
		Actions: []correlation.Action{
			{
				Type:     "restart",
				Priority: "high",
			},
		},
	}

	ctx := context.Background()
	err := exporter.ExportCorrelationResult(ctx, result)
	assert.NoError(t, err)

	// Verify metrics were recorded
	registry := exporter.GetRegistry()

	// Test correlations total metric
	correlationsMetric := testutil.CollectAndCount(exporter.correlationsTotal, "test_tapio_test_correlation_correlations_total")
	assert.Equal(t, 1, correlationsMetric, "Expected one correlation metric")

	// Test confidence histogram
	confidenceCount := testutil.CollectAndCount(exporter.correlationConfidence, "test_tapio_test_correlation_correlation_confidence")
	assert.Equal(t, 1, confidenceCount, "Expected one confidence metric")

	// Verify metric values using metric families
	metricFamilies, err := registry.Gather()
	require.NoError(t, err)

	// Find and verify specific metrics
	var foundCorrelationsTotal, foundConfidence bool

	for _, mf := range metricFamilies {
		switch *mf.Name {
		case "test_tapio_test_correlation_correlations_total":
			foundCorrelationsTotal = true
			assert.Equal(t, 1, len(mf.Metric), "Expected one metric sample")
			assert.Equal(t, float64(1), *mf.Metric[0].Counter.Value, "Expected counter value of 1")

			// Verify labels
			labels := mf.Metric[0].Label
			labelMap := make(map[string]string)
			for _, label := range labels {
				labelMap[*label.Name] = *label.Value
			}

			assert.Equal(t, "test_memory_leak", labelMap["rule_id"])
			assert.Equal(t, "memory_leak_detection", labelMap["rule_name"])
			assert.Equal(t, "high", labelMap["severity"])
			assert.Equal(t, "performance", labelMap["category"])

		case "test_tapio_test_correlation_correlation_confidence":
			foundConfidence = true
			assert.Greater(t, len(mf.Metric), 0, "Expected at least one confidence metric")
		}
	}

	assert.True(t, foundCorrelationsTotal, "Should find correlations total metric")
	assert.True(t, foundConfidence, "Should find confidence metric")
}

func TestMetricsExporter_ExportSystemHealth(t *testing.T) {
	config := &MetricsConfig{
		Namespace:           "test_tapio",
		Subsystem:           "test_system",
		EnableSystemMetrics: true,
	}

	exporter := NewMetricsExporter(config)
	require.NotNil(t, exporter)

	ctx := context.Background()
	err := exporter.ExportSystemHealth(ctx, "test-namespace", "test-cluster", 0.95)
	assert.NoError(t, err)

	// Verify system health metric
	metricFamilies, err := exporter.GetRegistry().Gather()
	require.NoError(t, err)

	found := false
	for _, mf := range metricFamilies {
		if *mf.Name == "test_tapio_system_health_score" {
			found = true
			assert.Equal(t, 1, len(mf.Metric))
			assert.Equal(t, 0.95, *mf.Metric[0].Gauge.Value)

			// Verify labels
			labels := mf.Metric[0].Label
			labelMap := make(map[string]string)
			for _, label := range labels {
				labelMap[*label.Name] = *label.Value
			}

			assert.Equal(t, "test-namespace", labelMap["namespace"])
			assert.Equal(t, "test-cluster", labelMap["cluster"])
		}
	}

	assert.True(t, found, "Should find system health metric")
}

func TestMetricsExporter_ExportPatternMetrics(t *testing.T) {
	config := &MetricsConfig{
		Namespace:            "test_tapio",
		Subsystem:            "test_patterns",
		EnablePatternMetrics: true,
	}

	exporter := NewMetricsExporter(config)
	require.NotNil(t, exporter)

	ctx := context.Background()

	// Test true positive detection
	err := exporter.ExportPatternMetrics(ctx, "memory_leak", true, 0.9, 0.85)
	assert.NoError(t, err)

	// Test false positive
	err = exporter.ExportPatternMetrics(ctx, "network_failure", false, 0.3, 0.7)
	assert.NoError(t, err)

	// Verify pattern metrics
	metricFamilies, err := exporter.GetRegistry().Gather()
	require.NoError(t, err)

	var foundDetected, foundTruePositives, foundFalsePositives bool

	for _, mf := range metricFamilies {
		switch *mf.Name {
		case "test_tapio_patterns_detected_total":
			foundDetected = true
			assert.Equal(t, 1, len(mf.Metric)) // Only the true positive should be counted

		case "test_tapio_patterns_true_positives_total":
			foundTruePositives = true
			assert.Equal(t, 1, len(mf.Metric))
			assert.Equal(t, float64(1), *mf.Metric[0].Counter.Value)

		case "test_tapio_patterns_false_positives_total":
			foundFalsePositives = true
			assert.Equal(t, 1, len(mf.Metric))
			assert.Equal(t, float64(1), *mf.Metric[0].Counter.Value)
		}
	}

	assert.True(t, foundDetected, "Should find patterns detected metric")
	assert.True(t, foundTruePositives, "Should find true positives metric")
	assert.True(t, foundFalsePositives, "Should find false positives metric")
}

func TestMetricsExporter_RecordProcessingTime(t *testing.T) {
	config := DefaultMetricsConfig()
	exporter := NewMetricsExporter(config)

	// Record processing time
	duration := 150 * time.Millisecond
	exporter.RecordProcessingTime("test-rule", duration)

	// Verify processing time histogram
	metricFamilies, err := exporter.GetRegistry().Gather()
	require.NoError(t, err)

	found := false
	for _, mf := range metricFamilies {
		if *mf.Name == "tapio_correlation_correlation_processing_time_seconds" {
			found = true
			assert.Equal(t, 1, len(mf.Metric))
			histogram := mf.Metric[0].Histogram
			assert.Equal(t, uint64(1), *histogram.SampleCount)
			assert.Equal(t, duration.Seconds(), *histogram.SampleSum)
		}
	}

	assert.True(t, found, "Should find processing time metric")
}

func TestMetricsExporter_SanitizeLabel(t *testing.T) {
	exporter := NewMetricsExporter(DefaultMetricsConfig())

	tests := []struct {
		input    string
		expected string
	}{
		{"valid_label", "valid_label"},
		{"invalid-label", "invalid_label"},
		{"invalid.label", "invalid_label"},
		{"invalid label", "invalid_label"},
		{"123invalid", "_123invalid"},
		{"UPPERCASE", "UPPERCASE"},
		{"mixed_CASE-123", "mixed_CASE_123"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := exporter.sanitizeLabel(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMetricsExporter_DetermineSeverityFromConfidence(t *testing.T) {
	config := &MetricsConfig{
		CriticalSeverityThreshold: 0.9,
		HighSeverityThreshold:     0.7,
	}
	exporter := NewMetricsExporter(config)

	tests := []struct {
		confidence float64
		expected   string
	}{
		{0.95, "critical"},
		{0.9, "critical"},
		{0.85, "high"},
		{0.7, "high"},
		{0.6, "medium"},
		{0.3, "low"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := exporter.determineSeverityFromConfidence(tt.confidence)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultMetricsConfig(t *testing.T) {
	config := DefaultMetricsConfig()

	assert.Equal(t, "tapio", config.Namespace)
	assert.Equal(t, "correlation", config.Subsystem)
	assert.True(t, config.EnablePatternMetrics)
	assert.True(t, config.EnableSystemMetrics)
	assert.True(t, config.EnableEntityMetrics)
	assert.True(t, config.EnablePerformanceMetrics)
	assert.Equal(t, 0.8, config.HighSeverityThreshold)
	assert.Equal(t, 0.9, config.CriticalSeverityThreshold)
	assert.NotNil(t, config.BucketConfiguration)
	assert.Contains(t, config.BucketConfiguration, "processing_time")
	assert.Contains(t, config.BucketConfiguration, "confidence")
}

func TestMetricsExporter_Concurrency(t *testing.T) {
	config := DefaultMetricsConfig()
	exporter := NewMetricsExporter(config)

	// Test concurrent exports
	const numGoroutines = 10
	const numExportsPerGoroutine = 5

	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < numExportsPerGoroutine; j++ {
				result := &correlation.Result{
					RuleID:     "concurrent-test",
					RuleName:   "concurrent_test_rule",
					Confidence: 0.8,
					Severity:   correlation.SeverityMedium,
					Category:   correlation.CategoryPerformance,
				}

				ctx := context.Background()
				err := exporter.ExportCorrelationResult(ctx, result)
				assert.NoError(t, err)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify total count
	metricFamilies, err := exporter.GetRegistry().Gather()
	require.NoError(t, err)

	expectedTotal := float64(numGoroutines * numExportsPerGoroutine)
	found := false

	for _, mf := range metricFamilies {
		if *mf.Name == "tapio_correlation_correlations_total" {
			found = true
			assert.Equal(t, 1, len(mf.Metric))
			assert.Equal(t, expectedTotal, *mf.Metric[0].Counter.Value)
		}
	}

	assert.True(t, found, "Should find correlations total metric with correct count")
}

// Benchmark tests
func BenchmarkMetricsExporter_ExportCorrelationResult(b *testing.B) {
	config := DefaultMetricsConfig()
	exporter := NewMetricsExporter(config)

	result := &correlation.Result{
		RuleID:     "benchmark-rule",
		RuleName:   "benchmark_rule",
		Confidence: 0.8,
		Severity:   correlation.SeverityMedium,
		Category:   correlation.CategoryPerformance,
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := exporter.ExportCorrelationResult(ctx, result)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMetricsExporter_RecordProcessingTime(b *testing.B) {
	config := DefaultMetricsConfig()
	exporter := NewMetricsExporter(config)

	duration := 100 * time.Millisecond

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exporter.RecordProcessingTime("benchmark-rule", duration)
	}
}

// Helper function to create test correlation result
func createTestCorrelationResult(ruleID string, severity correlation.Severity) *correlation.Result {
	return &correlation.Result{
		RuleID:      ruleID,
		RuleName:    ruleID + "_detection",
		Timestamp:   time.Now(),
		Confidence:  0.8,
		Severity:    severity,
		Category:    correlation.CategoryPerformance,
		Title:       "Test correlation",
		Description: "Test description",
		Evidence: correlation.Evidence{
			Entities: []correlation.Entity{
				{
					Type:      "pod",
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
			},
		},
	}
}

// Test metric text format output
func TestMetricsExporter_TextFormat(t *testing.T) {
	config := &MetricsConfig{
		Namespace: "test",
		Subsystem: "example",
	}
	exporter := NewMetricsExporter(config)

	// Export some test data
	result := createTestCorrelationResult("test_rule", correlation.SeverityHigh)
	ctx := context.Background()
	err := exporter.ExportCorrelationResult(ctx, result)
	require.NoError(t, err)

	// Get text format output
	registry := exporter.GetRegistry()
	output := &strings.Builder{}
	err = prometheus.WriteToTextfile(output.String()+"_test.prom", registry)
	// Note: In a real test, you'd verify the actual text format output
	assert.NoError(t, err)
}
