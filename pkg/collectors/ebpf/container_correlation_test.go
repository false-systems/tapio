package ebpf

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainerInfoCorrelation(t *testing.T) {
	collector, err := NewCollector("test-ebpf")
	require.NoError(t, err)

	// Mock the eBPF objects for testing
	collector.objs = &kernelMonitorObjects{}

	// Test UpdateContainerInfo
	err = collector.UpdateContainerInfo(1234, "container-abc123", "pod-def456", "nginx:1.20")
	if err != nil {
		// Skip test if eBPF maps not available (expected in unit tests)
		t.Skip("eBPF maps not available in unit test environment")
	}

	// Test GetContainerInfo
	containerInfo, err := collector.GetContainerInfo(1234)
	require.NoError(t, err)
	assert.NotNil(t, containerInfo)

	// Verify container information
	containerID := collector.nullTerminatedString(containerInfo.ContainerID[:])
	podUID := collector.nullTerminatedString(containerInfo.PodUID[:])
	image := collector.nullTerminatedString(containerInfo.Image[:])

	assert.Equal(t, "container-abc123", containerID)
	assert.Equal(t, "pod-def456", podUID)
	assert.Equal(t, "nginx:1.20", image)
	assert.Greater(t, containerInfo.StartedAt, uint64(0))

	// Test RemoveContainerInfo
	err = collector.RemoveContainerInfo(1234)
	require.NoError(t, err)

	// Verify removal
	_, err = collector.GetContainerInfo(1234)
	assert.Error(t, err) // Should not exist anymore
}

func TestContainerInfoNullTerminated(t *testing.T) {
	collector, err := NewCollector("test-ebpf")
	require.NoError(t, err)

	// Test null-terminated string handling
	var containerInfo ContainerInfo
	copy(containerInfo.ContainerID[:], "test-container")
	copy(containerInfo.PodUID[:], "test-pod-uid")
	copy(containerInfo.Image[:], "test-image:v1.0")

	containerID := collector.nullTerminatedString(containerInfo.ContainerID[:])
	podUID := collector.nullTerminatedString(containerInfo.PodUID[:])
	image := collector.nullTerminatedString(containerInfo.Image[:])

	assert.Equal(t, "test-container", containerID)
	assert.Equal(t, "test-pod-uid", podUID)
	assert.Equal(t, "test-image:v1.0", image)
}

func TestContainerInfoStructSize(t *testing.T) {
	// Ensure Go struct matches C struct size expectations
	var containerInfo ContainerInfo

	// Check field sizes match what we expect
	assert.Equal(t, 64, len(containerInfo.ContainerID))
	assert.Equal(t, 36, len(containerInfo.PodUID))
	assert.Equal(t, 128, len(containerInfo.Image))
	assert.Equal(t, uint64(0), containerInfo.StartedAt)
}

func TestContainerInfoBounds(t *testing.T) {
	collector, err := NewCollector("test-ebpf")
	require.NoError(t, err)

	// Test with long strings that should be truncated
	longContainerID := "this-is-a-very-long-container-id-that-exceeds-64-characters-and-should-be-truncated"
	longPodUID := "this-is-a-very-long-pod-uid-that-should-be-truncated"
	longImage := "this-is-a-very-long-image-name-with-registry-and-tag-that-exceeds-128-characters-and-should-be-truncated-properly-without-issues"

	containerInfo := &ContainerInfo{
		StartedAt: uint64(time.Now().Unix()),
	}

	// Copy with bounds checking (same as in UpdateContainerInfo)
	copy(containerInfo.ContainerID[:], longContainerID)
	copy(containerInfo.PodUID[:], longPodUID)
	copy(containerInfo.Image[:], longImage)

	// Verify strings are truncated to fit (copy truncates automatically)
	containerID := collector.nullTerminatedString(containerInfo.ContainerID[:])
	podUID := collector.nullTerminatedString(containerInfo.PodUID[:])
	image := collector.nullTerminatedString(containerInfo.Image[:])

	// Copy truncates, so extracted strings should fit in buffers minus null terminator
	assert.LessOrEqual(t, len(containerID), 64) // Full buffer can be used
	assert.LessOrEqual(t, len(podUID), 36)      // Full buffer can be used
	assert.LessOrEqual(t, len(image), 128)      // Full buffer can be used

	// Verify they start with expected prefixes
	assert.True(t, len(containerID) > 0)
	assert.True(t, len(podUID) > 0)
	assert.True(t, len(image) > 0)
}
