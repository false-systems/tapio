package correlation

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// mockStorage is a test implementation of the Storage interface
type mockStorage struct {
	mu              sync.Mutex
	stored          []*CorrelationResult
	storeCallCount  int32
	storeDelay      time.Duration
	failAfter       int
	simulateTimeout bool
}

func (m *mockStorage) Store(ctx context.Context, result *CorrelationResult) error {
	atomic.AddInt32(&m.storeCallCount, 1)

	// Simulate processing delay
	if m.storeDelay > 0 {
		select {
		case <-time.After(m.storeDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Simulate timeout
	if m.simulateTimeout {
		<-ctx.Done()
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Simulate failure after N calls
	if m.failAfter > 0 && len(m.stored) >= m.failAfter {
		return assert.AnError
	}

	m.stored = append(m.stored, result)
	return nil
}

func (m *mockStorage) GetRecent(ctx context.Context, limit int) ([]*CorrelationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if limit > len(m.stored) {
		limit = len(m.stored)
	}
	return m.stored[:limit], nil
}

func (m *mockStorage) GetByTraceID(ctx context.Context, traceID string) ([]*CorrelationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var results []*CorrelationResult
	for _, r := range m.stored {
		if r.TraceID == traceID {
			results = append(results, r)
		}
	}
	return results, nil
}

func (m *mockStorage) GetByTimeRange(ctx context.Context, start, end time.Time) ([]*CorrelationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var results []*CorrelationResult
	for _, r := range m.stored {
		if r.StartTime.After(start) && r.EndTime.Before(end) {
			results = append(results, r)
		}
	}
	return results, nil
}

func (m *mockStorage) GetByResource(ctx context.Context, resourceType, namespace, name string) ([]*CorrelationResult, error) {
	return nil, nil
}

func (m *mockStorage) Cleanup(ctx context.Context, olderThan time.Duration) error {
	return nil
}

// TestStorageWorkerPool tests the storage worker pool implementation
func TestStorageWorkerPool(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("basic_worker_pool_operation", func(t *testing.T) {
		storage := &mockStorage{}
		config := TestEngineConfig()
		config.StorageWorkerCount = 5
		config.StorageQueueSize = 50

		engine, err := NewEngine(logger, *config, nil, storage)
		require.NoError(t, err)

		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)
		defer engine.Stop()

		// Submit multiple correlations
		numResults := 20
		for i := 0; i < numResults; i++ {
			result := &CorrelationResult{
				ID:         testCorrelationID(i),
				Type:       "test",
				Confidence: 0.8,
				Message:    "Test correlation",
			}
			engine.asyncStoreResult(ctx, result)
		}

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		// Verify all were stored
		assert.Equal(t, int32(numResults), atomic.LoadInt32(&storage.storeCallCount))
		assert.Len(t, storage.stored, numResults)
	})

	t.Run("worker_pool_with_backpressure", func(t *testing.T) {
		storage := &mockStorage{
			storeDelay: 10 * time.Millisecond, // Slow storage
		}
		config := TestEngineConfig()
		config.StorageWorkerCount = 2
		config.StorageQueueSize = 5 // Small queue to test backpressure

		engine, err := NewEngine(logger, *config, nil, storage)
		require.NoError(t, err)

		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)
		defer engine.Stop()

		// Submit more results than queue can hold
		numResults := 10
		for i := 0; i < numResults; i++ {
			result := &CorrelationResult{
				ID:         testCorrelationID(i),
				Type:       "test",
				Confidence: 0.8,
			}
			engine.asyncStoreResult(ctx, result)
		}

		// Some should be rejected due to queue full
		metrics := engine.GetMetrics()
		assert.True(t, metrics.StorageRejected > 0, "Expected some rejections due to backpressure")

		// Wait for processing
		time.Sleep(200 * time.Millisecond)

		// Verify processed count
		assert.True(t, metrics.StorageProcessed > 0, "Expected some successful storage operations")
	})

	t.Run("graceful_shutdown", func(t *testing.T) {
		storage := &mockStorage{
			storeDelay: 5 * time.Millisecond,
		}
		config := TestEngineConfig()
		config.StorageWorkerCount = 3
		config.StorageQueueSize = 30

		engine, err := NewEngine(logger, *config, nil, storage)
		require.NoError(t, err)

		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)

		// Submit correlations
		numResults := 15
		for i := 0; i < numResults; i++ {
			result := &CorrelationResult{
				ID:   testCorrelationID(i),
				Type: "test",
			}
			engine.asyncStoreResult(ctx, result)
		}

		// Stop engine - should process pending items
		err = engine.Stop()
		require.NoError(t, err)

		// Verify no goroutine leaks
		metrics := engine.GetMetrics()
		assert.Equal(t, int64(0), metrics.WorkersCount)
		assert.Equal(t, int64(0), metrics.StorageWorkers)
	})

	t.Run("storage_timeout_handling", func(t *testing.T) {
		storage := &mockStorage{
			simulateTimeout: true,
		}
		config := TestEngineConfig()
		config.StorageWorkerCount = 2

		engine, err := NewEngine(logger, *config, nil, storage)
		require.NoError(t, err)

		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)
		defer engine.Stop()

		// Submit result that will timeout
		result := &CorrelationResult{
			ID:   "timeout-test",
			Type: "test",
		}
		engine.asyncStoreResult(ctx, result)

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		// Verify no stored results due to timeout
		assert.Len(t, storage.stored, 0)
	})

	t.Run("concurrent_processing", func(t *testing.T) {
		storage := &mockStorage{}
		config := TestEngineConfig()
		config.StorageWorkerCount = 10
		config.StorageQueueSize = 100

		engine, err := NewEngine(logger, *config, nil, storage)
		require.NoError(t, err)

		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)
		defer engine.Stop()

		// Submit many correlations concurrently
		var wg sync.WaitGroup
		numGoroutines := 10
		resultsPerGoroutine := 10

		for g := 0; g < numGoroutines; g++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()
				for i := 0; i < resultsPerGoroutine; i++ {
					result := &CorrelationResult{
						ID:   testCorrelationIDWithPrefix(goroutineID, i),
						Type: "concurrent-test",
					}
					engine.asyncStoreResult(ctx, result)
				}
			}(g)
		}

		wg.Wait()

		// Wait for processing
		time.Sleep(200 * time.Millisecond)

		// Verify all were processed
		expectedTotal := numGoroutines * resultsPerGoroutine
		assert.Equal(t, int32(expectedTotal), atomic.LoadInt32(&storage.storeCallCount))
	})

	t.Run("metrics_tracking", func(t *testing.T) {
		storage := &mockStorage{
			storeDelay: 5 * time.Millisecond,
		}
		config := TestEngineConfig()
		config.StorageWorkerCount = 3
		config.StorageQueueSize = 30

		engine, err := NewEngine(logger, *config, nil, storage)
		require.NoError(t, err)

		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)
		defer engine.Stop()

		// Get initial metrics
		initialMetrics := engine.GetMetrics()
		assert.Equal(t, int64(0), initialMetrics.StorageProcessed)
		assert.Equal(t, int64(0), initialMetrics.StorageRejected)
		assert.Equal(t, 3, initialMetrics.StorageWorkers)

		// Submit correlations
		numResults := 10
		for i := 0; i < numResults; i++ {
			result := &CorrelationResult{
				ID:   testCorrelationID(i),
				Type: "metrics-test",
			}
			engine.asyncStoreResult(ctx, result)
		}

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		// Get final metrics
		finalMetrics := engine.GetMetrics()
		assert.Equal(t, int64(numResults), finalMetrics.StorageProcessed)
		assert.True(t, finalMetrics.StorageQueueSize >= 0)
		assert.True(t, finalMetrics.StorageQueueSize <= 30)
	})
}

