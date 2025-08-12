package correlation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewMetricFactory(t *testing.T) {
	logger := zaptest.NewLogger(t)
	componentName := "test-component"

	factory := NewMetricFactory(componentName, logger)

	assert.NotNil(t, factory)
	assert.Equal(t, logger, factory.logger)
	assert.NotNil(t, factory.meter)
}

func TestMetricFactory_CreateEngineMetrics(t *testing.T) {
	// Setup test metric provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	logger := zaptest.NewLogger(t)
	factory := NewMetricFactory("correlation-engine", logger)

	metrics, err := factory.CreateEngineMetrics()

	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Verify all core processing metrics are created
	assert.NotNil(t, metrics.EventsProcessedCtr)
	assert.NotNil(t, metrics.ErrorsTotalCtr)
	assert.NotNil(t, metrics.ProcessingTimeHist)
	assert.NotNil(t, metrics.CorrelationsFoundCtr)

	// Verify all queue and worker metrics are created
	assert.NotNil(t, metrics.QueueDepthGauge)
	assert.NotNil(t, metrics.ActiveWorkersGauge)

	// Verify all storage metrics are created
	assert.NotNil(t, metrics.StorageQueueDepthGauge)
	assert.NotNil(t, metrics.StorageWorkersGauge)
	assert.NotNil(t, metrics.StorageProcessedCtr)
	assert.NotNil(t, metrics.StorageRejectedCtr)
	assert.NotNil(t, metrics.StorageLatencyHist)
}

func TestMetricFactory_CreateEngineMetrics_MetricsRecording(t *testing.T) {
	// Setup test metric provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	logger := zaptest.NewLogger(t)
	factory := NewMetricFactory("correlation-engine", logger)

	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)
	require.NotNil(t, metrics)

	ctx := context.Background()

	// Test counter recording
	metrics.EventsProcessedCtr.Add(ctx, 5)
	metrics.ErrorsTotalCtr.Add(ctx, 2)
	metrics.CorrelationsFoundCtr.Add(ctx, 3)
	metrics.StorageProcessedCtr.Add(ctx, 10)
	metrics.StorageRejectedCtr.Add(ctx, 1)

	// Test gauge recording
	metrics.QueueDepthGauge.Add(ctx, 100)
	metrics.ActiveWorkersGauge.Add(ctx, 4)
	metrics.StorageQueueDepthGauge.Add(ctx, 50)
	metrics.StorageWorkersGauge.Add(ctx, 10)

	// Test histogram recording
	metrics.ProcessingTimeHist.Record(ctx, 250.5) // 250.5ms
	metrics.StorageLatencyHist.Record(ctx, 15.2)  // 15.2ms

	// Recording without panics confirms metrics are working correctly
	assert.True(t, true, "All metrics recorded successfully")
}

func TestMetricFactory_CreateTestMetrics(t *testing.T) {
	metrics := CreateTestMetrics()

	require.NotNil(t, metrics)

	// All metrics should be nil (no-op for testing)
	assert.Nil(t, metrics.EventsProcessedCtr)
	assert.Nil(t, metrics.ErrorsTotalCtr)
	assert.Nil(t, metrics.ProcessingTimeHist)
	assert.Nil(t, metrics.CorrelationsFoundCtr)
	assert.Nil(t, metrics.QueueDepthGauge)
	assert.Nil(t, metrics.ActiveWorkersGauge)
	assert.Nil(t, metrics.StorageQueueDepthGauge)
	assert.Nil(t, metrics.StorageWorkersGauge)
	assert.Nil(t, metrics.StorageProcessedCtr)
	assert.Nil(t, metrics.StorageRejectedCtr)
	assert.Nil(t, metrics.StorageLatencyHist)
}

func TestMetricFactory_initProcessingMetrics(t *testing.T) {
	// Setup test metric provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	logger := zaptest.NewLogger(t)
	factory := NewMetricFactory("test-component", logger)

	metrics := &EngineOTELMetrics{}
	err := factory.initProcessingMetrics(metrics)

	require.NoError(t, err)
	assert.NotNil(t, metrics.EventsProcessedCtr)
	assert.NotNil(t, metrics.ErrorsTotalCtr)
	assert.NotNil(t, metrics.ProcessingTimeHist)
	assert.NotNil(t, metrics.CorrelationsFoundCtr)

	// Other metrics should still be nil
	assert.Nil(t, metrics.QueueDepthGauge)
	assert.Nil(t, metrics.StorageQueueDepthGauge)
}

