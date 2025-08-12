package correlation

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestStorageJobProcessing tests the core storage job processing without dependencies
func TestStorageJobProcessing(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("storage_job_creation", func(t *testing.T) {
		result := &CorrelationResult{
			ID:         "test-1",
			Type:       "test",
			Confidence: 0.8,
			Message:    "Test correlation",
			StartTime:  time.Now(),
			EndTime:    time.Now(),
		}

		job := &storageJob{
			result:    result,
			timestamp: time.Now(),
		}

		assert.NotNil(t, job)
		assert.Equal(t, "test-1", job.result.ID)
		assert.Equal(t, "test", job.result.Type)
		assert.True(t, job.timestamp.Before(time.Now().Add(time.Second)))
	})

	t.Run("engine_metrics_with_storage", func(t *testing.T) {
		// Mock storage that counts operations
		storage := &mockStorage{}

		config := &EngineConfig{
			EventBufferSize:        100,
			ResultBufferSize:       100,
			WorkerCount:            2,
			StorageWorkerCount:     5,
			StorageQueueSize:       50,
			ProcessingTimeout:      30 * time.Second,
			StorageCleanupInterval: 5 * time.Minute,
			StorageRetention:       24 * time.Hour,
			EnabledCorrelators:     []string{}, // No correlators to avoid dependencies
		}

		engine, err := NewEngine(logger, *config, nil, storage)
		require.NoError(t, err)

		// Check initial metrics
		metrics := engine.GetMetrics()
		assert.Equal(t, 5, metrics.StorageWorkers)
		assert.Equal(t, int64(0), metrics.StorageProcessed)
		assert.Equal(t, int64(0), metrics.StorageRejected)
		assert.Equal(t, 0, metrics.StorageQueueSize)

		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)
		defer engine.Stop()

		// Submit some storage operations directly
		for i := 0; i < 10; i++ {
			result := &CorrelationResult{
				ID:         "test-" + string(rune('0'+i)),
				Type:       "unit-test",
				Confidence: 0.8,
			}
			engine.asyncStoreResult(ctx, result)
		}

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		// Check final metrics
		finalMetrics := engine.GetMetrics()
		assert.True(t, finalMetrics.StorageProcessed > 0)
		assert.Equal(t, int32(10), atomic.LoadInt32(&storage.storeCallCount))
	})

	t.Run("storage_worker_backpressure", func(t *testing.T) {
		storage := &mockStorage{
			storeDelay: 50 * time.Millisecond, // Slow storage
		}

		config := &EngineConfig{
			EventBufferSize:        10,
			ResultBufferSize:       10,
			WorkerCount:            1,
			StorageWorkerCount:     2,
			StorageQueueSize:       5, // Small queue
			ProcessingTimeout:      30 * time.Second,
			StorageCleanupInterval: 5 * time.Minute,
			StorageRetention:       24 * time.Hour,
			EnabledCorrelators:     []string{}, // No correlators
		}

		engine, err := NewEngine(logger, *config, nil, storage)
		require.NoError(t, err)

		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)
		defer engine.Stop()

		// Fill up the queue rapidly
		for i := 0; i < 15; i++ {
			result := &CorrelationResult{
				ID:   "backpressure-" + string(rune('0'+(i%10))),
				Type: "backpressure-test",
			}
			engine.asyncStoreResult(ctx, result)
		}

		// Some operations should be rejected due to backpressure
		metrics := engine.GetMetrics()
		assert.True(t, metrics.StorageRejected > 0, "Expected rejections due to queue full")
	})

	t.Run("concurrent_storage_operations", func(t *testing.T) {
		storage := &mockStorage{}

		config := &EngineConfig{
			EventBufferSize:        100,
			ResultBufferSize:       100,
			WorkerCount:            4,
			StorageWorkerCount:     10,
			StorageQueueSize:       200,
			ProcessingTimeout:      30 * time.Second,
			StorageCleanupInterval: 5 * time.Minute,
			StorageRetention:       24 * time.Hour,
			EnabledCorrelators:     []string{}, // No correlators
		}

		engine, err := NewEngine(logger, *config, nil, storage)
		require.NoError(t, err)

		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)
		defer engine.Stop()

		// Submit operations from multiple goroutines
		var wg sync.WaitGroup
		numGoroutines := 10
		opsPerGoroutine := 20

		for g := 0; g < numGoroutines; g++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()
				for i := 0; i < opsPerGoroutine; i++ {
					result := &CorrelationResult{
						ID:   "concurrent-" + string(rune('0'+(goroutineID%10))) + "-" + string(rune('0'+(i%10))),
						Type: "concurrent-test",
					}
					engine.asyncStoreResult(ctx, result)
				}
			}(g)
		}

		wg.Wait()

		// Wait for all operations to complete
		time.Sleep(200 * time.Millisecond)

		// Verify all operations were processed
		totalExpected := numGoroutines * opsPerGoroutine
		assert.Equal(t, int32(totalExpected), atomic.LoadInt32(&storage.storeCallCount))
	})
}