// TestStorageWorkerPoolStress performs stress testing on the worker pool
func TestStorageWorkerPoolStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	logger := zaptest.NewLogger(t)
	storage := &mockStorage{}

	config := TestEngineConfig()
	config.StorageWorkerCount = 50
	config.StorageQueueSize = 1000
	config.WorkerCount = 10
	config.EventBufferSize = 1000

	engine, err := NewEngine(logger, *config, nil, storage)
	require.NoError(t, err)

	ctx := context.Background()
	err = engine.Start(ctx)
	require.NoError(t, err)
	defer engine.Stop()

	// Generate load
	numEvents := 10000
	var wg sync.WaitGroup

	// Event producers
	for p := 0; p < 5; p++ {
		wg.Add(1)
		go func(producerID int) {
			defer wg.Done()
			for i := 0; i < numEvents/5; i++ {
				event := &domain.UnifiedEvent{
					ID:   testEventIDWithPrefix(producerID, i),
					Type: domain.EventTypeKubernetes,
				}
				engine.Process(ctx, event)

				// Also generate direct storage operations
				if i%10 == 0 {
					result := &CorrelationResult{
						ID:   testCorrelationIDWithPrefix(producerID, i),
						Type: "stress-test",
					}
					engine.asyncStoreResult(ctx, result)
				}
			}
		}(p)
	}

	wg.Wait()

	// Wait for processing to complete
	time.Sleep(2 * time.Second)

	// Verify system stability
	metrics := engine.GetMetrics()
	assert.True(t, metrics.IsHealthy)
	assert.True(t, metrics.EventsProcessed > 0)
	assert.True(t, metrics.StorageProcessed > 0)

	// Check for memory leaks (goroutine count should be reasonable)
	assert.Equal(t, config.WorkerCount, metrics.WorkersCount)
	assert.Equal(t, config.StorageWorkerCount, metrics.StorageWorkers)
}

// Helper functions for test IDs
func testCorrelationID(i int) string {
	return "correlation-" + string(rune('0'+i))
}

func testCorrelationIDWithPrefix(prefix, i int) string {
	return "correlation-" + string(rune('0'+prefix)) + "-" + string(rune('0'+i))
}

func testEventIDWithPrefix(prefix, i int) string {
	return "event-" + string(rune('0'+prefix)) + "-" + string(rune('0'+i))
}

// BenchmarkStorageWorkerPool benchmarks the storage worker pool
func BenchmarkStorageWorkerPool(b *testing.B) {
	logger := zap.NewNop()
	storage := &mockStorage{}

	config := DefaultEngineConfig()
	config.StorageWorkerCount = 10
	config.StorageQueueSize = 100

	engine, err := NewEngine(logger, *config, nil, storage)
	require.NoError(b, err)

	ctx := context.Background()
	err = engine.Start(ctx)
	require.NoError(b, err)
	defer engine.Stop()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			result := &CorrelationResult{
				ID:         testCorrelationID(i),
				Type:       "benchmark",
				Confidence: 0.8,
			}
			engine.asyncStoreResult(ctx, result)
			i++
		}
	})
}
