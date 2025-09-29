package containerruntime

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestObserver_StressConcurrentEvents tests observer under high concurrent load
func TestObserver_StressConcurrentEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("stress-test")
	config.BufferSize = 10000 // Large buffer for stress test

	observer, err := NewObserver("stress-test", config)
	require.NoError(t, err)
	observer.logger = logger
	observer.containerCache = make(map[string]*ContainerMetadata)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the observer
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	var wg sync.WaitGroup
	var successCount atomic.Int64
	var errorCount atomic.Int64

	// Number of concurrent goroutines
	concurrency := 100
	eventsPerGoroutine := 100

	// Start concurrent container operations
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < eventsPerGoroutine; j++ {
				containerID := fmt.Sprintf("stress-%d-%d", id, j)
				metadata := &ContainerMetadata{
					ContainerID:   containerID,
					ContainerName: fmt.Sprintf("app-%d", id),
					PodName:       fmt.Sprintf("pod-%d", id),
					Runtime:       "docker",
					CreatedAt:     time.Now(),
					LastSeen:      time.Now(),
				}

				// Random operation: start or stop
				if rand.Float32() < 0.7 {
					// 70% starts, 30% stops
					err := observer.OnContainerStart(containerID, metadata)
					if err != nil {
						errorCount.Add(1)
					} else {
						successCount.Add(1)
					}
				} else {
					err := observer.OnContainerStop(containerID)
					if err != nil {
						errorCount.Add(1)
					} else {
						successCount.Add(1)
					}
				}

				// Small random delay
				if j%10 == 0 {
					time.Sleep(time.Microsecond * time.Duration(rand.Intn(100)))
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify results
	totalOperations := int64(concurrency * eventsPerGoroutine)
	actualOperations := successCount.Load() + errorCount.Load()

	t.Logf("Stress test completed: %d successful, %d errors out of %d total",
		successCount.Load(), errorCount.Load(), totalOperations)

	assert.Equal(t, totalOperations, actualOperations)
	assert.Greater(t, successCount.Load(), int64(0))

	// Error rate should be very low
	errorRate := float64(errorCount.Load()) / float64(totalOperations)
	assert.Less(t, errorRate, 0.01) // Less than 1% error rate
}

// TestObserver_MemoryLeakDetection tests for memory leaks during long running operations
func TestObserver_MemoryLeakDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("memory-test")

	observer, err := NewObserver("memory-test", config)
	require.NoError(t, err)
	observer.logger = logger
	observer.containerCache = make(map[string]*ContainerMetadata)

	_, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Track memory usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	initialAlloc := memStats.Alloc

	// Perform many operations
	for i := 0; i < 10000; i++ {
		containerID := fmt.Sprintf("mem-test-%d", i)
		metadata := &ContainerMetadata{
			ContainerID: containerID,
			PodName:     fmt.Sprintf("pod-%d", i),
			Labels:      make(map[string]string),
		}

		// Add random labels
		for j := 0; j < 10; j++ {
			metadata.Labels[fmt.Sprintf("key-%d", j)] = fmt.Sprintf("value-%d", j)
		}

		// Start container
		_ = observer.OnContainerStart(containerID, metadata)

		// Stop container after a while to trigger cleanup
		if i > 100 {
			oldID := fmt.Sprintf("mem-test-%d", i-100)
			_ = observer.OnContainerStop(oldID)
		}
	}

	// Force garbage collection
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	finalAlloc := memStats.Alloc

	// Calculate memory growth
	memoryGrowth := finalAlloc - initialAlloc
	memoryGrowthMB := float64(memoryGrowth) / 1024 / 1024

	t.Logf("Memory growth: %.2f MB", memoryGrowthMB)

	// Memory growth should be reasonable (less than 100MB for this test)
	assert.Less(t, memoryGrowthMB, 100.0)

	// Cache should have cleaned up old entries
	assert.LessOrEqual(t, len(observer.containerCache), 100)
}

// BenchmarkObserver_OnContainerStart benchmarks container start operations
func BenchmarkObserver_OnContainerStart(b *testing.B) {
	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("bench")
	observer, _ := NewObserver("bench", config)
	observer.logger = logger
	observer.containerCache = make(map[string]*ContainerMetadata)

	metadata := &ContainerMetadata{
		ContainerID:   "bench-container",
		ContainerName: "bench-app",
		PodName:       "bench-pod",
		Runtime:       "docker",
		Labels:        map[string]string{"app": "bench"},
		CreatedAt:     time.Now(),
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			containerID := fmt.Sprintf("bench-%d", i)
			_ = observer.OnContainerStart(containerID, metadata)
			i++
		}
	})
}

// BenchmarkObserver_CacheOperations benchmarks cache read/write operations
func BenchmarkObserver_CacheOperations(b *testing.B) {
	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("bench-cache")
	observer, _ := NewObserver("bench-cache", config)
	observer.logger = logger
	observer.containerCache = make(map[string]*ContainerMetadata)

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		observer.containerCache[fmt.Sprintf("container-%d", i)] = &ContainerMetadata{
			ContainerID: fmt.Sprintf("container-%d", i),
		}
	}

	b.Run("Read", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				observer.cacheMu.RLock()
				_ = observer.containerCache["container-500"]
				observer.cacheMu.RUnlock()
			}
		})
	})

	b.Run("Write", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				observer.cacheMu.Lock()
				observer.containerCache[fmt.Sprintf("new-%d", i)] = &ContainerMetadata{
					ContainerID: fmt.Sprintf("new-%d", i),
				}
				observer.cacheMu.Unlock()
				i++
			}
		})
	})

	b.Run("ReadWrite", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if i%2 == 0 {
					observer.cacheMu.RLock()
					_ = observer.containerCache["container-500"]
					observer.cacheMu.RUnlock()
				} else {
					observer.cacheMu.Lock()
					observer.containerCache[fmt.Sprintf("mixed-%d", i)] = &ContainerMetadata{
						ContainerID: fmt.Sprintf("mixed-%d", i),
					}
					observer.cacheMu.Unlock()
				}
				i++
			}
		})
	})
}

// TestObserver_RaceConditions tests for race conditions
func TestObserver_RaceConditions(t *testing.T) {
	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("race-test")
	observer, err := NewObserver("race-test", config)
	require.NoError(t, err)
	observer.logger = logger
	observer.containerCache = make(map[string]*ContainerMetadata)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start multiple goroutines doing different operations
	var wg sync.WaitGroup

	// Goroutine 1: Continuously start containers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			metadata := &ContainerMetadata{
				ContainerID: fmt.Sprintf("race-%d", i),
			}
			_ = observer.OnContainerStart(fmt.Sprintf("race-%d", i), metadata)
			time.Sleep(time.Microsecond)
		}
	}()

	// Goroutine 2: Continuously stop containers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = observer.OnContainerStop(fmt.Sprintf("race-%d", i))
			time.Sleep(time.Microsecond)
		}
	}()

	// Goroutine 3: Read cache
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			observer.cacheMu.RLock()
			_ = len(observer.containerCache)
			observer.cacheMu.RUnlock()
			time.Sleep(time.Microsecond)
		}
	}()

	// Goroutine 4: Get statistics
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = observer.Statistics()
			time.Sleep(time.Millisecond)
		}
	}()

	// Wait for all goroutines
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Test completed successfully
	case <-ctx.Done():
		t.Fatal("Test timeout - possible deadlock")
	}
}
