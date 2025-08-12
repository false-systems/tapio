package correlation

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestAsyncBatchStorage_BasicFunctionality tests basic async storage operations
func TestAsyncBatchStorage_BasicFunctionality(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockStorage := &testMockStorage{}

	config := AsyncBatchConfig{
		BatchSize:     10,
		FlushInterval: 100 * time.Millisecond,
		WorkerCount:   2,
		QueueSize:     100,
	}

	asyncStorage, err := NewAsyncBatchStorage(logger, mockStorage, config)
	require.NoError(t, err)
	defer asyncStorage.Shutdown()

	ctx := context.Background()

	// Test storing correlations
	for i := 0; i < 25; i++ {
		result := &CorrelationResult{
			ID:         fmt.Sprintf("test-%d", i),
			Type:       "test",
			Confidence: 0.9,
		}
		err := asyncStorage.Store(ctx, result)
		assert.NoError(t, err)
	}

	// Wait for batch processing
	time.Sleep(200 * time.Millisecond)

	// Verify all items were processed
	stats := asyncStorage.GetStats()
	assert.GreaterOrEqual(t, stats.TotalProcessed, int64(25))
	assert.Equal(t, int64(0), stats.TotalDropped)
	assert.GreaterOrEqual(t, stats.TotalBatches, int64(1)) // At least 1 batch (items may be processed in 1 batch due to timing)
}

// TestAsyncBatchStorage_QueueOverflow tests behavior when queue is full
func TestAsyncBatchStorage_QueueOverflow(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockStorage := &testMockStorage{
		storeDelay: 100 * time.Millisecond, // Slow storage to cause backpressure
	}

	config := AsyncBatchConfig{
		BatchSize:     5,
		FlushInterval: 1 * time.Second,
		WorkerCount:   1,
		QueueSize:     10, // Small queue
	}

	asyncStorage, err := NewAsyncBatchStorage(logger, mockStorage, config)
	require.NoError(t, err)
	defer asyncStorage.Shutdown()

	ctx := context.Background()

	// Try to store more items than queue can handle
	var storeErrors int
	for i := 0; i < 20; i++ {
		result := &CorrelationResult{
			ID: fmt.Sprintf("overflow-%d", i),
		}
		if err := asyncStorage.Store(ctx, result); err != nil {
			storeErrors++
		}
	}

	// Some items should be dropped due to queue overflow
	assert.Greater(t, storeErrors, 0)

	stats := asyncStorage.GetStats()
	assert.Greater(t, stats.TotalDropped, int64(0))
}

// TestAsyncBatchStorage_GracefulShutdown tests graceful shutdown
func TestAsyncBatchStorage_GracefulShutdown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockStorage := &testMockStorage{}

	config := AsyncBatchConfig{
		BatchSize:       10,
		FlushInterval:   1 * time.Second,
		WorkerCount:     2,
		QueueSize:       100,
		ShutdownTimeout: 5 * time.Second,
	}

	asyncStorage, err := NewAsyncBatchStorage(logger, mockStorage, config)
	require.NoError(t, err)

	ctx := context.Background()

	// Store some items
	for i := 0; i < 5; i++ {
		result := &CorrelationResult{
			ID: fmt.Sprintf("shutdown-%d", i),
		}
		err := asyncStorage.Store(ctx, result)
		require.NoError(t, err)
	}

	// Shutdown should process remaining items
	err = asyncStorage.Shutdown()
	assert.NoError(t, err)

	// Verify items were processed before shutdown
	assert.Len(t, mockStorage.stored, 5)
}

