package grpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.uber.org/zap"
)

func TestPrometheusMetricsCollector_RecordEvent(t *testing.T) {
	logger := zap.NewNop()
	collector := NewPrometheusMetricsCollector(logger)

	// Record events
	collector.RecordEvent("process", "collector-a")
	collector.RecordEvent("process", "collector-a")
	collector.RecordEvent("network", "collector-b")
	collector.RecordEvent("kernel", "collector-a")

	// Verify counters
	assert.Equal(t, uint64(2), collector.eventsTotal["events_total_type_process"].value.Load())
	assert.Equal(t, uint64(1), collector.eventsTotal["events_total_type_network"].value.Load())
	assert.Equal(t, uint64(1), collector.eventsTotal["events_total_type_kernel"].value.Load())

	// Verify source counters
	assert.Equal(t, uint64(3), collector.eventsSource["events_by_source_source_collector-a"].value.Load())
	assert.Equal(t, uint64(1), collector.eventsSource["events_by_source_source_collector-b"].value.Load())
}

func TestPrometheusMetricsCollector_RecordCorrelation(t *testing.T) {
	logger := zap.NewNop()
	collector := NewPrometheusMetricsCollector(logger)

	// Record correlations with different confidence levels
	collector.RecordCorrelation("error_cascade", 0.95)
	collector.RecordCorrelation("error_cascade", 0.85)
	collector.RecordCorrelation("latency_spike", 0.75)
	collector.RecordCorrelation("resource_exhaustion", 0.65)

	// Verify correlation counters
	highConfKey := "correlations_total_pattern_error_cascade_confidence_level_high"
	mediumConfKey := "correlations_total_pattern_latency_spike_confidence_level_medium"
	lowConfKey := "correlations_total_pattern_resource_exhaustion_confidence_level_low"

	assert.Equal(t, uint64(2), collector.correlationsTotal[highConfKey].value.Load())
	assert.Equal(t, uint64(1), collector.correlationsTotal[mediumConfKey].value.Load())
	assert.Equal(t, uint64(1), collector.correlationsTotal[lowConfKey].value.Load())
}

func TestPrometheusMetricsCollector_RecordLatency(t *testing.T) {
	logger := zap.NewNop()
	collector := NewPrometheusMetricsCollector(logger)

	// Record latencies
	collector.RecordLatency("submit_event", 5*time.Millisecond)
	collector.RecordLatency("submit_event", 15*time.Millisecond)
	collector.RecordLatency("submit_event", 100*time.Millisecond)
	collector.RecordLatency("query_events", 500*time.Millisecond)

	// Get histogram
	submitHistogram := collector.latencyHistograms["operation_duration_seconds_operation_submit_event"]
	require.NotNil(t, submitHistogram)

	// Verify count and sum
	assert.Equal(t, uint64(3), submitHistogram.count.Load())
	expectedSum := (5 + 15 + 100) / 1000.0 // Convert to seconds
	assert.InDelta(t, expectedSum, submitHistogram.sum.Load(), 0.001)

	// Verify buckets
	// 5ms = 0.005s should be in buckets <= 0.005
	// 15ms = 0.015s should be in buckets <= 0.025
	// 100ms = 0.1s should be in buckets <= 0.1
	assert.GreaterOrEqual(t, submitHistogram.bucketCounts[1].Load(), uint64(1)) // 0.005
	assert.GreaterOrEqual(t, submitHistogram.bucketCounts[3].Load(), uint64(2)) // 0.025
	assert.GreaterOrEqual(t, submitHistogram.bucketCounts[5].Load(), uint64(3)) // 0.1
}

