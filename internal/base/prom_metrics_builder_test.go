//go:build linux

package base

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPromMetricBuilder_Counter creates a counter metric
func TestPromMetricBuilder_Counter(t *testing.T) {
	reg := prometheus.NewRegistry()

	var counter *prometheus.Counter
	err := NewPromMetricBuilder(reg, "test").
		Counter(&counter, "requests_total", "Total requests").
		Build()

	require.NoError(t, err)
	require.NotNil(t, counter)

	// Verify metric is registered
	gathered, err := reg.Gather()
	require.NoError(t, err)

	found := false
	for _, m := range gathered {
		if m.GetName() == "tapio_test_requests_total" {
			found = true
			break
		}
	}
	assert.True(t, found, "counter should be registered with prefixed name")
}

// TestPromMetricBuilder_CounterVec creates a counter vector
func TestPromMetricBuilder_CounterVec(t *testing.T) {
	reg := prometheus.NewRegistry()

	var counterVec *prometheus.CounterVec
	err := NewPromMetricBuilder(reg, "network").
		CounterVec(&counterVec, "packets_total", "Total packets", []string{"direction"}).
		Build()

	require.NoError(t, err)
	require.NotNil(t, counterVec)

	// Use the counter vec
	counterVec.WithLabelValues("inbound").Inc()

	gathered, err := reg.Gather()
	require.NoError(t, err)

	found := false
	for _, m := range gathered {
		if m.GetName() == "tapio_network_packets_total" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestPromMetricBuilder_Gauge creates a gauge metric
func TestPromMetricBuilder_Gauge(t *testing.T) {
	reg := prometheus.NewRegistry()

	var gauge *prometheus.Gauge
	err := NewPromMetricBuilder(reg, "container").
		Gauge(&gauge, "memory_bytes", "Memory usage in bytes").
		Build()

	require.NoError(t, err)
	require.NotNil(t, gauge)
}

// TestPromMetricBuilder_GaugeVec creates a gauge vector
func TestPromMetricBuilder_GaugeVec(t *testing.T) {
	reg := prometheus.NewRegistry()

	var gaugeVec *prometheus.GaugeVec
	err := NewPromMetricBuilder(reg, "node").
		GaugeVec(&gaugeVec, "cpu_usage", "CPU usage", []string{"core"}).
		Build()

	require.NoError(t, err)
	require.NotNil(t, gaugeVec)

	gaugeVec.WithLabelValues("0").Set(0.5)
}

// TestPromMetricBuilder_Histogram creates a histogram metric
func TestPromMetricBuilder_Histogram(t *testing.T) {
	reg := prometheus.NewRegistry()

	var histogram *prometheus.Histogram
	buckets := []float64{0.1, 0.5, 1, 5, 10}
	err := NewPromMetricBuilder(reg, "scheduler").
		Histogram(&histogram, "latency_ms", "Latency in ms", buckets).
		Build()

	require.NoError(t, err)
	require.NotNil(t, histogram)
}

// TestPromMetricBuilder_Chaining verifies fluent API
func TestPromMetricBuilder_Chaining(t *testing.T) {
	reg := prometheus.NewRegistry()

	var counter *prometheus.Counter
	var gauge *prometheus.Gauge

	err := NewPromMetricBuilder(reg, "api").
		Counter(&counter, "calls_total", "Total API calls").
		Gauge(&gauge, "active_connections", "Active connections").
		Build()

	require.NoError(t, err)
	require.NotNil(t, counter)
	require.NotNil(t, gauge)
}