// TestQueryCache_BasicOperations tests basic cache operations
func TestQueryCache_BasicOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := CacheConfig{
		MaxEntries:      100,
		TTL:             1 * time.Minute,
		CleanupInterval: 10 * time.Second,
		ShardCount:      4,
		EnableLRU:       true,
		EnableTTL:       true,
	}

	cache, err := NewQueryCache(logger, config)
	require.NoError(t, err)
	defer cache.Shutdown()

	ctx := context.Background()

	// Test Set and Get
	cache.Set(ctx, "key1", "value1", 0)
	value, found := cache.Get(ctx, "key1")
	assert.True(t, found)
	assert.Equal(t, "value1", value)

	// Test cache miss
	_, found = cache.Get(ctx, "nonexistent")
	assert.False(t, found)

	// Test invalidation
	cache.Invalidate(ctx, "key1")
	_, found = cache.Get(ctx, "key1")
	assert.False(t, found)

	// Verify metrics
	stats := cache.GetStats()
	assert.Equal(t, int64(1), stats.TotalHits)
	assert.Equal(t, int64(2), stats.TotalMisses) // nonexistent + invalidated key
}

// TestQueryCache_TTLExpiration tests TTL-based expiration
func TestQueryCache_TTLExpiration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := CacheConfig{
		MaxEntries:      100,
		TTL:             100 * time.Millisecond,
		CleanupInterval: 50 * time.Millisecond,
		ShardCount:      4,
		EnableLRU:       true,
		EnableTTL:       true,
	}

	cache, err := NewQueryCache(logger, config)
	require.NoError(t, err)
	defer cache.Shutdown()

	ctx := context.Background()

	// Set with short TTL
	cache.Set(ctx, "expiring", "value", 100*time.Millisecond)

	// Should be found immediately
	value, found := cache.Get(ctx, "expiring")
	assert.True(t, found)
	assert.Equal(t, "value", value)

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	_, found = cache.Get(ctx, "expiring")
	assert.False(t, found)
}

// TestQueryCache_LRUEviction tests LRU eviction when cache is full
func TestQueryCache_LRUEviction(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := CacheConfig{
		MaxEntries:      3, // Small cache to test eviction
		TTL:             1 * time.Hour,
		CleanupInterval: 1 * time.Hour,
		ShardCount:      1, // Single shard for predictable behavior
		EnableLRU:       true,
		EnableTTL:       false,
	}

	cache, err := NewQueryCache(logger, config)
	require.NoError(t, err)
	defer cache.Shutdown()

	ctx := context.Background()

	// Fill cache
	cache.Set(ctx, "key1", "value1", 0)
	cache.Set(ctx, "key2", "value2", 0)
	cache.Set(ctx, "key3", "value3", 0)

	// Access key1 to make it recently used
	cache.Get(ctx, "key1")

	// Add new item, should evict least recently used (key2 or key3)
	cache.Set(ctx, "key4", "value4", 0)

	// key1 should still be there (was accessed)
	value, found := cache.Get(ctx, "key1")
	assert.True(t, found)
	assert.Equal(t, "value1", value)

	// key4 should be there (just added)
	value, found = cache.Get(ctx, "key4")
	assert.True(t, found)
	assert.Equal(t, "value4", value)
}

// TestQueryCache_PatternInvalidation tests pattern-based invalidation
func TestQueryCache_PatternInvalidation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := DefaultCacheConfig()
	cache, err := NewQueryCache(logger, config)
	require.NoError(t, err)
	defer cache.Shutdown()

	ctx := context.Background()

	// Set multiple keys with pattern
	cache.Set(ctx, "user:1:profile", "profile1", 0)
	cache.Set(ctx, "user:1:settings", "settings1", 0)
	cache.Set(ctx, "user:2:profile", "profile2", 0)
	cache.Set(ctx, "post:1:content", "content1", 0)

	// Invalidate all user:1 keys
	invalidated := cache.InvalidatePattern(ctx, "user:1")
	assert.Equal(t, 2, invalidated)

	// user:1 keys should be gone
	_, found := cache.Get(ctx, "user:1:profile")
	assert.False(t, found)
	_, found = cache.Get(ctx, "user:1:settings")
	assert.False(t, found)

	// Other keys should remain
	_, found = cache.Get(ctx, "user:2:profile")
	assert.True(t, found)
	_, found = cache.Get(ctx, "post:1:content")
	assert.True(t, found)
}