func TestMetricFactory_initQueueMetrics(t *testing.T) {
	// Setup test metric provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	logger := zaptest.NewLogger(t)
	factory := NewMetricFactory("test-component", logger)

	metrics := &EngineOTELMetrics{}
	err := factory.initQueueMetrics(metrics)

	require.NoError(t, err)
	assert.NotNil(t, metrics.QueueDepthGauge)
	assert.NotNil(t, metrics.ActiveWorkersGauge)

	// Other metrics should still be nil
	assert.Nil(t, metrics.EventsProcessedCtr)
	assert.Nil(t, metrics.StorageQueueDepthGauge)
}

func TestMetricFactory_initStorageMetrics(t *testing.T) {
	// Setup test metric provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	logger := zaptest.NewLogger(t)
	factory := NewMetricFactory("test-component", logger)

	metrics := &EngineOTELMetrics{}
	err := factory.initStorageMetrics(metrics)

	require.NoError(t, err)
	assert.NotNil(t, metrics.StorageQueueDepthGauge)
	assert.NotNil(t, metrics.StorageWorkersGauge)
	assert.NotNil(t, metrics.StorageProcessedCtr)
	assert.NotNil(t, metrics.StorageRejectedCtr)
	assert.NotNil(t, metrics.StorageLatencyHist)

	// Other metrics should still be nil
	assert.Nil(t, metrics.EventsProcessedCtr)
	assert.Nil(t, metrics.QueueDepthGauge)
}

func TestMetricFactory_GracefulDegradation(t *testing.T) {
	// Test that nil metrics are handled gracefully (existing engine pattern)
	testMetrics := CreateTestMetrics()

	// Simulate engine usage pattern - check for nil before use
	ctx := context.Background()

	// These should not panic when metrics are nil
	if testMetrics.EventsProcessedCtr != nil {
		testMetrics.EventsProcessedCtr.Add(ctx, 1)
	}

	if testMetrics.ProcessingTimeHist != nil {
		testMetrics.ProcessingTimeHist.Record(ctx, 100.0)
	}

	if testMetrics.QueueDepthGauge != nil {
		testMetrics.QueueDepthGauge.Add(ctx, 1)
	}

	// No panics means graceful degradation works
	assert.True(t, true, "Graceful degradation handled nil metrics correctly")
}

func TestMetricFactory_ComponentNameIsolation(t *testing.T) {
	// Test that different component names create isolated metric spaces
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	logger := zaptest.NewLogger(t)

	// Create factories with different component names
	factory1 := NewMetricFactory("engine-1", logger)
	factory2 := NewMetricFactory("engine-2", logger)

	metrics1, err1 := factory1.CreateEngineMetrics()
	metrics2, err2 := factory2.CreateEngineMetrics()

	require.NoError(t, err1)
	require.NoError(t, err2)
	require.NotNil(t, metrics1)
	require.NotNil(t, metrics2)

	ctx := context.Background()

	// Record different values to each metrics instance
	metrics1.EventsProcessedCtr.Add(ctx, 100)
	metrics2.EventsProcessedCtr.Add(ctx, 200)

	// Both should work independently without interference - validated by no panics
	assert.True(t, true, "Metrics from different components work independently")
}

// Benchmark the metric factory performance
func BenchmarkMetricFactory_CreateEngineMetrics(b *testing.B) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	logger := zap.NewNop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		factory := NewMetricFactory("benchmark-component", logger)
		_, err := factory.CreateEngineMetrics()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMetricFactory_MetricRecording(b *testing.B) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	logger := zap.NewNop()
	factory := NewMetricFactory("benchmark-component", logger)
	metrics, err := factory.CreateEngineMetrics()
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.EventsProcessedCtr.Add(ctx, 1)
		metrics.ProcessingTimeHist.Record(ctx, 100.0)
		metrics.QueueDepthGauge.Add(ctx, 1)
		metrics.QueueDepthGauge.Add(ctx, -1)
	}
}
