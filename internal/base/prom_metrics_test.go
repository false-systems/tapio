package base

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewPromObserverMetrics verifies metrics creation
func TestNewPromObserverMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := NewPromObserverMetrics(reg)

	require.NotNil(t, metrics)
	assert.NotNil(t, metrics.EventsProcessed)
	assert.NotNil(t, metrics.EventsDropped)
	assert.NotNil(t, metrics.ErrorsTotal)
	assert.NotNil(t, metrics.ProcessingTime)
}

// TestPromObserverMetrics_RecordEvent verifies event recording
func TestPromObserverMetrics_RecordEvent(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := NewPromObserverMetrics(reg)

	// Should not panic
	metrics.RecordEvent("network", "connection")
	metrics.RecordEvent("container", "oom_kill")

	// Verify metrics were recorded
	gathered, err := reg.Gather()
	require.NoError(t, err)

	found := false
	for _, m := range gathered {
		if m.GetName() == "tapio_observer_events_processed_total" {
			found = true
			break
		}
	}
	assert.True(t, found, "events_processed metric should exist")
}

// TestPromObserverMetrics_RecordError verifies error recording
func TestPromObserverMetrics_RecordError(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := NewPromObserverMetrics(reg)

	metrics.RecordError("network", "connection", "timeout")

	gathered, err := reg.Gather()
	require.NoError(t, err)

	found := false
	for _, m := range gathered {
		if m.GetName() == "tapio_observer_errors_total" {
			found = true
			break
		}
	}
	assert.True(t, found, "errors_total metric should exist")
}

// TestPromObserverMetrics_RecordDrop verifies drop recording
func TestPromObserverMetrics_RecordDrop(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := NewPromObserverMetrics(reg)

	metrics.RecordDrop("container", "exit")

	gathered, err := reg.Gather()
	require.NoError(t, err)

	found := false
	for _, m := range gathered {
		if m.GetName() == "tapio_observer_events_dropped_total" {
			found = true
			break
		}
	}
	assert.True(t, found, "events_dropped metric should exist")
}

// TestEventDomainFromType verifies domain extraction
func TestEventDomainFromType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"network.connection", "network"},
		{"container.oom", "container"},
		{"node.pressure", "node"},
		{"simple", "simple"},
		{"", ""},
	}

	for _, tc := range tests {
		result := EventDomainFromType(tc.input)
		assert.Equal(t, tc.expected, result, "EventDomainFromType(%q)", tc.input)
	}
}

// TestPromObserverMetrics_PipelineMetrics verifies pipeline health metrics
func TestPromObserverMetrics_PipelineMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := NewPromObserverMetrics(reg)

	// Record pipeline metrics
	metrics.RecordPipelineQueueDepth("network", 50)
	metrics.RecordPipelineQueueUtilization("network", 0.75)
	metrics.RecordPipelineStagesActive("network", 3)
	metrics.RecordPipelineStageFailed("network", "processor")

	// Verify all metrics exist
	gathered, err := reg.Gather()
	require.NoError(t, err)

	expectedMetrics := []string{
		"tapio_observer_pipeline_queue_depth",
		"tapio_observer_pipeline_queue_utilization_ratio",
		"tapio_observer_pipeline_stages_active",
		"tapio_observer_pipeline_stages_failed_total",
	}

	for _, expected := range expectedMetrics {
		found := false
		for _, m := range gathered {
			if m.GetName() == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "metric %s should exist", expected)
	}
}

// TestPromObserverMetrics_EBPFMetrics verifies eBPF health metrics
func TestPromObserverMetrics_EBPFMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := NewPromObserverMetrics(reg)

	metrics.RecordEBPFMapSize("network", 1000)
	metrics.RecordEBPFMapCapacity("network", 10000)
	metrics.RecordEBPFRingBufferLost("network", 5)
	metrics.RecordEBPFRingBufferUtilization("network", 0.3)

	gathered, err := reg.Gather()
	require.NoError(t, err)

	expectedMetrics := []string{
		"tapio_observer_ebpf_map_entries",
		"tapio_observer_ebpf_map_capacity",
		"tapio_observer_ebpf_ringbuffer_lost_total",
		"tapio_observer_ebpf_ringbuffer_utilization_ratio",
	}

	for _, expected := range expectedMetrics {
		found := false
		for _, m := range gathered {
			if m.GetName() == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "metric %s should exist", expected)
	}
}