// TestAtomicMetrics_Concurrency tests atomic metrics under concurrent access
func TestAtomicMetrics_Concurrency(t *testing.T) {
	metrics := NewAtomicMetrics()

	const (
		workers    = 100
		operations = 1000
	)

	var wg sync.WaitGroup

	// Concurrent increments
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < operations; i++ {
				metrics.IncrementEventsProcessed()
				metrics.IncrementCorrelationsFound()
				metrics.AddProcessingTime(time.Microsecond)
			}
		}()
	}

	wg.Wait()

	// Verify counts
	snapshot := metrics.GetSnapshot()
	assert.Equal(t, int64(workers*operations), snapshot.EventsProcessed)
	assert.Equal(t, int64(workers*operations), snapshot.CorrelationsFound)
	assert.Greater(t, snapshot.AvgProcessingTime.Nanoseconds(), int64(0))
}

// TestAtomicMetrics_Reset tests metric reset functionality
func TestAtomicMetrics_Reset(t *testing.T) {
	metrics := NewAtomicMetrics()

	// Add some metrics
	metrics.IncrementEventsProcessed()
	metrics.IncrementCorrelationsFound()
	metrics.UpdateEventQueueDepth(10)

	// Verify non-zero
	snapshot := metrics.GetSnapshot()
	assert.Greater(t, snapshot.EventsProcessed, int64(0))
	assert.Greater(t, snapshot.EventQueueDepth, int64(0))

	// Reset
	metrics.Reset()

	// Verify all zeros
	snapshot = metrics.GetSnapshot()
	assert.Equal(t, int64(0), snapshot.EventsProcessed)
	assert.Equal(t, int64(0), snapshot.CorrelationsFound)
	assert.Equal(t, int64(0), snapshot.EventQueueDepth)
}

// TestRingBuffer_BasicOperations tests ring buffer push/pop operations
func TestRingBuffer_BasicOperations(t *testing.T) {
	rb := NewRingBuffer(16)

	// Test push
	assert.True(t, rb.Push("item1"))
	assert.True(t, rb.Push("item2"))
	assert.True(t, rb.Push("item3"))
	assert.Equal(t, 3, rb.Size())

	// Test pop
	item, ok := rb.Pop()
	assert.True(t, ok)
	assert.Equal(t, "item1", item)

	item, ok = rb.Pop()
	assert.True(t, ok)
	assert.Equal(t, "item2", item)

	assert.Equal(t, 1, rb.Size())

	// Test empty pop
	rb.Pop() // Remove item3
	item, ok = rb.Pop()
	assert.False(t, ok)
	assert.Nil(t, item)
	assert.True(t, rb.IsEmpty())
}

// TestRingBuffer_Overflow tests ring buffer behavior when full
func TestRingBuffer_Overflow(t *testing.T) {
	rb := NewRingBuffer(4) // Small buffer

	// Fill buffer
	for i := 0; i < 4; i++ {
		assert.True(t, rb.Push(fmt.Sprintf("item%d", i)))
	}

	assert.True(t, rb.IsFull())

	// Try to push when full
	assert.False(t, rb.Push("overflow"))

	// Pop one and try again
	rb.Pop()
	assert.False(t, rb.IsFull())
	assert.True(t, rb.Push("new-item"))
}

// TestRingBuffer_Concurrent tests ring buffer under concurrent access
func TestRingBuffer_Concurrent(t *testing.T) {
	rb := NewRingBuffer(1024)

	const (
		producers = 10
		consumers = 10
		items     = 100
	)

	var wg sync.WaitGroup
	produced := &atomicCounter{}
	consumed := &atomicCounter{}

	// Producers
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < items; i++ {
				item := fmt.Sprintf("p%d-i%d", id, i)
				for !rb.Push(item) {
					time.Sleep(time.Microsecond)
				}
				produced.increment()
			}
		}(p)
	}

	// Consumers
	for c := 0; c < consumers; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for consumed.get() < int64(producers*items) {
				if item, ok := rb.Pop(); ok {
					consumed.increment()
					_ = item // Process item
				} else {
					time.Sleep(time.Microsecond)
				}
			}
		}()
	}

	wg.Wait()

	// Verify all items were produced and consumed
	assert.Equal(t, int64(producers*items), produced.get())
	assert.Equal(t, int64(producers*items), consumed.get())
	assert.True(t, rb.IsEmpty())
}