func TestPrometheusMetricsCollector_GetMetrics(t *testing.T) {
	logger := zap.NewNop()
	collector := NewPrometheusMetricsCollector(logger)

	// Generate some metrics
	collector.RecordEvent("process", "test-source")
	collector.RecordCorrelation("error_cascade", 0.9)
	collector.RecordLatency("operation", 50*time.Millisecond)
	collector.UpdateGauge("active_streams", 5)
	collector.UpdateGauge("storage_utilization", 75.5)
	collector.UpdateGauge("correlation_count", 10)

	tests := []struct {
		name      string
		component pb.TapioGetMetricsRequest_Component
		minCount  int
	}{
		{
			name:      "all components",
			component: pb.TapioGetMetricsRequest_COMPONENT_ALL,
			minCount:  4,
		},
		{
			name:      "server component",
			component: pb.TapioGetMetricsRequest_COMPONENT_SERVER,
			minCount:  2,
		},
		{
			name:      "collectors component",
			component: pb.TapioGetMetricsRequest_COMPONENT_COLLECTORS,
			minCount:  1,
		},
		{
			name:      "correlation component",
			component: pb.TapioGetMetricsRequest_COMPONENT_CORRELATION,
			minCount:  2,
		},
		{
			name:      "storage component",
			component: pb.TapioGetMetricsRequest_COMPONENT_STORAGE,
			minCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics, err := collector.GetMetrics(tt.component)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, len(metrics), tt.minCount)

			// Verify metric structure
			for _, metric := range metrics {
				assert.NotEmpty(t, metric.Name)
				assert.NotEmpty(t, metric.Component)
				assert.NotEmpty(t, metric.Unit)
				assert.GreaterOrEqual(t, metric.Value, float64(0))
			}
		})
	}
}

func TestPrometheusMetricsCollector_UpdateGauge(t *testing.T) {
	logger := zap.NewNop()
	collector := NewPrometheusMetricsCollector(logger)

	// Update gauges
	collector.UpdateGauge("active_streams", 10)
	collector.UpdateGauge("storage_utilization", 85.5)
	collector.UpdateGauge("correlation_count", 25)

	// Verify values
	assert.Equal(t, int64(10), collector.activeStreams.Load())
	assert.Equal(t, 85.5, collector.storageUtilization.Load())
	assert.Equal(t, int64(25), collector.correlationCount.Load())

	// Update again
	collector.UpdateGauge("active_streams", 15)
	assert.Equal(t, int64(15), collector.activeStreams.Load())
}

func TestPrometheusMetricsCollector_Health(t *testing.T) {
	logger := zap.NewNop()
	collector := NewPrometheusMetricsCollector(logger)

	// Initially healthy
	health := collector.Health()
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, health.Status)
	assert.Contains(t, health.Message, "healthy")

	// Create many metrics to test cardinality warning
	for i := 0; i < 5000; i++ {
		collector.RecordEvent("type-"+string(rune(i%100)), "source-"+string(rune(i%50)))
		if i%10 == 0 {
			collector.RecordCorrelation("pattern-"+string(rune(i%20)), 0.8)
		}
		if i%5 == 0 {
			collector.RecordLatency("op-"+string(rune(i%30)), time.Duration(i)*time.Millisecond)
		}
	}

	// Check if degraded due to high cardinality
	health = collector.Health()
	totalMetrics := health.Metrics["total_metrics"]
	if totalMetrics > 10000 {
		assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_DEGRADED, health.Status)
		assert.Contains(t, health.Message, "High metric cardinality")
	}
}

func TestPrometheusMetricsCollector_GetStatistics(t *testing.T) {
	logger := zap.NewNop()
	collector := NewPrometheusMetricsCollector(logger)

	// Generate various metrics
	for i := 0; i < 100; i++ {
		collector.RecordEvent("process", "source-a")
		if i%10 == 0 {
			collector.RecordEvent("network", "source-b")
		}
		if i%5 == 0 {
			collector.RecordCorrelation("error_cascade", 0.85)
		}
		if i%3 == 0 {
			collector.RecordLatency("query", time.Duration(i)*time.Millisecond)
		}
	}

	collector.UpdateGauge("active_streams", 5)
	collector.UpdateGauge("storage_utilization", 65.5)
	collector.UpdateGauge("correlation_count", 15)

	// Get statistics
	stats := collector.GetStatistics()

	// Verify basic stats
	assert.Equal(t, uint64(110), stats["total_events"])      // 100 + 10
	assert.Equal(t, uint64(20), stats["total_correlations"]) // 100/5
	assert.Equal(t, int64(5), stats["active_streams"])
	assert.Equal(t, 65.5, stats["storage_utilization"])
	assert.Equal(t, int64(15), stats["correlation_count"])
	assert.Greater(t, stats["uptime_seconds"], float64(0))

	// Verify metric counts
	metricCount := stats["metric_count"].(map[string]int)
	assert.Greater(t, metricCount["counters"], 0)
	assert.Greater(t, metricCount["histograms"], 0)
	assert.Equal(t, 3, metricCount["gauges"])

	// Verify histogram statistics
	histograms := stats["histograms"].(map[string]interface{})
	assert.Greater(t, len(histograms), 0)

	for _, histStats := range histograms {
		hist := histStats.(map[string]interface{})
		assert.Contains(t, hist, "count")
		assert.Contains(t, hist, "sum")
		assert.Contains(t, hist, "avg")
		assert.Contains(t, hist, "p50")
		assert.Contains(t, hist, "p90")
		assert.Contains(t, hist, "p99")
	}
}

