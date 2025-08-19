//go:build linux
// +build linux

package kernel

import (
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestConvertToRawEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "convert-test"}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Create test kernel event
	kernelEvent := KernelEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1234,
		TID:       1234,
		EventType: EventTypeProcess,
		Size:      64,
		CgroupID:  567890,
	}

	copy(kernelEvent.Comm[:], "test-process")
	copy(kernelEvent.PodUID[:], "test-pod-uid-1234-5678-9012")

	// Convert to RawEvent
	rawEvent := collector.convertToRawEvent(kernelEvent)

	// Verify RawEvent structure
	assert.Equal(t, collector.name, rawEvent.Source)
	assert.NotZero(t, rawEvent.Timestamp)
	assert.NotEmpty(t, rawEvent.Data)
	assert.Len(t, rawEvent.Data, int(unsafe.Sizeof(kernelEvent)))

	// Verify timestamp conversion
	expectedTime := time.Unix(0, int64(kernelEvent.Timestamp))
	assert.Equal(t, expectedTime, rawEvent.Timestamp)
}

func TestContainerInfoExtraction(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "container-test"}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	tests := []struct {
		name       string
		cgroupPath string
		expectID   string
	}{
		{
			name:       "docker container",
			cgroupPath: "/docker/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectID:   "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		},
		{
			name:       "containerd container",
			cgroupPath: "/containerd.service/123456789012345678901234567890123456789012345678901234567890",
			expectID:   "123456789012345678901234567890123456789012345678901234567890",
		},
		{
			name:       "cri-o container",
			cgroupPath: "/crio-fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321.scope",
			expectID:   "fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321",
		},
		{
			name:       "no container",
			cgroupPath: "/system.slice/some-service.service",
			expectID:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			containerID := collector.extractContainerID(tt.cgroupPath)
			assert.Equal(t, tt.expectID, containerID)
		})
	}
}

func TestPodUIDExtraction(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "pod-test"}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	tests := []struct {
		name       string
		cgroupPath string
		expectUID  string
	}{
		{
			name:       "kubernetes pod",
			cgroupPath: "/kubepods/besteffort/pod12345678-1234-1234-1234-123456789012/container",
			expectUID:  "12345678-1234-1234-1234-123456789012",
		},
		{
			name:       "kubernetes pod with underscores",
			cgroupPath: "/kubepods.slice/kubepods-besteffort.slice/kubepods-besteffort-pod87654321_4321_4321_4321_210987654321.slice",
			expectUID:  "87654321-4321-4321-4321-210987654321",
		},
		{
			name:       "no pod",
			cgroupPath: "/system.slice/docker.service",
			expectUID:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			podUID := collector.extractPodUID(tt.cgroupPath)
			assert.Equal(t, tt.expectUID, podUID)
		})
	}
}

func TestContainerRuntimeDetection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "runtime-test"}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// This test is more for coverage since it depends on filesystem
	runtime := collector.getContainerRuntime()
	assert.NotEmpty(t, runtime)
	// On most test systems, this will be "unknown" since container sockets won't exist
	assert.Contains(t, []string{"docker", "containerd", "crio", "unknown"}, runtime)
}

func TestEnrichEventWithContainerInfo(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "enrich-test"}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Test event with no pod UID (should trigger enrichment)
	event := &KernelEvent{
		PID:      1234,
		CgroupID: 567890,
	}

	// This method depends on /proc filesystem and may not work in test environment
	// but should not panic
	assert.NotPanics(t, func() {
		collector.enrichEventWithContainerInfo(event)
	})

	// Test event with existing pod UID (should skip enrichment)
	eventWithPod := &KernelEvent{
		PID:      1234,
		CgroupID: 567890,
	}
	copy(eventWithPod.PodUID[:], "existing-pod-uid")

	assert.NotPanics(t, func() {
		collector.enrichEventWithContainerInfo(eventWithPod)
	})
}

func TestGetCgroupPath(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "cgroup-test"}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Test with current process PID (should not panic)
	// This may return empty string if /proc/self/cgroup doesn't exist or is unreadable
	assert.NotPanics(t, func() {
		path := collector.getCgroupPath(uint32(1)) // init process
		// Path might be empty in test environment, but function should not panic
		_ = path
	})

	// Test with invalid PID (should return empty string)
	assert.NotPanics(t, func() {
		path := collector.getCgroupPath(uint32(999999)) // Non-existent PID
		assert.Equal(t, "", path)
	})
}

func TestLinuxSpecificEBPFIntegration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "linux-ebpf-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Test eBPF state initialization
	assert.Nil(t, collector.ebpfState) // Should be nil before start

	// Note: Actual eBPF functionality depends on kernel support and privileges
	// In most test environments, eBPF may not be available, so we just test
	// that the methods don't panic and handle errors gracefully
}

func TestEBPFStateManagement(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "ebpf-state-test"}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Test that stopEBPF handles nil state gracefully
	assert.NotPanics(t, func() {
		collector.stopEBPF()
	})

	// Test that readEBPFEvents handles nil state gracefully
	assert.NotPanics(t, func() {
		collector.readEBPFEvents()
	})
}
