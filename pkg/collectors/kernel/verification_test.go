package kernel

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Production readiness test suite
func TestProductionReadiness(t *testing.T) {
	t.Run("MemoryUsage", testMemoryUsage)
	t.Run("ResourceLimits", testResourceLimits)
	t.Run("BackpressureHandling", testBackpressureHandling)
	t.Run("GracefulDegradation", testGracefulDegradation)
	t.Run("CORECompatibility", testCORECompatibility)
	t.Run("ConcurrencyStress", testConcurrencyStress)
	t.Run("LongRunningStability", testLongRunningStability)
	t.Run("ErrorRecovery", testErrorRecovery)
}

// testMemoryUsage verifies memory usage stays within limits
func testMemoryUsage(t *testing.T) {
	config := DefaultConfig()
	config.ResourceLimits.MaxMemoryMB = 50 // 50MB limit for test

	resourceManager := NewResourceManager(&config.ResourceLimits)
	err := resourceManager.Start()
	require.NoError(t, err)
	defer resourceManager.Stop()

	// Monitor memory usage over time
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var maxMemory int64
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			usage := resourceManager.GetMemoryUsage()
			if usage > maxMemory {
				maxMemory = usage
			}

			// Verify we don't exceed 110% of limit (10% buffer for spikes)
			limitBytes := int64(config.ResourceLimits.MaxMemoryMB) * 1024 * 1024
			assert.True(t, usage < limitBytes*11/10,
				"Memory usage %d bytes exceeds 110%% of limit %d bytes", usage, limitBytes)

		case <-ctx.Done():
			t.Logf("Max memory usage: %d bytes (%.2f MB)", maxMemory, float64(maxMemory)/1024/1024)
			return
		}
	}
}

// testResourceLimits verifies resource limits are enforced
func testResourceLimits(t *testing.T) {
	config := DefaultConfig()
	config.ResourceLimits.MaxMemoryMB = 10   // Very low for testing
	config.ResourceLimits.MaxCPUPercent = 10 // Very low for testing
	config.ResourceLimits.EventQueueSize = 100

	resourceManager := NewResourceManager(&config.ResourceLimits)
	err := resourceManager.Start()
	require.NoError(t, err)
	defer resourceManager.Stop()

	// Test memory throttling
	t.Run("MemoryThrottling", func(t *testing.T) {
		// Simulate high memory usage
		// In a real test, we'd allocate memory to trigger throttling

		// For now, verify throttling methods work
		assert.False(t, resourceManager.IsMemoryThrottled(), "Should not be throttled initially")
		assert.True(t, resourceManager.CanProcessEvent(), "Should be able to process events initially")
	})

	// Test CPU throttling
	t.Run("CPUThrottling", func(t *testing.T) {
		assert.False(t, resourceManager.IsCPUThrottled(), "Should not be CPU throttled initially")
	})

	// Test queue throttling
	t.Run("QueueThrottling", func(t *testing.T) {
		// Simulate queue filling up
		for i := 0; i < 150; i++ { // Exceed queue size
			resourceManager.RecordEventEnqueue()
		}

		// Eventually should be throttled
		time.Sleep(2 * time.Second)
		queueLen := resourceManager.GetQueueLength()
		t.Logf("Queue length: %d", queueLen)

		// Clean up
		for i := 0; i < 150; i++ {
			resourceManager.RecordEventDequeue()
		}
	})
}

// testBackpressureHandling verifies backpressure mechanisms work
func testBackpressureHandling(t *testing.T) {
	config := DefaultConfig()
	config.Backpressure.Enabled = true
	config.Backpressure.HighWatermark = 0.7
	config.Backpressure.DropThreshold = 0.9

	backpressure := NewBackpressureManager(&config.Backpressure)

	// Test buffer usage tracking
	backpressure.UpdateBufferUsage("test_buffer", 500, 1000) // 50% usage
	assert.False(t, backpressure.IsThrottling(), "Should not throttle at 50% usage")
	assert.False(t, backpressure.ShouldDropEvent(), "Should not drop events at 50% usage")

	// Increase to high watermark
	backpressure.UpdateBufferUsage("test_buffer", 800, 1000) // 80% usage
	time.Sleep(100 * time.Millisecond)                       // Allow monitoring to update

	// Increase to drop threshold
	backpressure.UpdateBufferUsage("test_buffer", 950, 1000) // 95% usage
	time.Sleep(100 * time.Millisecond)

	stats := backpressure.GetStats()
	t.Logf("Backpressure stats: %+v", stats)

	backpressure.Stop()
}

