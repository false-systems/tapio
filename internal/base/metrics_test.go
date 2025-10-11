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
	metrics, err := NewObserverMetrics("test-observer")
	require.NoError(t, err)
	require.NotNil(t, metrics)

	assert.NotNil(t, metrics.EventsProcessed)
	assert.NotNil(t, metrics.EventsDropped)
	assert.NotNil(t, metrics.ErrorsTotal)
	assert.NotNil(t, metrics.ProcessingTime)
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
					if len(sum.DataPoints) > 0 {
						return sum.DataPoints[0].Value
					}
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
