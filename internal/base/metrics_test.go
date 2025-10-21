package base

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewObserverMetrics(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	metrics, err := NewObserverMetrics("test-observer")
	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Existing metrics
	assert.NotNil(t, metrics.EventsProcessed)
	assert.NotNil(t, metrics.EventsDropped)
	assert.NotNil(t, metrics.ErrorsTotal)
	assert.NotNil(t, metrics.ProcessingTime)

	// Pipeline health metrics
	assert.NotNil(t, metrics.PipelineStagesActive)
	assert.NotNil(t, metrics.PipelineStagesFailed)
	assert.NotNil(t, metrics.PipelineQueueDepth)
	assert.NotNil(t, metrics.PipelineQueueUtilization)

	// Data quality metrics
	assert.NotNil(t, metrics.EventsOutOfOrder)
	assert.NotNil(t, metrics.EventsDuplicate)
	assert.NotNil(t, metrics.EventsEnrichmentFailed)

	// eBPF metrics (optional, can be nil for non-eBPF observers)
	assert.NotNil(t, metrics.EBPFMapSize)
	assert.NotNil(t, metrics.EBPFMapCapacity)
	assert.NotNil(t, metrics.EBPFRingBufferLost)
	assert.NotNil(t, metrics.EBPFRingBufferUtilization)
}

func TestObserverMetrics_RecordEvent(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	metrics, err := NewObserverMetrics("test-observer")
	require.NoError(t, err)

	ctx := context.Background()
	event := &domain.ObserverEvent{
		ID:        "test-1",
		Type:      "tcp_connect",
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	metrics.RecordEvent(ctx, "test-observer", event)
	metrics.RecordEvent(ctx, "test-observer", event)
	metrics.RecordEvent(ctx, "test-observer", event)

	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	sum := findSum(t, rm, "observer_events_processed_total")
	assert.Equal(t, int64(3), sum)
}

func TestObserverMetrics_RecordDrop(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	metrics, err := NewObserverMetrics("test-observer")
	require.NoError(t, err)

	ctx := context.Background()
	metrics.RecordDrop(ctx, "test-observer", "tcp_connect")
	metrics.RecordDrop(ctx, "test-observer", "tcp_connect")

	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	sum := findSum(t, rm, "observer_events_dropped_total")
	assert.Equal(t, int64(2), sum)
}

func TestObserverMetrics_RecordError(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	metrics, err := NewObserverMetrics("test-observer")
	require.NoError(t, err)

	ctx := context.Background()
	event := &domain.ObserverEvent{
		ID:        "test-1",
		Type:      "oom_kill",
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	metrics.RecordError(ctx, "test-observer", event)

	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	sum := findSum(t, rm, "observer_errors_total")
	assert.Equal(t, int64(1), sum)
}

func TestObserverMetrics_RecordProcessingTime(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	metrics, err := NewObserverMetrics("test-observer")
	require.NoError(t, err)

	ctx := context.Background()
	event := &domain.ObserverEvent{
		ID:        "test-1",
		Type:      "tcp_connect",
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	metrics.RecordProcessingTime(ctx, "test-observer", event, 10.5)
	metrics.RecordProcessingTime(ctx, "test-observer", event, 20.3)

	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	count, sum := findHistogram(t, rm, "observer_processing_duration_ms")
	assert.Equal(t, uint64(2), count)
	assert.InDelta(t, 30.8, sum, 0.01)
}

func findSum(t *testing.T, rm metricdata.ResourceMetrics, metricName string) int64 {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricName {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
					// Sum across all data points (different label combinations)
					var total int64
					for _, dp := range sum.DataPoints {
						total += dp.Value
					}
					return total
				}
			}
		}
	}

	t.Fatalf("metric %s not found", metricName)
	return 0
}

func findHistogram(t *testing.T, rm metricdata.ResourceMetrics, metricName string) (uint64, float64) {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricName {
				if hist, ok := m.Data.(metricdata.Histogram[float64]); ok {
					if len(hist.DataPoints) > 0 {
						return hist.DataPoints[0].Count, hist.DataPoints[0].Sum
					}
				}
			}
		}
	}

	t.Fatalf("histogram %s not found", metricName)
	return 0, 0
}

func findGaugeInt64(t *testing.T, rm metricdata.ResourceMetrics, metricName string) int64 {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricName {
				if gauge, ok := m.Data.(metricdata.Gauge[int64]); ok {
					if len(gauge.DataPoints) > 0 {
						return gauge.DataPoints[0].Value
					}
				}
			}
		}
	}

	t.Fatalf("int64 gauge %s not found", metricName)
	return 0
}