// TestEngineLifecycle tests proper startup and shutdown with storage workers
func TestEngineLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	storage := &mockStorage{}

	config := &EngineConfig{
		EventBufferSize:        50,
		ResultBufferSize:       50,
		WorkerCount:            3,
		StorageWorkerCount:     5,
		StorageQueueSize:       25,
		ProcessingTimeout:      30 * time.Second,
		StorageCleanupInterval: 5 * time.Minute,
		StorageRetention:       24 * time.Hour,
		EnabledCorrelators:     []string{}, // No correlators to avoid dependencies
	}

	engine, err := NewEngine(logger, *config, nil, storage)
	require.NoError(t, err)

	ctx := context.Background()

	// Start engine
	err = engine.Start(ctx)
	require.NoError(t, err)

	// Verify workers are running
	metrics := engine.GetMetrics()
	assert.Equal(t, 3, metrics.WorkersCount)
	assert.Equal(t, 5, metrics.StorageWorkers)
	assert.True(t, metrics.IsHealthy)

	// Submit some work
	for i := 0; i < 10; i++ {
		result := &CorrelationResult{
			ID:   "lifecycle-" + string(rune('0'+(i%10))),
			Type: "lifecycle-test",
		}
		engine.asyncStoreResult(ctx, result)
	}

	// Stop engine gracefully
	err = engine.Stop()
	require.NoError(t, err)

	// Verify clean shutdown
	finalMetrics := engine.GetMetrics()
	assert.False(t, finalMetrics.IsHealthy)

	// Verify all submitted work was processed
	assert.Equal(t, int32(10), atomic.LoadInt32(&storage.storeCallCount))
}

// BenchmarkStorageWorkerPoolOverhead benchmarks the overhead of the worker pool
func BenchmarkStorageWorkerPoolOverhead(b *testing.B) {
	logger := zaptest.NewLogger(b)
	storage := &mockStorage{}

	config := &EngineConfig{
		EventBufferSize:        1000,
		ResultBufferSize:       1000,
		WorkerCount:            1,
		StorageWorkerCount:     10,
		StorageQueueSize:       1000,
		ProcessingTimeout:      30 * time.Second,
		StorageCleanupInterval: 5 * time.Minute,
		StorageRetention:       24 * time.Hour,
		EnabledCorrelators:     []string{}, // No correlators
	}

	engine, err := NewEngine(logger, *config, nil, storage)
	require.NoError(b, err)

	ctx := context.Background()
	err = engine.Start(ctx)
	require.NoError(b, err)
	defer engine.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			result := &CorrelationResult{
				ID:   "bench-" + string(rune('0'+(i%10))),
				Type: "benchmark",
			}
			engine.asyncStoreResult(ctx, result)
			i++
		}
	})
}
