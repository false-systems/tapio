package ebpf

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectorCreation(t *testing.T) {
	collector, err := NewCollector("ebpf-test")
	require.NoError(t, err)
	
	assert.Equal(t, "ebpf-test", collector.Name())
	assert.True(t, collector.IsHealthy())
	assert.NotNil(t, collector.perfAdapter, "Performance adapter should be initialized")
}

func TestCollectorLifecycle(t *testing.T) {
	collector, err := NewCollector("ebpf-lifecycle")
	require.NoError(t, err)

	// Test that events channel is available
	events := collector.Events()
	assert.NotNil(t, events, "Events channel should not be nil")

	// Test stop without start
	err = collector.Stop()
	assert.NoError(t, err, "Stop should not fail even if not started")
}

func TestEventChannelCapacity(t *testing.T) {
	collector, err := NewCollector("ebpf-events")
	require.NoError(t, err)

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Note: This will likely fail in test environment without proper eBPF setup
	// but we test the basic functionality
	err = collector.Start(ctx)
	if err != nil {
		t.Logf("Expected error in test environment: %v", err)
		return // Skip rest of test in environments without eBPF support
	}

	// If start succeeded, test cleanup
	defer collector.Stop()

	// Test that collector reports as healthy
	assert.True(t, collector.IsHealthy(), "Collector should be healthy after successful start")
}

func TestCollectorHealthAndStatistics(t *testing.T) {
	collector, err := NewCollector("ebpf-stats")
	require.NoError(t, err)

	// Test initial health
	healthy, details := collector.Health()
	assert.True(t, healthy)
	assert.Contains(t, details, "healthy")
	assert.Contains(t, details, "events_collected")
	assert.Contains(t, details, "events_dropped")
	assert.Contains(t, details, "error_count")
	assert.Contains(t, details, "ebpf_loaded")
	assert.Contains(t, details, "links_count")

	// Test statistics
	stats := collector.Statistics()
	assert.Contains(t, stats, "events_collected")
	assert.Contains(t, stats, "events_dropped")
	assert.Contains(t, stats, "error_count")
	assert.Contains(t, stats, "last_event_time")
	assert.Contains(t, stats, "pod_trace_count")
	
	// Performance metrics should be present
	assert.Contains(t, stats, "perf_buffer_size")
	assert.Contains(t, stats, "perf_buffer_capacity")
	assert.Contains(t, stats, "perf_buffer_utilization")
	assert.Contains(t, stats, "perf_batches_processed")
	assert.Contains(t, stats, "perf_pool_in_use")
	assert.Contains(t, stats, "perf_events_processed")
}

func TestPerformanceAdapterMetrics(t *testing.T) {
	collector, err := NewCollector("ebpf-perf")
	require.NoError(t, err)

	// Verify performance adapter configuration
	stats := collector.Statistics()
	
	// Check buffer capacity is as configured (32768)
	assert.Equal(t, uint64(32768), stats["perf_buffer_capacity"])
	
	// Initial metrics should be zero
	assert.Equal(t, uint64(0), stats["perf_buffer_size"])
	assert.Equal(t, uint64(0), stats["perf_batches_processed"])
	assert.Equal(t, uint64(0), stats["perf_events_processed"])
}

func TestEventTypeToString(t *testing.T) {
	collector, _ := NewCollector("ebpf-types")

	testCases := []struct {
		eventType uint32
		expected  string
	}{
		{1, "memory_alloc"},
		{2, "memory_free"},
		{3, "process_exec"},
		{4, "pod_syscall"},
		{5, "network_conn"},
		{99, "unknown"},
	}

	for _, tc := range testCases {
		result := collector.eventTypeToString(tc.eventType)
		if result != tc.expected {
			t.Errorf("For event type %d, expected '%s', got '%s'", tc.eventType, tc.expected, result)
		}
	}
}

func TestNullTerminatedString(t *testing.T) {
	collector, _ := NewCollector("ebpf-strings")

	testCases := []struct {
		input    []byte
		expected string
	}{
		{[]byte{'h', 'e', 'l', 'l', 'o', 0, 'w', 'o', 'r', 'l', 'd'}, "hello"},
		{[]byte{'t', 'e', 's', 't', 0}, "test"},
		{[]byte{0, 'a', 'b', 'c'}, ""},
		{[]byte{'n', 'o', 'n', 'u', 'l', 'l'}, "nonull"},
	}

	for _, tc := range testCases {
		result := collector.nullTerminatedString(tc.input)
		if result != tc.expected {
			t.Errorf("For input %v, expected '%s', got '%s'", tc.input, tc.expected, result)
		}
	}
}

func TestPodManagement(t *testing.T) {
	collector, _ := NewCollector("ebpf-pod")

	// Test UpdatePodInfo with uninitialized eBPF objects (should fail gracefully)
	err := collector.UpdatePodInfo(12345, "pod-123", "default", "nginx-pod")
	if err == nil {
		t.Error("Expected error when eBPF objects not initialized")
	}

	// Test RemovePodInfo with uninitialized eBPF objects (should fail gracefully)
	err = collector.RemovePodInfo(12345)
	if err == nil {
		t.Error("Expected error when eBPF objects not initialized")
	}

	// Test GetPodInfo with uninitialized eBPF objects (should fail gracefully)
	_, err = collector.GetPodInfo(12345)
	if err == nil {
		t.Error("Expected error when eBPF objects not initialized")
	}
}