func TestMetricCounter(t *testing.T) {
	counter := &MetricCounter{
		name: "test_counter",
		labels: map[string]string{
			"type": "test",
		},
	}

	// Test Inc
	counter.Inc()
	assert.Equal(t, uint64(1), counter.Get())

	counter.Inc()
	counter.Inc()
	assert.Equal(t, uint64(3), counter.Get())

	// Test Add
	counter.Add(10)
	assert.Equal(t, uint64(13), counter.Get())

	counter.Add(7)
	assert.Equal(t, uint64(20), counter.Get())
}

func TestMetricHistogram(t *testing.T) {
	histogram := &MetricHistogram{
		name: "test_histogram",
		labels: map[string]string{
			"operation": "test",
		},
		buckets:      []float64{0.1, 0.5, 1.0, 2.0, 5.0},
		bucketCounts: make([]atomic.Uint64, 5),
	}

	// Observe values
	values := []float64{0.05, 0.3, 0.7, 1.5, 3.0, 10.0}
	for _, v := range values {
		histogram.Observe(v)
	}

	// Verify count and sum
	assert.Equal(t, uint64(6), histogram.count.Load())
	expectedSum := 0.05 + 0.3 + 0.7 + 1.5 + 3.0 + 10.0
	assert.InDelta(t, expectedSum, histogram.sum.Load(), 0.01)

	// Verify bucket counts
	// 0.05 <= 0.1 (bucket 0)
	// 0.3 <= 0.5 (buckets 0,1)
	// 0.7 <= 1.0 (buckets 0,1,2)
	// 1.5 <= 2.0 (buckets 0,1,2,3)
	// 3.0 <= 5.0 (buckets 0,1,2,3,4)
	// 10.0 > 5.0 (all buckets)
	assert.Equal(t, uint64(6), histogram.bucketCounts[0].Load()) // <= 0.1
	assert.Equal(t, uint64(5), histogram.bucketCounts[1].Load()) // <= 0.5
	assert.Equal(t, uint64(4), histogram.bucketCounts[2].Load()) // <= 1.0
	assert.Equal(t, uint64(3), histogram.bucketCounts[3].Load()) // <= 2.0
	assert.Equal(t, uint64(2), histogram.bucketCounts[4].Load()) // <= 5.0
}

func TestMetricHistogram_GetPercentile(t *testing.T) {
	histogram := &MetricHistogram{
		name:         "test_histogram",
		buckets:      []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0},
		bucketCounts: make([]atomic.Uint64, 6),
	}

	// Add 100 observations distributed across buckets
	for i := 0; i < 100; i++ {
		var value float64
		switch {
		case i < 10:
			value = 0.05 // 10 values <= 0.1
		case i < 30:
			value = 0.3 // 20 values <= 0.5
		case i < 60:
			value = 0.8 // 30 values <= 1.0
		case i < 80:
			value = 1.5 // 20 values <= 2.0
		case i < 95:
			value = 3.0 // 15 values <= 5.0
		default:
			value = 8.0 // 5 values <= 10.0
		}
		histogram.Observe(value)
	}

	// Test percentiles
	tests := []struct {
		percentile float64
		expected   float64
		tolerance  float64
	}{
		{0.1, 0.05, 0.1}, // p10 should be in first bucket
		{0.5, 0.75, 0.5}, // p50 should be in third bucket
		{0.9, 3.5, 2.0},  // p90 should be in fifth bucket
		{0.99, 7.5, 3.0}, // p99 should be in last bucket
	}

	for _, tt := range tests {
		t.Run(string(rune(int(tt.percentile*100)))+"th percentile", func(t *testing.T) {
			result := histogram.GetPercentile(tt.percentile)
			assert.InDelta(t, tt.expected, result, tt.tolerance)
		})
	}

	// Test edge cases
	assert.Equal(t, float64(0), histogram.GetPercentile(-0.1))
	assert.Equal(t, float64(0), histogram.GetPercentile(1.5))

	// Test empty histogram
	emptyHist := &MetricHistogram{
		buckets:      []float64{1.0},
		bucketCounts: make([]atomic.Uint64, 1),
	}
	assert.Equal(t, float64(0), emptyHist.GetPercentile(0.5))
}