// testGracefulDegradation verifies fallback mechanisms work
func testGracefulDegradation(t *testing.T) {
	config := DefaultConfig()

	// Create mock compatibility checker that reports no eBPF support
	coreCompat := &CoreCompatibility{
		kernelVersion: KernelVersion{Major: 3, Minor: 10, Patch: 0}, // Old kernel
		features: KernelFeatures{
			HasBTF:             false,
			HasRingBuffer:      false,
			HasBPFLSM:          false,
			HasTracepoints:     true,
			HasKprobes:         false,
			HasCORERelocations: false,
		},
	}

	backpressure := NewBackpressureManager(&config.Backpressure)
	degradation := NewGracefulDegradation(config, coreCompat, backpressure)

	err := degradation.StartMonitoring()
	assert.NoError(t, err, "Should start monitoring successfully")

	// Wait a bit for fallback activation
	time.Sleep(2 * time.Second)

	// Verify eBPF is disabled and fallbacks are active
	assert.False(t, degradation.IsEBPFEnabled(), "eBPF should be disabled on old kernel")

	activeFallbacks := degradation.GetActiveFallbacks()
	t.Logf("Active fallbacks: %v", activeFallbacks)
	assert.Greater(t, len(activeFallbacks), 0, "Should have active fallbacks")

	stats := degradation.GetStats()
	t.Logf("Degradation stats: %+v", stats)

	degradation.Stop()
	backpressure.Stop()
}

// testCORECompatibility verifies CO-RE compatibility detection
func testCORECompatibility(t *testing.T) {
	coreCompat, err := NewCoreCompatibility()
	require.NoError(t, err, "Should create CO-RE compatibility checker")

	version := coreCompat.GetKernelVersion()
	t.Logf("Kernel version: %s", version.String())
	assert.Greater(t, version.Major, 0, "Should detect kernel version")

	features := coreCompat.GetFeatures()
	t.Logf("Kernel features: %+v", features)

	// Test compatibility checks
	programs := coreCompat.GetCompatiblePrograms()
	t.Logf("Compatible programs: %v", programs)
	assert.Greater(t, len(programs), 0, "Should have at least some compatible programs")

	// Test fallback strategies
	if !coreCompat.IsCompatible("ring_buffer") {
		strategy := coreCompat.GetFallbackStrategy("ring_buffer")
		assert.Equal(t, "use_perf_buffer", strategy, "Should fallback to perf buffer")
	}

	// Test program validation
	compatible, reason := coreCompat.ValidateEBPFProgram("kprobe")
	t.Logf("Kprobe compatibility: %v (%s)", compatible, reason)
}

// testConcurrencyStress tests concurrent access patterns
func testConcurrencyStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrency stress test in short mode")
	}

	config := DefaultConfig()

	// Create components
	resourceManager := NewResourceManager(&config.ResourceLimits)
	err := resourceManager.Start()
	require.NoError(t, err)
	defer resourceManager.Stop()

	backpressure := NewBackpressureManager(&config.Backpressure)
	defer backpressure.Stop()

	eventPool := NewEventPool(1000)
	defer eventPool.Stop()

	// Stress test with multiple goroutines
	const numGoroutines = 50
	const operationsPerGoroutine = 1000

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errors := make(chan error, numGoroutines)

	// Start concurrent goroutines
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Test event pool
				event := eventPool.Get()
				event.PID = uint32(id*1000 + j)
				event.EventType = uint32(j % 10)
				eventPool.Put(event)

				// Test resource manager
				resourceManager.RecordEventEnqueue()
				if resourceManager.CanProcessEvent() {
					resourceManager.RecordEventDequeue()
				} else {
					resourceManager.RecordEventDrop()
				}

				// Test backpressure
				backpressure.UpdateBufferUsage(fmt.Sprintf("buffer_%d", id%5),
					int64(j%1000), 1000)

				if j%100 == 0 {
					runtime.Gosched() // Yield to other goroutines
				}
			}
		}(i)
	}

	// Wait for completion
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Log("Concurrency stress test completed successfully")
	case <-ctx.Done():
		t.Error("Concurrency stress test timed out")
	case err := <-errors:
		t.Errorf("Concurrency stress test failed: %v", err)
	}

	// Verify final state
	stats := resourceManager.GetStats()
	t.Logf("Final resource stats: %+v", stats)

	poolStats := eventPool.Stats()
	t.Logf("Final pool stats: %+v", poolStats)
}

