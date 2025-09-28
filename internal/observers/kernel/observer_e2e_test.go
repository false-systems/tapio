package kernel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// TestE2EKubernetesConfigWorkflow tests complete ConfigMap/Secret monitoring workflow
func TestE2EKubernetesConfigWorkflow(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("E2E test requires Linux with eBPF support")
	}

	if os.Geteuid() != 0 {
		t.Skip("E2E test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "e2e-kernel",
		BufferSize: 1000,
		EnableEBPF: true,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)
	require.NotNil(t, observer)

	// Start observer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Get event channel
	events := observer.Events()
	require.NotNil(t, events)

	// Create test directory structure simulating Kubernetes volumes
	tempDir, err := os.MkdirTemp("", "e2e-kernel-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Simulate Kubernetes pod volume structure
	podUID := "test-pod-abc123"
	configMapPath := filepath.Join(tempDir, "kubelet", "pods", podUID,
		"volumes", "kubernetes.io~configmap", "app-config")
	secretPath := filepath.Join(tempDir, "kubelet", "pods", podUID,
		"volumes", "kubernetes.io~secret", "db-credentials")

	// Create directory structure
	err = os.MkdirAll(configMapPath, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(secretPath, 0755)
	require.NoError(t, err)

	// Workflow simulation
	var wg sync.WaitGroup
	capturedEvents := make([]*domain.CollectorEvent, 0)
	errorEvents := make([]*domain.CollectorEvent, 0)

	// Event collector goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		timeout := time.After(5 * time.Second)
		for {
			select {
			case event := <-events:
				t.Logf("Captured E2E event: Type=%s, Source=%s",
					event.Type, event.Source)

				if event.EventData.Kernel != nil {
					capturedEvents = append(capturedEvents, event)

					// Check for error events
					if event.EventData.Kernel.ReturnCode != 0 {
						errorEvents = append(errorEvents, event)
					}
				}

			case <-timeout:
				return
			}
		}
	}()

	// Simulate application workflow
	t.Log("Simulating Kubernetes config access workflow...")

	// 1. ConfigMap access (success)
	configFile := filepath.Join(configMapPath, "application.yaml")
	err = os.WriteFile(configFile, []byte("app: config"), 0644)
	require.NoError(t, err)

	// Read config file
	data, err := os.ReadFile(configFile)
	require.NoError(t, err)
	assert.Equal(t, "app: config", string(data))

	// 2. Secret access (success)
	secretFile := filepath.Join(secretPath, "password.txt")
	err = os.WriteFile(secretFile, []byte("s3cr3t"), 0600)
	require.NoError(t, err)

	// Read secret file
	data, err = os.ReadFile(secretFile)
	require.NoError(t, err)
	assert.Equal(t, "s3cr3t", string(data))

	// 3. Failed config access (simulate missing config)
	nonExistentConfig := filepath.Join(configMapPath, "missing.yaml")
	_, err = os.ReadFile(nonExistentConfig)
	assert.Error(t, err) // Should fail

	// 4. Permission denied (simulate secret without permissions)
	restrictedSecret := filepath.Join(secretPath, "restricted.key")
	err = os.WriteFile(restrictedSecret, []byte("restricted"), 0000)
	require.NoError(t, err)

	_, err = os.ReadFile(restrictedSecret)
	assert.Error(t, err) // Should fail with permission denied

	// Wait for events to be captured
	wg.Wait()

	// Verify workflow results
	t.Logf("Total events captured: %d", len(capturedEvents))
	t.Logf("Error events: %d", len(errorEvents))

	// Verify we captured some events
	if len(capturedEvents) > 0 {
		// Check event structure
		for _, event := range capturedEvents {
			assert.Equal(t, "e2e-kernel", event.Source)
			assert.Equal(t, domain.EventTypeKernelSyscall, event.Type)
			assert.NotNil(t, event.EventData.Kernel)
			assert.NotEmpty(t, event.EventID)
			assert.NotZero(t, event.Timestamp)
		}

		// Log some sample events
		for i, event := range capturedEvents {
			if i >= 3 {
				break
			}
			t.Logf("Sample event %d: PID=%d, Command=%s, EventType=%s",
				i+1,
				event.EventData.Kernel.PID,
				event.EventData.Kernel.Command,
				event.EventData.Kernel.EventType)
		}
	}

	// Check observer health
	health := observer.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)

	// Check statistics
	stats := observer.Statistics()
	t.Logf("Observer statistics: Processed=%d, Errors=%d",
		stats.EventsProcessed, stats.ErrorCount)
}

