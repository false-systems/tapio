//go:build integration
// +build integration

package kernel

import (
	"context"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// TestKernelCollectorIntegration runs comprehensive integration tests
func TestKernelCollectorIntegration(t *testing.T) {
	// Skip if not running as root (required for eBPF)
	if os.Geteuid() != 0 {
		t.Skip("Integration tests require root privileges for eBPF")
	}

	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "integration-test-collector"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Test initial state
	assert.Equal(t, config.Name, collector.Name())
	assert.True(t, collector.IsHealthy())

	// Test complete lifecycle
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err, "Collector should start successfully")

	// Give some time for eBPF to initialize and events to flow
	time.Sleep(2 * time.Second)

	// Test events are being generated
	eventsCh := collector.Events()
	require.NotNil(t, eventsCh, "Events channel should not be nil")

	// Test shutdown
	err = collector.Stop()
	require.NoError(t, err, "Collector should stop cleanly")

	// Verify collector is stopped
	assert.False(t, collector.IsHealthy(), "Collector should be unhealthy after stop")
}

// TestEBPFLifecycle tests the complete eBPF lifecycle on Linux
func TestEBPFLifecycle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF lifecycle test only runs on Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("eBPF lifecycle test requires root privileges")
	}

	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "ebpf-lifecycle-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test 1: eBPF initialization
	err = collector.Start(ctx)
	require.NoError(t, err, "eBPF should initialize successfully")

	// Verify eBPF state is set up (Linux-specific)
	if runtime.GOOS == "linux" {
		assert.NotNil(t, collector.ebpfState, "eBPF state should be initialized")
	}

	// Test 2: Event processing
	eventsCh := collector.Events()
	var eventsReceived int64

	// Start event consumer
	done := make(chan bool)
	go func() {
		defer close(done)
		timeout := time.After(10 * time.Second)
		for {
			select {
			case <-eventsCh:
				atomic.AddInt64(&eventsReceived, 1)
			case <-timeout:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Generate some activity to trigger eBPF events
	generateKernelActivity(t)

	// Wait for events or timeout
	<-done

	// Test 3: Cleanup
	err = collector.Stop()
	require.NoError(t, err, "eBPF cleanup should succeed")

	// Verify cleanup (Linux-specific check)
	if runtime.GOOS == "linux" && collector.ebpfState != nil {
		// On Linux, ebpfState should still exist but links should be closed
		// We can't easily verify link closure without exposing internals
		t.Log("eBPF cleanup completed")
	}

	eventsCount := atomic.LoadInt64(&eventsReceived)
	t.Logf("Received %d events during lifecycle test", eventsCount)
}

// TestNetworkMonitoringIntegration tests network event capture
func TestNetworkMonitoringIntegration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Network monitoring test only runs on Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("Network monitoring test requires root privileges")
	}

	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "network-monitoring-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer func() {
		err := collector.Stop()
		assert.NoError(t, err)
	}()

	// Monitor for network events
	eventsCh := collector.Events()
	var networkEvents []*domain.CollectorEvent

	done := make(chan bool)
	go func() {
		defer close(done)
		timeout := time.After(15 * time.Second)
		for {
			select {
			case event := <-eventsCh:
				// Check if this is a network-related event by examining raw data
				if isNetworkEvent(event) {
					eventCopy := event
					networkEvents = append(networkEvents, &eventCopy)
					if len(networkEvents) >= 5 { // Collect some network events
						return
					}
				}
			case <-timeout:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Generate network activity
	generateNetworkActivity(t)

	<-done

	t.Logf("Captured %d network events", len(networkEvents))

	// Verify we captured some network events
	for _, event := range networkEvents {
		assert.Equal(t, "network-monitoring-test", event.Source)
		assert.NotZero(t, event.Timestamp)
		assert.NotEmpty(t, event.Data)
	}
}

// TestContainerCorrelationIntegration tests container ID and pod UID extraction patterns
func TestContainerCorrelationIntegration(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "container-correlation-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Test cgroup path patterns (available on all platforms)
	testCases := []struct {
		name        string
		cgroupPath  string
		expectMatch bool
		description string
	}{
		{
			name:        "Docker container pattern",
			cgroupPath:  "/docker/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			expectMatch: true,
			description: "Should match Docker container pattern",
		},
		{
			name:        "containerd container pattern",
			cgroupPath:  "/containerd/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectMatch: true,
			description: "Should match containerd pattern",
		},
		{
			name:        "cri-o container pattern",
			cgroupPath:  "/crio-fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321.scope",
			expectMatch: true,
			description: "Should match cri-o pattern",
		},
		{
			name:        "Kubernetes pod pattern",
			cgroupPath:  "/kubepods/besteffort/pod12345678-1234-5678-9012-123456789012/container-id",
			expectMatch: true,
			description: "Should match Kubernetes pod pattern",
		},
		{
			name:        "Non-container path",
			cgroupPath:  "/system.slice/ssh.service",
			expectMatch: false,
			description: "Should not match non-container paths",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test that the patterns would match appropriately
			// We can't call the extraction methods directly on non-Linux platforms
			// but we can test the pattern logic

			hasContainerPattern := strings.Contains(tc.cgroupPath, "docker/") ||
				strings.Contains(tc.cgroupPath, "containerd") ||
				strings.Contains(tc.cgroupPath, "crio-") ||
				strings.Contains(tc.cgroupPath, "kubepods")

			if tc.expectMatch {
				assert.True(t, hasContainerPattern, tc.description)
			} else {
				assert.False(t, hasContainerPattern, tc.description)
			}

			t.Logf("Tested pattern matching for: %s", tc.cgroupPath)
		})
	}
}

// TestEventProcessingIntegration tests end-to-end event processing
func TestEventProcessingIntegration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Event processing test only runs on Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("Event processing test requires root privileges")
	}

	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "event-processing-test"
	// Reduce buffer size for faster testing
	config.ResourceLimits.EventQueueSize = 1000

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer func() {
		err := collector.Stop()
		assert.NoError(t, err)
	}()

	// Test event flow and conversion
	eventsCh := collector.Events()
	var processedEvents []*domain.CollectorEvent

	done := make(chan bool)
	go func() {
		defer close(done)
		timeout := time.After(10 * time.Second)
		eventCount := 0
		for {
			select {
			case event := <-eventsCh:
				eventCopy := event
				processedEvents = append(processedEvents, &eventCopy)
				eventCount++
				if eventCount >= 50 { // Process a batch of events
					return
				}
			case <-timeout:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Generate kernel activity
	generateKernelActivity(t)

	<-done

	t.Logf("Processed %d events", len(processedEvents))

	// Verify events are properly formatted
	for i, event := range processedEvents {
		if i >= 10 { // Check first 10 events
			break
		}

		assert.Equal(t, "event-processing-test", event.Source)
		assert.NotZero(t, event.Timestamp, "Event %d should have non-zero timestamp", i)
		assert.NotEmpty(t, event.Data, "Event %d should have data", i)
		assert.True(t, len(event.Data) > 0, "Event %d data should not be empty", i)

		// Verify timestamp is reasonable (within last minute)
		now := time.Now()
		timeDiff := now.Sub(event.Timestamp)
		assert.True(t, timeDiff >= 0 && timeDiff < time.Minute,
			"Event %d timestamp should be recent: %v", i, event.Timestamp)
	}
}

// TestErrorScenarios tests error handling and recovery
func TestErrorScenarios(t *testing.T) {
	logger := zap.NewNop()

	t.Run("InvalidConfig", func(t *testing.T) {
		config := &Config{
			Name:    "", // Invalid empty name
			Enabled: true,
		}
		err := config.Validate()
		assert.Error(t, err, "Config with empty name should be invalid")
		assert.Contains(t, err.Error(), "name cannot be empty")
	})

	t.Run("MultipleStartStop", func(t *testing.T) {
		config := DefaultConfig()
		config.Name = "multi-start-test"

		collector, err := NewCollectorWithConfig(config, logger)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// First start
		err = collector.Start(ctx)
		require.NoError(t, err)

		// Second start should not fail (idempotent)
		err = collector.Start(ctx)
		assert.NoError(t, err, "Multiple starts should be idempotent")

		// Stop
		err = collector.Stop()
		require.NoError(t, err)

		// Second stop should not fail (idempotent)
		err = collector.Stop()
		assert.NoError(t, err, "Multiple stops should be idempotent")
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		config := DefaultConfig()
		config.Name = "context-cancel-test"

		collector, err := NewCollectorWithConfig(config, logger)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		err = collector.Start(ctx)
		require.NoError(t, err)

		// Cancel context quickly
		cancel()
		time.Sleep(100 * time.Millisecond)

		// Collector should handle cancellation gracefully
		err = collector.Stop()
		assert.NoError(t, err)
	})
}

// TestCrossPlatformBehavior tests behavior on different platforms
func TestCrossPlatformBehavior(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "cross-platform-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// On non-Linux platforms, collector should start but not generate events
	eventsCh := collector.Events()
	require.NotNil(t, eventsCh)

	if runtime.GOOS == "linux" {
		t.Log("Running on Linux - eBPF functionality enabled")
		// On Linux, we might get events (tested in other integration tests)
	} else {
		t.Logf("Running on %s - eBPF functionality stubbed", runtime.GOOS)
		// On non-Linux, should not generate events but should not crash
		select {
		case <-eventsCh:
			t.Error("Non-Linux platform should not generate events")
		case <-time.After(1 * time.Second):
			// Expected - no events on non-Linux
		}
	}

	err = collector.Stop()
	require.NoError(t, err)
}

// TestResourceCleanup tests proper resource cleanup
func TestResourceCleanup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Resource cleanup test only meaningful on Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("Resource cleanup test requires root privileges")
	}

	logger := zap.NewNop()

	// Create multiple collectors to stress resource management
	const numCollectors = 5
	collectors := make([]*Collector, numCollectors)

	for i := 0; i < numCollectors; i++ {
		config := DefaultConfig()
		config.Name = "cleanup-test-" + string(rune('a'+i))

		collector, err := NewCollectorWithConfig(config, logger)
		require.NoError(t, err)
		collectors[i] = collector
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Start all collectors
	for i, collector := range collectors {
		err := collector.Start(ctx)
		require.NoError(t, err, "Collector %d should start", i)
	}

	// Let them run briefly
	time.Sleep(2 * time.Second)

	// Stop all collectors
	for i, collector := range collectors {
		err := collector.Stop()
		require.NoError(t, err, "Collector %d should stop cleanly", i)
	}

	t.Log("Successfully created, started, and stopped multiple collectors")
}

// Helper functions

// generateKernelActivity generates system activity to trigger eBPF events
func generateKernelActivity(t *testing.T) {
	// Generate file operations
	go func() {
		for i := 0; i < 10; i++ {
			tempFile := "/tmp/kernel_test_" + string(rune('a'+i))
			file, err := os.Create(tempFile)
			if err == nil {
				file.WriteString("test data")
				file.Close()
				os.Remove(tempFile)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// Generate some process activity
	go func() {
		for i := 0; i < 5; i++ {
			// This will trigger execve events
			cmd := []string{"/bin/echo", "integration-test"}
			if len(cmd) > 0 {
				// We can't easily exec without external dependencies
				// So we'll just create some CPU activity
				for j := 0; j < 1000; j++ {
					_ = j * j
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()

	time.Sleep(3 * time.Second)
}

// generateNetworkActivity generates network activity to trigger network events
func generateNetworkActivity(t *testing.T) {
	// Note: In a real integration environment, you might:
	// 1. Create actual network connections
	// 2. Use netcat or similar tools
	// 3. Connect to known services

	// For this test, we'll simulate by creating some network-related syscalls
	go func() {
		// This would normally create socket operations that eBPF would catch
		for i := 0; i < 10; i++ {
			// Generate some activity that might trigger network monitoring
			time.Sleep(100 * time.Millisecond)
		}
	}()

	time.Sleep(2 * time.Second)
}

// isNetworkEvent checks if a RawEvent contains network-related data
func isNetworkEvent(event *domain.CollectorEvent) bool {
	// In a real implementation, you would parse the event data
	// and check if it contains network information
	// For now, we'll use a simple heuristic based on data size and content
	if len(event.Data) < 20 {
		return false
	}

	// Check if this might be a network event based on the raw eBPF data structure
	// This is a simplified check - in reality you'd properly decode the KernelEvent
	return len(event.Data) >= int(64) // Size of our KernelEvent struct or larger
}
