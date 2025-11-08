package base

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// setupMeterProvider creates a test meter provider
func setupMeterProvider(t *testing.T) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(nil)
	})
}

// RED: Test NewMetricBuilder creates builder
func TestNewMetricBuilder(t *testing.T) {
	setupMeterProvider(t)

	builder := NewMetricBuilder("test-observer")
	require.NotNil(t, builder)
	assert.NotNil(t, builder.meter)
	assert.Empty(t, builder.errs)
}

// RED: Test Counter creates Int64Counter
func TestMetricBuilder_Counter(t *testing.T) {
	setupMeterProvider(t)

	builder := NewMetricBuilder("test")

	var counter metric.Int64Counter
	builder.Counter(&counter, "test_counter_total", "Test counter")

	assert.NoError(t, builder.Build())
	assert.NotNil(t, counter)
}

// RED: Test Gauge creates Float64Gauge
func TestMetricBuilder_Gauge(t *testing.T) {
	setupMeterProvider(t)

	builder := NewMetricBuilder("test")

	var gauge metric.Float64Gauge
	builder.Gauge(&gauge, "test_gauge", "Test gauge")

	assert.NoError(t, builder.Build())
	assert.NotNil(t, gauge)
}

// RED: Test Int64Gauge creates Int64Gauge
func TestMetricBuilder_Int64Gauge(t *testing.T) {
	setupMeterProvider(t)

	builder := NewMetricBuilder("test")

	var gauge metric.Int64Gauge
	builder.Int64Gauge(&gauge, "test_int64_gauge", "Test int64 gauge")

	assert.NoError(t, builder.Build())
	assert.NotNil(t, gauge)
}

// RED: Test Histogram creates Float64Histogram
func TestMetricBuilder_Histogram(t *testing.T) {
	setupMeterProvider(t)

	builder := NewMetricBuilder("test")

	var histogram metric.Float64Histogram
	builder.Histogram(&histogram, "test_histogram_ms", "Test histogram")

	assert.NoError(t, builder.Build())
	assert.NotNil(t, histogram)
}

// RED: Test fluent chaining
func TestMetricBuilder_FluentChaining(t *testing.T) {
	setupMeterProvider(t)

	builder := NewMetricBuilder("test")

	var counter metric.Int64Counter
	var gauge metric.Float64Gauge
	var int64Gauge metric.Int64Gauge
	var histogram metric.Float64Histogram

	// Chain all metric creations
	err := builder.
		Counter(&counter, "events_total", "Total events").
		Gauge(&gauge, "queue_size", "Queue size").
		Int64Gauge(&int64Gauge, "connections", "Active connections").
		Histogram(&histogram, "latency_ms", "Request latency").
		Build()

	assert.NoError(t, err)
	assert.NotNil(t, counter)
	assert.NotNil(t, gauge)
	assert.NotNil(t, int64Gauge)
	assert.NotNil(t, histogram)
}

// RED: Test Build with no metrics returns no error
func TestMetricBuilder_Build_NoMetrics(t *testing.T) {
	setupMeterProvider(t)

	builder := NewMetricBuilder("test")
	err := builder.Build()
	assert.NoError(t, err)
}

// RED: Test multiple counters
func TestMetricBuilder_MultipleCounters(t *testing.T) {
	setupMeterProvider(t)

	builder := NewMetricBuilder("test")

	var counter1 metric.Int64Counter
	var counter2 metric.Int64Counter
	var counter3 metric.Int64Counter

	builder.
		Counter(&counter1, "counter1_total", "Counter 1").
		Counter(&counter2, "counter2_total", "Counter 2").
		Counter(&counter3, "counter3_total", "Counter 3")

	err := builder.Build()
	assert.NoError(t, err)
	assert.NotNil(t, counter1)
	assert.NotNil(t, counter2)
	assert.NotNil(t, counter3)
}

// RED: Test can call Build multiple times
func TestMetricBuilder_MultipleBuildCalls(t *testing.T) {
	setupMeterProvider(t)

	builder := NewMetricBuilder("test")

	var counter metric.Int64Counter
	builder.Counter(&counter, "test_total", "Test")

	// First build
	err1 := builder.Build()
	assert.NoError(t, err1)

	// Second build (should also succeed)
	err2 := builder.Build()
	assert.NoError(t, err2)
}