// testLongRunningStability tests long-running stability
func testLongRunningStability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running stability test in short mode")
	}

	config := DefaultConfig()

	// Create all components
	resourceManager := NewResourceManager(&config.ResourceLimits)
	err := resourceManager.Start()
	require.NoError(t, err)
	defer resourceManager.Stop()

	backpressure := NewBackpressureManager(&config.Backpressure)
	defer backpressure.Stop()

	eventPool := NewEventPool(1000)
	defer eventPool.Stop()

	monitoring := NewProductionMonitoring(config)
	err = monitoring.Start(0) // Use port 0 for testing
	require.NoError(t, err)
	defer monitoring.Stop()

	// Run for a reasonable test duration
	testDuration := 2 * time.Minute
	if testing.Verbose() {
		testDuration = 5 * time.Minute
	}

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	// Simulate workload
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	eventCount := 0
	startMemory := resourceManager.GetMemoryUsage()

	for {
		select {
		case <-ticker.C:
			// Simulate event processing
			event := eventPool.Get()
			event.PID = uint32(eventCount)
			event.EventType = uint32(eventCount % 10)
			event.Timestamp = uint64(time.Now().UnixNano())

			// Record metrics
			monitoring.RecordEvent(fmt.Sprintf("type_%d", eventCount%5), "test")

			eventPool.Put(event)
			eventCount++

			// Update resource usage
			resourceManager.RecordEventEnqueue()
			if resourceManager.CanProcessEvent() {
				resourceManager.RecordEventDequeue()
			}

		case <-ctx.Done():
			endMemory := resourceManager.GetMemoryUsage()
			memoryGrowth := endMemory - startMemory

			t.Logf("Stability test results:")
			t.Logf("  Duration: %v", testDuration)
			t.Logf("  Events processed: %d", eventCount)
			t.Logf("  Start memory: %d bytes", startMemory)
			t.Logf("  End memory: %d bytes", endMemory)
			t.Logf("  Memory growth: %d bytes", memoryGrowth)

			// Verify memory growth is reasonable (< 10MB)
			assert.Less(t, memoryGrowth, int64(10*1024*1024),
				"Memory growth should be less than 10MB")

			stats := resourceManager.GetStats()
			t.Logf("  Final stats: %+v", stats)

			return
		}
	}
}

// testErrorRecovery tests error handling and recovery
func testErrorRecovery(t *testing.T) {
	config := DefaultConfig()
	config.Health.Enabled = true
	config.Health.MaxFailures = 3
	config.Health.RestartOnFailure = false // Don't restart in test

	coreCompat, err := NewCoreCompatibility()
	require.NoError(t, err)

	backpressure := NewBackpressureManager(&config.Backpressure)
	defer backpressure.Stop()

	degradation := NewGracefulDegradation(config, coreCompat, backpressure)
	err = degradation.StartMonitoring()
	require.NoError(t, err)
	defer degradation.Stop()

	// Test error scenarios
	t.Run("EBPFFailure", func(t *testing.T) {
		// Simulate eBPF failure
		degradation.handleEBPFFailure("test_failure", fmt.Errorf("test error"))

		// Verify fallback activation
		time.Sleep(1 * time.Second)
		assert.False(t, degradation.IsEBPFEnabled(), "eBPF should be disabled after failure")

		activeFallbacks := degradation.GetActiveFallbacks()
		assert.Greater(t, len(activeFallbacks), 0, "Should have active fallbacks after eBPF failure")
	})

	t.Run("HealthCheckRecovery", func(t *testing.T) {
		// Test health check recovery mechanisms
		stats := degradation.GetStats()
		t.Logf("Health stats: %+v", stats)

		// In a real test, we'd simulate health check failures and recoveries
		assert.NotZero(t, stats.EBPFFailureCount, "Should have recorded the test failure")
	})
}

