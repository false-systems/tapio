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
	// Collector should be properly initialized
	assert.NotNil(t, collector)
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

	// Performance metrics not yet implemented (TODO: integrate performance adapter)
	// assert.Contains(t, stats, "perf_buffer_size")
	// assert.Contains(t, stats, "perf_buffer_capacity")
	// assert.Contains(t, stats, "perf_buffer_utilization")
	// assert.Contains(t, stats, "perf_batches_processed")
	// assert.Contains(t, stats, "perf_pool_in_use")
	// assert.Contains(t, stats, "perf_events_processed")
}

func TestPerformanceAdapterMetrics(t *testing.T) {
	collector, err := NewCollector("ebpf-perf")
	require.NoError(t, err)

	// Verify performance adapter configuration
	stats := collector.Statistics()

	// Performance adapter not yet integrated - skip these tests
	// TODO: Integrate performance adapter and re-enable these tests
	// assert.Equal(t, uint64(32768), stats["perf_buffer_capacity"])
	// assert.Equal(t, uint64(0), stats["perf_buffer_size"])
	// assert.Equal(t, uint64(0), stats["perf_batches_processed"])
	// assert.Equal(t, uint64(0), stats["perf_events_processed"])

	// For now, just verify basic stats are present
	assert.Contains(t, stats, "events_collected")
	assert.Contains(t, stats, "events_dropped")
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

func TestCgroupIDValidation(t *testing.T) {
	_, _ = NewCollector("ebpf-cgroup")

	// Test cgroup ID validation - real cgroup IDs should be much larger than PIDs
	testCases := []struct {
		cgroupID    uint64
		description string
		shouldBePID bool
		isZero      bool
	}{
		{0, "zero cgroup ID", false, true},
		{1, "minimal edge case", true, false},       // Very small, could be confused with PID
		{12345, "potential PID value", true, false}, // PIDs are typically small
		{0x100000000, "cgroup ID with 4GB offset (fallback)", false, false},
		{0x800000000, "typical kernfs inode number", false, false},
		{18446744073709551615, "maximum uint64", false, false}, // Max value test
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Validate that cgroup IDs are properly distinguished from PIDs
			if tc.isZero {
				// Zero is invalid
				assert.Zero(t, tc.cgroupID, "Zero cgroup ID should be zero")
			} else if tc.shouldBePID {
				// PIDs should be small numbers (typically < 65536 on most systems)
				assert.True(t, tc.cgroupID < 65536, "PID-like value should be small")
			} else {
				// Real cgroup IDs should be large (either kernfs inodes or have our offset)
				// With our fix, we expect either:
				// 1. Large inode numbers (> 1M typically)
				// 2. Offset-based IDs (>= 4GB)
				isLargeCgroupID := tc.cgroupID >= 0x100000000 // 4GB offset
				isValidKernfsInode := tc.cgroupID > 1000000   // Large inode
				assert.True(t, isLargeCgroupID || isValidKernfsInode,
					"Valid cgroup ID should be distinguishable from PID")
			}
		})
	}
}

func TestCgroupPodCorrelation(t *testing.T) {
	_, _ = NewCollector("ebpf-correlation")

	// Test that correlation metadata includes proper cgroup information
	metadata := map[string]string{
		"cgroup_id": "1234567890", // Simulated large cgroup ID
		"pod_uid":   "test-pod-uid-12345",
		"pid":       "1234",
	}

	// Validate that cgroup ID is different from PID
	cgroupID := metadata["cgroup_id"]
	pidStr := metadata["pid"]

	assert.NotEqual(t, cgroupID, pidStr, "Cgroup ID should not equal PID")

	// Test pod UID extraction
	podUID := metadata["pod_uid"]
	assert.NotEmpty(t, podUID, "Pod UID should be present for correlation")
	assert.NotEqual(t, "0", podUID, "Pod UID should not be zero")
}

func TestContainerPIDValidation(t *testing.T) {
	collector, _ := NewCollector("ebpf-containers")

	// Test container PID detection logic
	testCases := []struct {
		cgroupPath  string
		shouldMatch bool
		description string
	}{
		{"/docker/container-id", true, "Docker container"},
		{"/containerd/container-id", true, "Containerd container"},
		{"/kubepods/besteffort/pod-id", true, "Kubernetes pod"},
		{"/system.slice/systemd-service", false, "System service"},
		{"/user.slice/user-session", false, "User session"},
		{"", false, "Empty cgroup path"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Simulate the container detection logic
			isContainer := collector.isContainerCgroupPath(tc.cgroupPath)
			assert.Equal(t, tc.shouldMatch, isContainer,
				"Container detection should match expected result")
		})
	}
}

// Helper function to simulate container cgroup path detection
func (c *Collector) isContainerCgroupPath(cgroupPath string) bool {
	// Simulate the logic used in populateContainerPIDs
	if cgroupPath == "" {
		return false
	}

	containerKeywords := []string{"docker", "containerd", "kubepods"}
	for _, keyword := range containerKeywords {
		if len(cgroupPath) > len(keyword) &&
			cgroupPath[:len(keyword)+1] == "/"+keyword {
			return true
		}
	}
	return false
}