func TestPrometheusMetricsCollector_ConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()
	collector := NewPrometheusMetricsCollector(logger)

	// Run concurrent operations
	errCh := make(chan error, 40)

	// Event recorders
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				collector.RecordEvent("type-"+string(rune(id)), "source-"+string(rune(id)))
			}
			errCh <- nil
		}(i)
	}

	// Correlation recorders
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 50; j++ {
				collector.RecordCorrelation("pattern-"+string(rune(id)), 0.8)
			}
			errCh <- nil
		}(i)
	}

	// Latency recorders
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 50; j++ {
				collector.RecordLatency("op-"+string(rune(id)), time.Duration(j)*time.Millisecond)
			}
			errCh <- nil
		}(i)
	}

	// Metric readers
	for i := 0; i < 10; i++ {
		go func() {
			_, err := collector.GetMetrics(pb.TapioGetMetricsRequest_COMPONENT_ALL)
			errCh <- err
		}()
	}

	// Wait for all operations
	for i := 0; i < 40; i++ {
		err := <-errCh
		assert.NoError(t, err)
	}

	// Verify data integrity
	stats := collector.GetStatistics()
	assert.Greater(t, stats["total_events"], uint64(0))
	assert.Greater(t, stats["total_correlations"], uint64(0))
}

func TestPrometheusMetricsCollector_MetricKey(t *testing.T) {
	logger := zap.NewNop()
	collector := NewPrometheusMetricsCollector(logger)

	tests := []struct {
		name     string
		labels   map[string]string
		expected string
	}{
		{
			name:     "events_total",
			labels:   map[string]string{"type": "process"},
			expected: "events_total_type_process",
		},
		{
			name:     "events_by_source",
			labels:   map[string]string{"source": "collector-a"},
			expected: "events_by_source_source_collector-a",
		},
		{
			name: "correlations_total",
			labels: map[string]string{
				"pattern":          "error_cascade",
				"confidence_level": "high",
			},
			expected: "correlations_total_pattern_error_cascade_confidence_level_high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := collector.metricKey(tt.name, tt.labels)
			assert.Equal(t, tt.expected, key)
		})
	}
}

// Benchmarks
func BenchmarkMetricsCollector_RecordEvent(b *testing.B) {
	logger := zap.NewNop()
	collector := NewPrometheusMetricsCollector(logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.RecordEvent("process", "benchmark")
	}
}

func BenchmarkMetricsCollector_RecordLatency(b *testing.B) {
	logger := zap.NewNop()
	collector := NewPrometheusMetricsCollector(logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.RecordLatency("operation", time.Duration(i)*time.Microsecond)
	}
}

func BenchmarkMetricsCollector_GetMetrics(b *testing.B) {
	logger := zap.NewNop()
	collector := NewPrometheusMetricsCollector(logger)

	// Pre-populate with metrics
	for i := 0; i < 100; i++ {
		collector.RecordEvent("type-"+string(rune(i%10)), "source-"+string(rune(i%5)))
		collector.RecordCorrelation("pattern-"+string(rune(i%5)), 0.8)
		collector.RecordLatency("op-"+string(rune(i%3)), time.Duration(i)*time.Millisecond)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.GetMetrics(pb.TapioGetMetricsRequest_COMPONENT_ALL)
	}
}
