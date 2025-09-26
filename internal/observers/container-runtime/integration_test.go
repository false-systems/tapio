//go:build linux && integration
// +build linux,integration

package containerruntime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContainerRuntimeIntegration tests real container discovery and eBPF integration
func TestContainerRuntimeIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create observer with real configuration
	config := NewDefaultConfig("integration-test")
	config.EnableOOMKill = true
	config.EnableMemoryPressure = true
	config.EnableProcessExit = true

	observer, err := NewObserver("integration-test", config)
	require.NoError(t, err, "Should create observer")
	require.NotNil(t, observer, "Observer should not be nil")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start observer (this will initialize eBPF and Docker client)
	err = observer.Start(ctx)
	require.NoError(t, err, "Should start observer successfully")

	// Verify observer is healthy
	assert.True(t, observer.IsHealthy(), "Observer should be healthy")

	// Get event channel
	events := observer.Events()
	require.NotNil(t, events, "Event channel should not be nil")

	// Wait for initial container discovery
	time.Sleep(2 * time.Second)

	// Check if any containers were discovered
	stats := observer.Statistics()
	t.Logf("Initial stats - Events processed: %d, Errors: %d",
		stats.EventsProcessed, stats.ErrorCount)

	// Run for a short period to capture any existing events
	deadline := time.Now().Add(10 * time.Second)
	eventCount := 0

	for time.Now().Before(deadline) {
		select {
		case event := <-events:
			eventCount++
			t.Logf("Captured event: Type=%s, Source=%s, ContainerID=%s",
				event.Type, event.Source,
				func() string {
					if event.CorrelationHints != nil {
						return event.CorrelationHints.ContainerID
					}
					return "unknown"
				}())

		case <-time.After(1 * time.Second):
			// No events in the last second, continue
		}
	}

	// Final statistics
	finalStats := observer.Statistics()
	t.Logf("Final stats - Events processed: %d, Errors: %d, Events captured: %d",
		finalStats.EventsProcessed, finalStats.ErrorCount, eventCount)

	// Stop observer
	err = observer.Stop()
	assert.NoError(t, err, "Should stop observer cleanly")

	// Test passes if:
	// 1. Observer starts without errors
	// 2. Observer reports as healthy
	// 3. No critical errors occur
	assert.Equal(t, int64(0), finalStats.ErrorCount, "Should have no errors")
}

// TestDockerClientIntegration tests Docker client connectivity
func TestDockerClientIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client, err := AutoDetectRuntime()
	if err != nil {
		t.Skipf("No container runtime detected: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test container listing
	containers, err := client.ListContainers(ctx)
	require.NoError(t, err, "Should list containers successfully")

	t.Logf("Found %d running containers", len(containers))
	for _, container := range containers {
		t.Logf("Container: ID=%s, PID=%d, CgroupID=%d, Runtime=%s",
			container.ID, container.PID, container.CgroupID, container.Runtime)
	}

	// Test event watching (just verify channel is created)
	eventCh, err := client.WatchEvents(ctx)
	require.NoError(t, err, "Should create event channel")
	require.NotNil(t, eventCh, "Event channel should not be nil")

	// Don't wait for events in this test, just verify the setup works
}
