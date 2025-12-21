package intelligence

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()

	m := NewMetrics(reg) // Should fail - undefined

	require.NotNil(t, m)
	assert.NotNil(t, m.EventsProcessed)
	assert.NotNil(t, m.Errors)
	assert.NotNil(t, m.ProcessingDuration)
}

func TestMetrics_Registration(t *testing.T) {
	reg := prometheus.NewRegistry()

	m := NewMetrics(reg)
	require.NotNil(t, m)

	// Use all metrics so they appear in Gather()
	m.RecordEvent("free", "network")
	m.RecordError("free", "network", "test")
	m.RecordDuration("free", "network", 0.001)

	// Verify metrics are registered
	families, err := reg.Gather()
	require.NoError(t, err)

	names := make([]string, 0, len(families))
	for _, f := range families {
		names = append(names, f.GetName())
	}

	assert.Contains(t, names, "intelligence_events_processed_total")
	assert.Contains(t, names, "intelligence_errors_total")
	assert.Contains(t, names, "intelligence_processing_duration_seconds")
}

func TestMetrics_RecordSuccess(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Record a successful event
	m.RecordEvent("free", "network")

	families, err := reg.Gather()
	require.NoError(t, err)

	// Find events_processed metric
	var found bool
	for _, f := range families {
		if f.GetName() == "intelligence_events_processed_total" {
			found = true
			require.Len(t, f.GetMetric(), 1)
			assert.Equal(t, float64(1), f.GetMetric()[0].GetCounter().GetValue())
		}
	}
	assert.True(t, found, "intelligence_events_processed_total metric not found")
}

func TestMetrics_RecordError(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Record an error
	m.RecordError("free", "network", "publish_failed")

	families, err := reg.Gather()
	require.NoError(t, err)

	// Find errors metric
	var found bool
	for _, f := range families {
		if f.GetName() == "intelligence_errors_total" {
			found = true
			require.Len(t, f.GetMetric(), 1)
			assert.Equal(t, float64(1), f.GetMetric()[0].GetCounter().GetValue())
		}
	}
	assert.True(t, found, "intelligence_errors_total metric not found")
}

func TestMetrics_RecordDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Record processing duration
	m.RecordDuration("free", "network", 0.005) // 5ms

	families, err := reg.Gather()
	require.NoError(t, err)

	// Find duration metric
	var found bool
	for _, f := range families {
		if f.GetName() == "intelligence_processing_duration_seconds" {
			found = true
			require.Len(t, f.GetMetric(), 1)
			// Histogram should have at least one observation
			assert.Greater(t, f.GetMetric()[0].GetHistogram().GetSampleCount(), uint64(0))
		}
	}
	assert.True(t, found, "intelligence_processing_duration_seconds metric not found")
}
