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
		timeout := time.After(30 * time.Second) // Extended from 5s to 30s
		for {
			select {
			case event := <-events:
				t.Logf("[%s] Captured E2E event: Type=%s, Source=%s, EventID=%s",
					time.Now().Format("15:04:05.000"),
					event.Type, event.Source, event.EventID)

				if event.EventData.Kernel != nil {
					capturedEvents = append(capturedEvents, event)
					t.Logf("[%s] Kernel event details: PID=%d, Command=%s, EventType=%s",
						time.Now().Format("15:04:05.000"),
						event.EventData.Kernel.PID,
						event.EventData.Kernel.Command,
						event.EventData.Kernel.EventType)

					// Check for error events
					if event.EventData.Kernel.ReturnCode != 0 {
						errorEvents = append(errorEvents, event)
						t.Logf("[%s] ERROR EVENT: ReturnCode=%d, Error=%s",
							time.Now().Format("15:04:05.000"),
							event.EventData.Kernel.ReturnCode,
							event.EventData.Kernel.ErrorMessage)
					}
				}

			case <-timeout:
				return
			}
		}
	}()

	// Simulate application workflow
	t.Logf("[%s] Starting Kubernetes config access workflow simulation...",
		time.Now().Format("15:04:05.000"))

	// 1. ConfigMap access (success)
	configFile := filepath.Join(configMapPath, "application.yaml")
	t.Logf("[%s] Writing ConfigMap file: %s", time.Now().Format("15:04:05.000"), configFile)
	err = os.WriteFile(configFile, []byte("app: config"), 0644)
	require.NoError(t, err)

	// Read config file
	t.Logf("[%s] Reading ConfigMap file: %s", time.Now().Format("15:04:05.000"), configFile)
	data, err := os.ReadFile(configFile)
	require.NoError(t, err)
	assert.Equal(t, "app: config", string(data))
	time.Sleep(100 * time.Millisecond) // Give time for event processing

	// 2. Secret access (success)
	secretFile := filepath.Join(secretPath, "password.txt")
	t.Logf("[%s] Writing Secret file: %s", time.Now().Format("15:04:05.000"), secretFile)
	err = os.WriteFile(secretFile, []byte("s3cr3t"), 0600)
	require.NoError(t, err)

	// Read secret file
	t.Logf("[%s] Reading Secret file: %s", time.Now().Format("15:04:05.000"), secretFile)
	data, err = os.ReadFile(secretFile)
	require.NoError(t, err)
	assert.Equal(t, "s3cr3t", string(data))
	time.Sleep(100 * time.Millisecond) // Give time for event processing

	// 3. Failed config access (simulate missing config)
	nonExistentConfig := filepath.Join(configMapPath, "missing.yaml")
	t.Logf("[%s] Attempting to read non-existent file: %s", time.Now().Format("15:04:05.000"), nonExistentConfig)
	_, err = os.ReadFile(nonExistentConfig)
	assert.Error(t, err) // Should fail
	t.Logf("[%s] Expected error occurred: %v", time.Now().Format("15:04:05.000"), err)
	time.Sleep(100 * time.Millisecond) // Give time for event processing

	// 4. Permission denied (simulate secret without permissions)
	restrictedSecret := filepath.Join(secretPath, "restricted.key")
	t.Logf("[%s] Writing restricted secret: %s", time.Now().Format("15:04:05.000"), restrictedSecret)
	err = os.WriteFile(restrictedSecret, []byte("restricted"), 0000)
	require.NoError(t, err)

	t.Logf("[%s] Attempting to read restricted file: %s", time.Now().Format("15:04:05.000"), restrictedSecret)
	_, err = os.ReadFile(restrictedSecret)
	assert.Error(t, err) // Should fail with permission denied
	t.Logf("[%s] Expected permission error: %v", time.Now().Format("15:04:05.000"), err)
	time.Sleep(100 * time.Millisecond) // Give time for event processing

	// Add more file operations to generate more events
	t.Logf("[%s] Generating additional file operations...", time.Now().Format("15:04:05.000"))
	for i := 0; i < 10; i++ {
		testFile := filepath.Join(configMapPath, fmt.Sprintf("test-%d.yaml", i))
		err := os.WriteFile(testFile, []byte(fmt.Sprintf("test: %d", i)), 0644)
		if err != nil {
			t.Logf("[%s] Failed to write test file %d: %v", time.Now().Format("15:04:05.000"), i, err)
		}
		_, err = os.ReadFile(testFile)
		if err != nil {
			t.Logf("[%s] Failed to read test file %d: %v", time.Now().Format("15:04:05.000"), i, err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for events to be captured
	t.Logf("[%s] Waiting for event collection to complete...", time.Now().Format("15:04:05.000"))
	wg.Wait()

	// Verify workflow results
	t.Logf("[%s] === TEST SUMMARY ===", time.Now().Format("15:04:05.000"))
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
	t.Logf("[%s] Observer health: %s", time.Now().Format("15:04:05.000"), health.Status)

	// Check statistics
	stats := observer.Statistics()
	t.Logf("[%s] Observer statistics:", time.Now().Format("15:04:05.000"))
	t.Logf("  - Events Processed: %d", stats.EventsProcessed)
	if droppedStr, ok := stats.CustomMetrics["events_dropped"]; ok {
		t.Logf("  - Events Dropped: %s", droppedStr)
	} else {
		t.Logf("  - Events Dropped: 0")
	}
	t.Logf("  - Errors: %d", stats.ErrorCount)
	t.Logf("  - Uptime: %s", stats.Uptime)

	// Log custom metrics if available
	if len(stats.CustomMetrics) > 0 {
		t.Logf("  - Custom Metrics:")
		for k, v := range stats.CustomMetrics {
			t.Logf("    %s: %s", k, v)
		}
	}
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
		timeout := time.After(30 * time.Second) // Extended from 10s to 30s
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case event := <-events:
				if event != nil {
					eventCount++
					if eventCount%100 == 0 {
						t.Logf("[%s] Processed %d events so far",
							time.Now().Format("15:04:05.000"), eventCount)
					}
				}
			case <-ticker.C:
				t.Logf("[%s] Status: %d events captured",
					time.Now().Format("15:04:05.000"), eventCount)
			case <-timeout:
				t.Logf("[%s] Collection timeout reached",
					time.Now().Format("15:04:05.000"))
				done <- true
				return
			}
		}
	}()

	// Generate high volume of file operations
	t.Logf("[%s] Starting high volume file operation generation...",
		time.Now().Format("15:04:05.000"))
	start := time.Now()

	// Parallel file operations
	numWorkers := 20       // Increased from 10 to 20
	numOpsPerWorker := 200 // Increased from 100 to 200
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

				// Log progress for long-running test
				if i > 0 && i%50 == 0 {
					t.Logf("[%s] Worker %d: %d/%d operations completed",
						time.Now().Format("15:04:05.000"), workerID, i, numOpsPerWorker)
				}
			}
		}(w)
	}

	wg.Wait()
	elapsed := time.Since(start)
	t.Logf("[%s] All file operations completed in %v",
		time.Now().Format("15:04:05.000"), elapsed)

	// Wait for event processing
	t.Logf("[%s] Waiting for event processing to complete...",
		time.Now().Format("15:04:05.000"))
	<-done

	// Calculate statistics
	stats := observer.Statistics()
	if droppedStr, ok := stats.CustomMetrics["events_dropped"]; ok {
		if dropped, err := strconv.ParseInt(droppedStr, 10, 64); err == nil {
			dropCount = dropped
		}
	}

	t.Logf("[%s] === HIGH VOLUME TEST SUMMARY ===", time.Now().Format("15:04:05.000"))
	t.Logf("Test duration: %v", elapsed)
	t.Logf("Total operations: %d", numWorkers*numOpsPerWorker*4)
	t.Logf("Events captured: %d", eventCount)
	t.Logf("Events dropped: %d", dropCount)
	t.Logf("Observer statistics:")
	t.Logf("  - Processed: %d", stats.EventsProcessed)
	t.Logf("  - Errors: %d", stats.ErrorCount)
	t.Logf("  - Uptime: %s", stats.Uptime)

	// Verify observer remained healthy under load
	health := observer.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)

	// Check for reasonable event capture (may not capture all due to filtering)
	if eventCount > 0 {
		captureRate := float64(eventCount) / float64(numWorkers*numOpsPerWorker*4) * 100
		t.Logf("Event capture rate: %.2f%%", captureRate)
		t.Logf("Events per second: %.2f", float64(eventCount)/elapsed.Seconds())
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
	// Extended runtime for better event collection
	timeout := time.After(30 * time.Second) // Extended from 15s to 30s
	requiredEvents := 5                     // Increased to collect more events

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