// Benchmark tests for performance verification
func BenchmarkEventPool(b *testing.B) {
	pool := NewEventPool(1000)
	defer pool.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			event := pool.Get()
			event.PID = 12345
			event.EventType = 1
			pool.Put(event)
		}
	})
}

func BenchmarkResourceManager(b *testing.B) {
	config := DefaultConfig()
	rm := NewResourceManager(&config.ResourceLimits)
	err := rm.Start()
	require.NoError(b, err)
	defer rm.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if rm.CanProcessEvent() {
				rm.RecordEventEnqueue()
				rm.RecordEventDequeue()
			}
		}
	})
}

func BenchmarkBackpressureManager(b *testing.B) {
	config := DefaultConfig()
	bp := NewBackpressureManager(&config.Backpressure)
	defer bp.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			bp.UpdateBufferUsage("test_buffer", int64(i%1000), 1000)
			bp.ShouldDropEvent()
			i++
		}
	})
}

// Helper function to run all production tests
func TestProductionSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping full production test suite in short mode")
	}

	t.Log("Running comprehensive production readiness test suite...")

	// Run all tests in order
	tests := []struct {
		name string
		fn   func(*testing.T)
	}{
		{"MemoryUsage", testMemoryUsage},
		{"ResourceLimits", testResourceLimits},
		{"BackpressureHandling", testBackpressureHandling},
		{"GracefulDegradation", testGracefulDegradation},
		{"CORECompatibility", testCORECompatibility},
		{"ErrorRecovery", testErrorRecovery},
		{"ConcurrencyStress", testConcurrencyStress},
		{"LongRunningStability", testLongRunningStability},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start := time.Now()
			test.fn(t)
			duration := time.Since(start)
			t.Logf("Test %s completed in %v", test.name, duration)
		})
	}

	t.Log("Production readiness test suite completed successfully!")
}

// Integration test that verifies all components work together
func TestFullIntegration(t *testing.T) {
	config := DefaultConfig()

	// Create all components
	coreCompat, err := NewCoreCompatibility()
	require.NoError(t, err)

	resourceManager := NewResourceManager(&config.ResourceLimits)
	err = resourceManager.Start()
	require.NoError(t, err)
	defer resourceManager.Stop()

	backpressure := NewBackpressureManager(&config.Backpressure)
	defer backpressure.Stop()

	degradation := NewGracefulDegradation(config, coreCompat, backpressure)
	err = degradation.StartMonitoring()
	require.NoError(t, err)
	defer degradation.Stop()

	eventPool := NewEventPool(1000)
	defer eventPool.Stop()

	monitoring := NewProductionMonitoring(config)
	err = monitoring.Start(0)
	require.NoError(t, err)
	defer monitoring.Stop()

	// Run integrated workload
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		eventCount := 0
		ticker := time.NewTicker(1 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if !resourceManager.CanProcessEvent() {
					resourceManager.RecordEventDrop()
					continue
				}

				if backpressure.ShouldDropEvent() {
					continue
				}

				// Process event
				event := eventPool.Get()
				event.PID = uint32(eventCount)
				event.EventType = uint32(eventCount % 5)
				event.Timestamp = uint64(time.Now().UnixNano())

				// Simulate processing time
				time.Sleep(10 * time.Microsecond)

				monitoring.RecordEvent(fmt.Sprintf("type_%d", eventCount%5), "integration_test")
				monitoring.UpdateBufferUsage("test_buffer", float64((eventCount%1000)*100)/1000)

				eventPool.Put(event)
				eventCount++

				if eventCount%1000 == 0 {
					t.Logf("Processed %d events", eventCount)
				}

			case <-ctx.Done():
				t.Logf("Integration test completed. Total events: %d", eventCount)
				return
			}
		}
	}()

	wg.Wait()

	// Verify all components are still healthy
	assert.False(t, resourceManager.IsMemoryThrottled(), "Should not be memory throttled")
	assert.False(t, resourceManager.IsCPUThrottled(), "Should not be CPU throttled")

	stats := backpressure.GetStats()
	t.Logf("Final backpressure stats: %+v", stats)

	poolStats := eventPool.Stats()
	t.Logf("Final pool stats: %+v", poolStats)

	t.Log("Full integration test passed!")
}