// Helper: atomic counter for testing
type atomicCounter struct {
	value int64
}

func (c *atomicCounter) increment() int64 {
	return atomic.AddInt64(&c.value, 1)
}

func (c *atomicCounter) get() int64 {
	return atomic.LoadInt64(&c.value)
}

// Helper: test mock storage implementation
type testMockStorage struct {
	mu         sync.Mutex
	stored     []*CorrelationResult
	storeDelay time.Duration
}

func (m *testMockStorage) Store(ctx context.Context, result *CorrelationResult) error {
	if m.storeDelay > 0 {
		time.Sleep(m.storeDelay)
	}
	m.mu.Lock()
	m.stored = append(m.stored, result)
	m.mu.Unlock()
	return nil
}

func (m *testMockStorage) GetRecent(ctx context.Context, limit int) ([]*CorrelationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.stored) <= limit {
		return m.stored, nil
	}
	return m.stored[len(m.stored)-limit:], nil
}

func (m *testMockStorage) GetByTraceID(ctx context.Context, traceID string) ([]*CorrelationResult, error) {
	return nil, nil
}

func (m *testMockStorage) GetByTimeRange(ctx context.Context, start, end time.Time) ([]*CorrelationResult, error) {
	return nil, nil
}

func (m *testMockStorage) GetByResource(ctx context.Context, resourceType, namespace, name string) ([]*CorrelationResult, error) {
	return nil, nil
}

func (m *testMockStorage) Cleanup(ctx context.Context, olderThan time.Duration) error {
	return nil
}

// TestIntegration_AllOptimizations tests all optimizations working together
func TestIntegration_AllOptimizations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	// Setup pooling
	pool := NewCorrelationResultPool(logger, 100)

	// Setup async storage
	mockStorage := &testMockStorage{}
	asyncConfig := DefaultAsyncBatchConfig()
	asyncStorage, err := NewAsyncBatchStorage(logger, mockStorage, asyncConfig)
	require.NoError(t, err)
	defer asyncStorage.Shutdown()

	// Setup caching
	cacheConfig := DefaultCacheConfig()
	cache, err := NewQueryCache(logger, cacheConfig)
	require.NoError(t, err)
	defer cache.Shutdown()

	// Setup atomic metrics
	metrics := NewAtomicMetrics()

	// Simulate correlation processing
	const numEvents = 100
	var wg sync.WaitGroup

	for i := 0; i < numEvents; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Check cache
			cacheKey := fmt.Sprintf("event-%d", id)
			if _, found := cache.Get(ctx, cacheKey); !found {
				// Get from pool
				result := pool.Get(ctx)
				result.ID = fmt.Sprintf("correlation-%d", id)
				result.Type = "test"
				result.Confidence = 0.9

				// Update metrics
				metrics.IncrementEventsProcessed()
				metrics.IncrementCorrelationsFound()

				// Store async
				err := asyncStorage.Store(ctx, result)
				assert.NoError(t, err)

				// Cache result
				cache.Set(ctx, cacheKey, result.ID, 1*time.Minute)

				// Return to pool
				pool.Put(ctx, result)
			} else {
				// Cache hit
				metrics.IncrementEventsProcessed()
			}
		}(i)
	}

	wg.Wait()

	// Wait for async processing
	time.Sleep(200 * time.Millisecond)

	// Verify results
	metricsSnapshot := metrics.GetSnapshot()
	assert.Equal(t, int64(numEvents), metricsSnapshot.EventsProcessed)
	assert.Greater(t, metricsSnapshot.CorrelationsFound, int64(0))

	poolStats := pool.GetStats()
	assert.Greater(t, poolStats.HitRate, 0.0)

	cacheStats := cache.GetStats()
	assert.Greater(t, cacheStats.TotalHits+cacheStats.TotalMisses, int64(0))

	storageStats := asyncStorage.GetStats()
	assert.Greater(t, storageStats.TotalProcessed, int64(0))
	assert.Equal(t, int64(0), storageStats.TotalDropped)
}