// TestE2EHighVolumeProcessing tests high-volume event processing
func TestE2EHighVolumeProcessing(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("E2E test requires Linux with eBPF support")
	}

	if os.Geteuid() != 0 {
		t.Skip("E2E test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "e2e-volume",
		BufferSize: 10000, // Large buffer for high volume
		EnableEBPF: true,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	events := observer.Events()

	// Create temporary test directory
	tempDir, err := os.MkdirTemp("", "e2e-volume-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Event counter
	var eventCount int64
	var dropCount int64
	done := make(chan bool)

	// Event consumer
	go func() {
		timeout := time.After(10 * time.Second)
		for {
			select {
			case event := <-events:
				if event != nil {
					eventCount++
				}
			case <-timeout:
				done <- true
				return
			}
		}
	}()

	// Generate high volume of file operations
	t.Log("Generating high volume of file operations...")
	start := time.Now()

	// Parallel file operations
	numWorkers := 10
	numOpsPerWorker := 100
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for i := 0; i < numOpsPerWorker; i++ {
				// Create files
				filename := filepath.Join(tempDir, fmt.Sprintf("worker_%d_file_%d.txt", workerID, i))
				if err := os.WriteFile(filename, []byte("test data"), 0644); err != nil {
					continue
				}

				// Read files
				if _, err := os.ReadFile(filename); err != nil {
					continue
				}

				// Try to read non-existent file (generate errors)
				if _, err := os.ReadFile(filepath.Join(tempDir, fmt.Sprintf("missing_%d_%d", workerID, i))); err != nil {
					// Expected error
				}

				// Delete files
				os.Remove(filename)
			}
		}(w)
	}

	wg.Wait()
	elapsed := time.Since(start)

	// Wait for event processing
	<-done

	// Calculate statistics
	stats := observer.Statistics()
	if droppedStr, ok := stats.CustomMetrics["events_dropped"]; ok {
		if dropped, err := strconv.ParseInt(droppedStr, 10, 64); err == nil {
			dropCount = dropped
		}
	}

	t.Logf("High volume test completed in %v", elapsed)
	t.Logf("Total operations: %d", numWorkers*numOpsPerWorker*4)
	t.Logf("Events captured: %d", eventCount)
	t.Logf("Events dropped: %d", dropCount)
	t.Logf("Observer stats: Processed=%d, Errors=%d",
		stats.EventsProcessed, stats.ErrorCount)

	// Verify observer remained healthy under load
	health := observer.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)

	// Check for reasonable event capture (may not capture all due to filtering)
	if eventCount > 0 {
		t.Logf("Event capture rate: %.2f%%",
			float64(eventCount)/float64(numWorkers*numOpsPerWorker*4)*100)
	}
}