func findGaugeFloat64(t *testing.T, rm metricdata.ResourceMetrics, metricName string) float64 {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricName {
				if gauge, ok := m.Data.(metricdata.Gauge[float64]); ok {
					if len(gauge.DataPoints) > 0 {
						return gauge.DataPoints[0].Value
					}
				}
			}
		}
	}

	t.Fatalf("float64 gauge %s not found", metricName)
	return 0
}

// Pipeline Health Metrics Tests

func TestObserverMetrics_RecordPipelineQueue(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	metrics, err := NewObserverMetrics("test-observer")
	require.NoError(t, err)

	ctx := context.Background()
	metrics.RecordPipelineQueueDepth(ctx, "test-observer", 42)
	metrics.RecordPipelineQueueUtilization(ctx, "test-observer", 0.75)

	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	queueDepth := findGaugeInt64(t, rm, "observer_pipeline_queue_depth")
	assert.Equal(t, int64(42), queueDepth)

	utilization := findGaugeFloat64(t, rm, "observer_pipeline_queue_utilization_ratio")
	assert.InDelta(t, 0.75, utilization, 0.01)
}

func TestObserverMetrics_RecordPipelineStages(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	metrics, err := NewObserverMetrics("test-observer")
	require.NoError(t, err)

	ctx := context.Background()
	metrics.RecordPipelineStagesActive(ctx, "test-observer", 3)
	metrics.RecordPipelineStagesActive(ctx, "test-observer", 5)
	metrics.RecordPipelineStageFailed(ctx, "test-observer", "enrichment")
	metrics.RecordPipelineStageFailed(ctx, "test-observer", "validation")

	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	activeStages := findGaugeInt64(t, rm, "observer_pipeline_stages_active")
	assert.Equal(t, int64(5), activeStages)

	failedStages := findSum(t, rm, "observer_pipeline_stages_failed_total")
	assert.Equal(t, int64(2), failedStages)
}

// Data Quality Metrics Tests

func TestObserverMetrics_RecordDataQuality(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	metrics, err := NewObserverMetrics("test-observer")
	require.NoError(t, err)

	ctx := context.Background()
	metrics.RecordEventOutOfOrder(ctx, "test-observer", "tcp_connect")
	metrics.RecordEventOutOfOrder(ctx, "test-observer", "tcp_connect")
	metrics.RecordEventDuplicate(ctx, "test-observer", "dns_query")
	metrics.RecordEnrichmentFailed(ctx, "test-observer", "pod_lookup")
	metrics.RecordEnrichmentFailed(ctx, "test-observer", "service_lookup")
	metrics.RecordEnrichmentFailed(ctx, "test-observer", "deployment_lookup")

	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	outOfOrder := findSum(t, rm, "observer_events_out_of_order_total")
	assert.Equal(t, int64(2), outOfOrder)

	duplicates := findSum(t, rm, "observer_events_duplicate_total")
	assert.Equal(t, int64(1), duplicates)

	enrichmentFailed := findSum(t, rm, "observer_events_enrichment_failed_total")
	assert.Equal(t, int64(3), enrichmentFailed)
}

// eBPF Metrics Tests

func TestObserverMetrics_RecordEBPFHealth(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	metrics, err := NewObserverMetrics("test-observer")
	require.NoError(t, err)

	ctx := context.Background()
	metrics.RecordEBPFMapSize(ctx, "test-observer", 1500)
	metrics.RecordEBPFMapCapacity(ctx, "test-observer", 10000)
	metrics.RecordEBPFRingBufferLost(ctx, "test-observer", 42)
	metrics.RecordEBPFRingBufferUtilization(ctx, "test-observer", 0.85)

	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	mapSize := findGaugeInt64(t, rm, "observer_ebpf_map_entries")
	assert.Equal(t, int64(1500), mapSize)

	mapCapacity := findGaugeInt64(t, rm, "observer_ebpf_map_capacity")
	assert.Equal(t, int64(10000), mapCapacity)

	ringBufferLost := findSum(t, rm, "observer_ebpf_ringbuffer_lost_total")
	assert.Equal(t, int64(42), ringBufferLost)

	ringBufferUtil := findGaugeFloat64(t, rm, "observer_ebpf_ringbuffer_utilization_ratio")
	assert.InDelta(t, 0.85, ringBufferUtil, 0.01)
}
