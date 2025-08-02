//go:build integration
// +build integration

package ebpf

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContainerCorrelationIntegration tests the full container correlation flow
// Run with: go test -tags=integration -v
func TestContainerCorrelationIntegration(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Integration test requires root privileges")
	}

	// Create collector
	collector, err := NewCollector("test-ebpf")
	require.NoError(t, err)

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Simulate a container process
	testPID := uint32(os.Getpid())
	testContainerID := "test-container-12345"
	testPodUID := "test-pod-67890"
	testImage := "test-app:v1.0"

	// Update container info
	err = collector.UpdateContainerInfo(testPID, testContainerID, testPodUID, testImage)
	require.NoError(t, err)

	// Also update pod info for the current cgroup
	// In real scenario, this would come from K8s API
	cgroupID := uint64(testPID) // Simplified for test
	err = collector.UpdatePodInfo(cgroupID, testPodUID, "default", "test-pod")
	require.NoError(t, err)

	// Wait for some events to be generated
	time.Sleep(2 * time.Second)

	// Collect events
	events := make([]interface{}, 0)
	eventChan := collector.Events()

	// Collect events for a short period
	timeout := time.After(3 * time.Second)
loop:
	for {
		select {
		case event := <-eventChan:
			events = append(events, event)
			// Check if this event has our container correlation
			if event.Metadata["pid"] == fmt.Sprintf("%d", testPID) {
				// Verify container information is present
				assert.Equal(t, testContainerID, event.Metadata["container_id"])
				assert.Equal(t, testImage, event.Metadata["container_image"])
				assert.NotEmpty(t, event.Metadata["container_started_at"])

				// Also check pod correlation
				assert.Equal(t, testPodUID, event.Metadata["pod_uid"])

				t.Logf("Found correlated event: PID=%s, Container=%s, Pod=%s",
					event.Metadata["pid"],
					event.Metadata["container_id"],
					event.Metadata["pod_uid"])
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	// We should have collected some events
	assert.NotEmpty(t, events, "Should have collected at least one event")

	// Clean up
	err = collector.RemoveContainerInfo(testPID)
	assert.NoError(t, err)

	err = collector.RemovePodInfo(cgroupID)
	assert.NoError(t, err)
}

// TestContainerLifecycle simulates a container lifecycle
func TestContainerLifecycle(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Integration test requires root privileges")
	}

	collector, err := NewCollector("test-lifecycle")
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Simulate container start
	containerPID := uint32(12345)
	containerID := "nginx-abc123"
	podUID := "web-pod-def456"
	image := "nginx:1.20-alpine"

	// Container starts - update correlation
	err = collector.UpdateContainerInfo(containerPID, containerID, podUID, image)
	assert.NoError(t, err)

	// Verify we can retrieve the info
	info, err := collector.GetContainerInfo(containerPID)
	assert.NoError(t, err)
	assert.NotNil(t, info)

	// Container stops - remove correlation
	err = collector.RemoveContainerInfo(containerPID)
	assert.NoError(t, err)

	// Verify info is gone
	_, err = collector.GetContainerInfo(containerPID)
	assert.Error(t, err)
}

// TestMultipleContainers tests correlation with multiple containers
func TestMultipleContainers(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Integration test requires root privileges")
	}

	collector, err := NewCollector("test-multi")
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Add multiple containers
	containers := []struct {
		pid         uint32
		containerID string
		podUID      string
		image       string
	}{
		{1001, "app-1", "pod-1", "myapp:v1"},
		{1002, "app-2", "pod-1", "myapp:v1"},
		{2001, "db-1", "pod-2", "postgres:13"},
		{3001, "cache-1", "pod-3", "redis:6.2"},
	}

	// Add all containers
	for _, c := range containers {
		err := collector.UpdateContainerInfo(c.pid, c.containerID, c.podUID, c.image)
		assert.NoError(t, err)
	}

	// Verify all containers
	for _, c := range containers {
		info, err := collector.GetContainerInfo(c.pid)
		assert.NoError(t, err)
		assert.Equal(t, c.containerID, collector.nullTerminatedString(info.ContainerID[:]))
		assert.Equal(t, c.podUID, collector.nullTerminatedString(info.PodUID[:]))
		assert.Equal(t, c.image, collector.nullTerminatedString(info.Image[:]))
	}

	// Remove some containers
	err = collector.RemoveContainerInfo(1001)
	assert.NoError(t, err)
	err = collector.RemoveContainerInfo(2001)
	assert.NoError(t, err)

	// Verify removed
	_, err = collector.GetContainerInfo(1001)
	assert.Error(t, err)
	_, err = collector.GetContainerInfo(2001)
	assert.Error(t, err)

	// Others should still exist
	_, err = collector.GetContainerInfo(1002)
	assert.NoError(t, err)
	_, err = collector.GetContainerInfo(3001)
	assert.NoError(t, err)
}