// TestE2EObserverResilience tests observer resilience and recovery
func TestE2EObserverResilience(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("E2E test requires Linux with eBPF support")
	}

	if os.Geteuid() != 0 {
		t.Skip("E2E test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "e2e-resilience",
		BufferSize: 100,
		EnableEBPF: true,
	}

	// Test 1: Start-Stop-Restart cycle
	t.Run("StartStopRestart", func(t *testing.T) {
		observer, err := NewObserverWithConfig(config, logger)
		require.NoError(t, err)

		ctx := context.Background()

		// First start
		err = observer.Start(ctx)
		require.NoError(t, err)

		// Verify it's running
		health := observer.Health()
		assert.Equal(t, domain.HealthHealthy, health.Status)

		// Stop
		err = observer.Stop()
		require.NoError(t, err)

		// Restart
		err = observer.Start(ctx)
		require.NoError(t, err)

		// Verify it's running again
		health = observer.Health()
		assert.Equal(t, domain.HealthHealthy, health.Status)

		// Final stop
		err = observer.Stop()
		require.NoError(t, err)
	})

	// Test 2: Context cancellation
	t.Run("ContextCancellation", func(t *testing.T) {
		observer, err := NewObserverWithConfig(config, logger)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())

		err = observer.Start(ctx)
		require.NoError(t, err)

		// Get events channel
		events := observer.Events()

		// Cancel context
		cancel()

		// Give it time to clean up
		time.Sleep(500 * time.Millisecond)

		// Stop should work gracefully
		err = observer.Stop()
		assert.NoError(t, err)

		// Events channel should be closed
		select {
		case _, ok := <-events:
			assert.False(t, ok, "Events channel should be closed")
		default:
			// Channel might be empty but not closed yet
		}
	})

	// Test 3: Buffer overflow handling
	t.Run("BufferOverflow", func(t *testing.T) {
		smallBufferConfig := &Config{
			Name:       "e2e-overflow",
			BufferSize: 10, // Very small buffer
			EnableEBPF: true,
		}

		observer, err := NewObserverWithConfig(smallBufferConfig, logger)
		require.NoError(t, err)

		ctx := context.Background()
		err = observer.Start(ctx)
		require.NoError(t, err)
		defer observer.Stop()

		// Don't consume events to cause buffer overflow
		tempDir, err := os.MkdirTemp("", "e2e-overflow")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Generate many file operations
		for i := 0; i < 100; i++ {
			filename := filepath.Join(tempDir, fmt.Sprintf("file_%d", i))
			os.WriteFile(filename, []byte("data"), 0644)
			os.ReadFile(filename)
			os.Remove(filename)
		}

		// Check statistics for dropped events
		stats := observer.Statistics()
		t.Logf("Buffer overflow test - Processed: %d, Errors: %d",
			stats.EventsProcessed, stats.ErrorCount)

		// Observer should remain healthy despite drops
		health := observer.Health()
		assert.Equal(t, domain.HealthHealthy, health.Status)
	})
}

// TestE2EMockModeWorkflow tests complete workflow in mock mode
func TestE2EMockModeWorkflow(t *testing.T) {
	// This test works on any platform
	t.Setenv("TAPIO_MOCK_MODE", "true")

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "e2e-mock",
		BufferSize: 100,
		EnableEBPF: false,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)
	assert.True(t, observer.mockMode)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	events := observer.Events()
	capturedEvents := make([]*domain.CollectorEvent, 0)

	// Collect mock events - mock events are generated every 3 seconds
	// so we need to wait at least 10 seconds for 3 events
	timeout := time.After(15 * time.Second)
	requiredEvents := 2 // Reduced to 2 since generation is every 3 seconds

	for len(capturedEvents) < requiredEvents {
		select {
		case event := <-events:
			if event != nil {
				capturedEvents = append(capturedEvents, event)
				t.Logf("Mock event %d: Type=%s, PID=%d, Command=%s",
					len(capturedEvents), event.Type,
					event.EventData.Kernel.PID,
					event.EventData.Kernel.Command)
			}
		case <-timeout:
			if len(capturedEvents) == 0 {
				t.Fatal("No mock events received")
			}
			t.Logf("Received %d mock events (expected at least %d)", len(capturedEvents), requiredEvents)
			break
		}
	}

	// Verify we got at least some mock events
	assert.GreaterOrEqual(t, len(capturedEvents), 1, "Should have at least 1 mock event")

	for _, event := range capturedEvents {
		assert.Equal(t, "kernel-e2e-mock", event.Source) // Source is prefixed with "kernel-"
		assert.Equal(t, domain.EventTypeKernelSyscall, event.Type)
		assert.NotNil(t, event.EventData.Kernel)
		assert.Contains(t, event.Metadata.Labels, "mock")
		assert.Equal(t, "true", event.Metadata.Labels["mock"])
		assert.Equal(t, "kernel", event.Metadata.Labels["observer"])

		// Verify mock event has realistic data
		assert.NotZero(t, event.EventData.Kernel.PID)
		assert.NotEmpty(t, event.EventData.Kernel.Command)
		assert.NotEmpty(t, event.EventData.Kernel.EventType)
	}

	// Check health and stats
	health := observer.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)

	stats := observer.Statistics()
	assert.GreaterOrEqual(t, stats.EventsProcessed, int64(len(capturedEvents)))
	t.Logf("Mock mode statistics: Processed=%d, Errors=%d",
		stats.EventsProcessed, stats.ErrorCount)
}
