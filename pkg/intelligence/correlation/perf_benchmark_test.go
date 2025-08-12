package correlation

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// BenchmarkAsyncBatchStorage_Throughput measures the throughput improvement of async batching
func BenchmarkAsyncBatchStorage_Throughput(b *testing.B) {
	scenarios := []struct {
		name      string
		async     bool
		batchSize int
		workers   int
		queueSize int
	}{
		{"Sync_Storage", false, 0, 0, 0},
		{"Async_Batch_10", true, 10, 2, 1000},
		{"Async_Batch_50", true, 50, 4, 5000},
		{"Async_Batch_100", true, 100, 4, 10000},
		{"Async_Batch_200", true, 200, 8, 20000},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			logger := zaptest.NewLogger(b)
			ctx := context.Background()

			// Create mock storage
			mockStorage := &benchmarkMockStorage{
				storeDelay: time.Microsecond * 100, // Simulate storage latency
			}

			var storage Storage
			if scenario.async {
				config := AsyncBatchConfig{
					BatchSize:     scenario.batchSize,
					FlushInterval: 50 * time.Millisecond,
					WorkerCount:   scenario.workers,
					QueueSize:     scenario.queueSize,
				}
				asyncStorage, err := NewAsyncBatchStorage(logger, mockStorage, config)
				if err != nil {
					b.Fatal(err)
				}
				defer asyncStorage.Shutdown()
				storage = asyncStorage
			} else {
				storage = mockStorage
			}

			b.ResetTimer()
			b.ReportAllocs()

			// Parallel store operations
			var wg sync.WaitGroup
			workers := 10
			itemsPerWorker := b.N / workers

			startTime := time.Now()

			for w := 0; w < workers; w++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for i := 0; i < itemsPerWorker; i++ {
						result := &CorrelationResult{
							ID:         fmt.Sprintf("corr-%d-%d", workerID, i),
							Type:       "test",
							Confidence: 0.9,
							Message:    "Test correlation",
						}
						if err := storage.Store(ctx, result); err != nil {
							b.Errorf("Store failed: %v", err)
						}
					}
				}(w)
			}

			wg.Wait()
			duration := time.Since(startTime)

			// Calculate throughput
			throughput := float64(b.N) / duration.Seconds()
			b.ReportMetric(throughput, "ops/sec")

			// Report async storage stats if applicable
			if asyncStorage, ok := storage.(*AsyncBatchStorage); ok {
				stats := asyncStorage.GetStats()
				b.ReportMetric(float64(stats.TotalProcessed), "processed")
				b.ReportMetric(float64(stats.TotalDropped), "dropped")
				b.ReportMetric(float64(stats.TotalBatches), "batches")
			}
		})
	}
}

// BenchmarkQueryCache_HitRate measures the cache effectiveness in reducing Neo4j load
func BenchmarkQueryCache_HitRate(b *testing.B) {
	scenarios := []struct {
		name      string
		cacheSize int
		ttl       time.Duration
		queryMix  float64 // Percentage of unique queries (0.1 = 10% unique, 90% repeated)
	}{
		{"NoCache", 0, 0, 0.5},
		{"SmallCache_10%Unique", 100, 5 * time.Minute, 0.1},
		{"SmallCache_50%Unique", 100, 5 * time.Minute, 0.5},
		{"MediumCache_10%Unique", 1000, 5 * time.Minute, 0.1},
		{"MediumCache_50%Unique", 1000, 5 * time.Minute, 0.5},
		{"LargeCache_10%Unique", 10000, 5 * time.Minute, 0.1},
		{"LargeCache_50%Unique", 10000, 5 * time.Minute, 0.5},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			logger := zaptest.NewLogger(b)
			ctx := context.Background()

			var cache *QueryCache
			var neo4jCalls int64

			if scenario.cacheSize > 0 {
				config := CacheConfig{
					MaxEntries:      scenario.cacheSize,
					TTL:             scenario.ttl,
					CleanupInterval: 1 * time.Minute,
					ShardCount:      16,
					EnableLRU:       true,
					EnableTTL:       true,
				}
				var err error
				cache, err = NewQueryCache(logger, config)
				if err != nil {
					b.Fatal(err)
				}
				defer cache.Shutdown()
			}

			// Generate query keys based on mix
			uniqueKeys := int(float64(b.N) * scenario.queryMix)
			if uniqueKeys < 1 {
				uniqueKeys = 1
			}

			b.ResetTimer()
			b.ReportAllocs()

			// Simulate query pattern
			for i := 0; i < b.N; i++ {
				// Generate key based on distribution
				var key string
				if i < uniqueKeys {
					key = fmt.Sprintf("query-%d", i)
				} else {
					// Repeat earlier keys
					key = fmt.Sprintf("query-%d", i%uniqueKeys)
				}

				// Try cache first
				var hit bool
				if cache != nil {
					_, hit = cache.Get(ctx, key)
				}

				if !hit {
					// Simulate Neo4j query
					neo4jCalls++
					result := simulateNeo4jQuery(key)

					// Store in cache
					if cache != nil {
						cache.Set(ctx, key, result, scenario.ttl)
					}
				}
			}

			// Calculate cache effectiveness
			neo4jReduction := float64(b.N-int(neo4jCalls)) / float64(b.N) * 100
			b.ReportMetric(neo4jReduction, "neo4j_reduction_%")
			b.ReportMetric(float64(neo4jCalls), "neo4j_calls")

			if cache != nil {
				stats := cache.GetStats()
				b.ReportMetric(stats.HitRate, "cache_hit_rate_%")
				b.ReportMetric(float64(stats.TotalEntries), "cache_entries")
			}
		})
	}
}

// BenchmarkAtomicMetrics_LockContention measures the reduction in lock contention
func BenchmarkAtomicMetrics_LockContention(b *testing.B) {
	scenarios := []struct {
		name      string
		atomic    bool
		workers   int
		operation string
	}{
		{"Mutex_1Worker", false, 1, "mixed"},
		{"Mutex_10Workers", false, 10, "mixed"},
		{"Mutex_100Workers", false, 100, "mixed"},
		{"Atomic_1Worker", true, 1, "mixed"},
		{"Atomic_10Workers", true, 10, "mixed"},
		{"Atomic_100Workers", true, 100, "mixed"},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			if scenario.atomic {
				benchmarkAtomicMetrics(b, scenario.workers)
			} else {
				benchmarkMutexMetrics(b, scenario.workers)
			}
		})
	}
}

// benchmarkAtomicMetrics tests atomic metric operations
func benchmarkAtomicMetrics(b *testing.B, workers int) {
	metrics := NewAtomicMetrics()

	b.ResetTimer()
	b.ReportAllocs()

	var wg sync.WaitGroup
	opsPerWorker := b.N / workers

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				// Simulate mixed operations
				switch i % 5 {
				case 0:
					metrics.IncrementEventsProcessed()
				case 1:
					metrics.IncrementCorrelationsFound()
				case 2:
					metrics.IncrementStorageProcessed()
				case 3:
					metrics.UpdateEventQueueDepth(1)
					metrics.UpdateEventQueueDepth(-1)
				case 4:
					metrics.AddProcessingTime(time.Microsecond)
				}
			}
		}()
	}

	wg.Wait()
}

// benchmarkMutexMetrics tests mutex-based metric operations
func benchmarkMutexMetrics(b *testing.B, workers int) {
	metrics := &mutexMetrics{}

	b.ResetTimer()
	b.ReportAllocs()

	var wg sync.WaitGroup
	opsPerWorker := b.N / workers

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				// Simulate mixed operations
				switch i % 5 {
				case 0:
					metrics.IncrementEventsProcessed()
				case 1:
					metrics.IncrementCorrelationsFound()
				case 2:
					metrics.IncrementStorageProcessed()
				case 3:
					metrics.UpdateEventQueueDepth(1)
					metrics.UpdateEventQueueDepth(-1)
				case 4:
					metrics.AddProcessingTime(time.Microsecond)
				}
			}
		}()
	}

	wg.Wait()
}

// BenchmarkPooling_GCReduction validates 60% GC reduction claim
func BenchmarkPooling_GCReduction(b *testing.B) {
	scenarios := []struct {
		name        string
		usePool     bool
		parallelism int
	}{
		{"NoPool_Serial", false, 1},
		{"NoPool_Parallel_10", false, 10},
		{"NoPool_Parallel_100", false, 100},
		{"WithPool_Serial", true, 1},
		{"WithPool_Parallel_10", true, 10},
		{"WithPool_Parallel_100", true, 100},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			logger := zaptest.NewLogger(b)
			ctx := context.Background()

			var pool *CorrelationResultPool
			if scenario.usePool {
				pool = NewCorrelationResultPool(logger, 1000)
			}

			// Measure initial GC stats
			var initialStats runtime.MemStats
			runtime.ReadMemStats(&initialStats)

			b.ResetTimer()
			b.ReportAllocs()

			// Run correlation processing
			var wg sync.WaitGroup
			opsPerWorker := b.N / scenario.parallelism

			for w := 0; w < scenario.parallelism; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < opsPerWorker; i++ {
						var result *CorrelationResult

						if pool != nil {
							// Use pool
							result = pool.Get(ctx)
							populateCorrelationResult(pool, result, i)
							processCorrelationResult(result)
							pool.Put(ctx, result)
						} else {
							// Allocate new
							result = createCorrelationResult(i)
							processCorrelationResult(result)
						}
					}
				}()
			}

			wg.Wait()

			// Force GC and measure final stats
			runtime.GC()
			var finalStats runtime.MemStats
			runtime.ReadMemStats(&finalStats)

			// Calculate GC metrics
			gcPauses := finalStats.PauseTotalNs - initialStats.PauseTotalNs
			numGCs := finalStats.NumGC - initialStats.NumGC
			allocations := finalStats.Mallocs - initialStats.Mallocs

			b.ReportMetric(float64(gcPauses)/1e6, "gc_pause_ms")
			b.ReportMetric(float64(numGCs), "gc_runs")
			b.ReportMetric(float64(allocations), "allocations")

			if pool != nil {
				stats := pool.GetStats()
				b.ReportMetric(stats.HitRate*100, "pool_hit_rate_%")
			}
		})
	}
}

// BenchmarkEndToEnd_FullOptimizations tests all optimizations together
func BenchmarkEndToEnd_FullOptimizations(b *testing.B) {
	scenarios := []struct {
		name          string
		pooling       bool
		asyncStorage  bool
		caching       bool
		atomicMetrics bool
	}{
		{"Baseline_NoOptimizations", false, false, false, false},
		{"WithPooling", true, false, false, false},
		{"WithAsyncStorage", false, true, false, false},
		{"WithCaching", false, false, true, false},
		{"WithAtomicMetrics", false, false, false, true},
		{"AllOptimizations", true, true, true, true},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			logger := zaptest.NewLogger(b)
			ctx := context.Background()

			// Setup components based on scenario
			var pool *CorrelationResultPool
			if scenario.pooling {
				pool = NewCorrelationResultPool(logger, 1000)
			}

			var storage Storage = &benchmarkMockStorage{storeDelay: time.Microsecond * 50}
			if scenario.asyncStorage {
				config := DefaultAsyncBatchConfig()
				asyncStorage, err := NewAsyncBatchStorage(logger, storage, config)
				if err != nil {
					b.Fatal(err)
				}
				defer asyncStorage.Shutdown()
				storage = asyncStorage
			}

			var cache *QueryCache
			if scenario.caching {
				config := DefaultCacheConfig()
				var err error
				cache, err = NewQueryCache(logger, config)
				if err != nil {
					b.Fatal(err)
				}
				defer cache.Shutdown()
			}

			var metrics interface{}
			if scenario.atomicMetrics {
				metrics = NewAtomicMetrics()
			} else {
				metrics = &mutexMetrics{}
			}

			b.ResetTimer()
			b.ReportAllocs()

			// Simulate full correlation processing pipeline
			var wg sync.WaitGroup
			workers := 10
			eventsPerWorker := b.N / workers

			startTime := time.Now()

			for w := 0; w < workers; w++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()

					for i := 0; i < eventsPerWorker; i++ {
						// Create event
						event := &domain.UnifiedEvent{
							ID:   fmt.Sprintf("event-%d-%d", workerID, i),
							Type: domain.EventTypeKubernetes,
						}

						// Check cache
						cacheKey := fmt.Sprintf("correlation-%s", event.ID)
						var cached bool
						if cache != nil {
							_, cached = cache.Get(ctx, cacheKey)
						}

						if !cached {
							// Process correlation
							var result *CorrelationResult
							if pool != nil {
								result = pool.Get(ctx)
								populateCorrelationResult(pool, result, i)
							} else {
								result = createCorrelationResult(i)
							}

							// Update metrics
							switch m := metrics.(type) {
							case *AtomicMetrics:
								m.IncrementEventsProcessed()
								m.IncrementCorrelationsFound()
							case *mutexMetrics:
								m.IncrementEventsProcessed()
								m.IncrementCorrelationsFound()
							}

							// Store result
							if err := storage.Store(ctx, result); err != nil {
								b.Errorf("Store failed: %v", err)
							}

							// Cache result
							if cache != nil {
								cache.Set(ctx, cacheKey, result, 5*time.Minute)
							}

							// Return to pool
							if pool != nil {
								pool.Put(ctx, result)
							}
						}
					}
				}(w)
			}

			wg.Wait()
			duration := time.Since(startTime)

			// Report performance metrics
			throughput := float64(b.N) / duration.Seconds()
			b.ReportMetric(throughput, "events/sec")

			// Report optimization-specific metrics
			if pool != nil {
				stats := pool.GetStats()
				b.ReportMetric(stats.HitRate*100, "pool_hit_rate_%")
			}

			if asyncStorage, ok := storage.(*AsyncBatchStorage); ok {
				stats := asyncStorage.GetStats()
				b.ReportMetric(float64(stats.TotalBatches), "storage_batches")
			}

			if cache != nil {
				stats := cache.GetStats()
				b.ReportMetric(stats.HitRate, "cache_hit_rate_%")
			}
		})
	}
}

// Helper: benchmark mock storage implementation
type benchmarkMockStorage struct {
	mu         sync.Mutex
	stored     []*CorrelationResult
	storeDelay time.Duration
}

func (m *benchmarkMockStorage) Store(ctx context.Context, result *CorrelationResult) error {
	if m.storeDelay > 0 {
		time.Sleep(m.storeDelay)
	}
	m.mu.Lock()
	m.stored = append(m.stored, result)
	m.mu.Unlock()
	return nil
}

func (m *benchmarkMockStorage) GetRecent(ctx context.Context, limit int) ([]*CorrelationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.stored) <= limit {
		return m.stored, nil
	}
	return m.stored[len(m.stored)-limit:], nil
}

func (m *benchmarkMockStorage) GetByTraceID(ctx context.Context, traceID string) ([]*CorrelationResult, error) {
	return nil, nil
}

func (m *benchmarkMockStorage) GetByTimeRange(ctx context.Context, start, end time.Time) ([]*CorrelationResult, error) {
	return nil, nil
}

func (m *benchmarkMockStorage) GetByResource(ctx context.Context, resourceType, namespace, name string) ([]*CorrelationResult, error) {
	return nil, nil
}

func (m *benchmarkMockStorage) Cleanup(ctx context.Context, olderThan time.Duration) error {
	return nil
}

// Helper: mutex-based metrics for comparison
type mutexMetrics struct {
	mu                sync.RWMutex
	eventsProcessed   int64
	correlationsFound int64
	storageProcessed  int64
	eventQueueDepth   int64
	processingTimeNs  int64
	processingCount   int64
}

func (m *mutexMetrics) IncrementEventsProcessed() {
	m.mu.Lock()
	m.eventsProcessed++
	m.mu.Unlock()
}

func (m *mutexMetrics) IncrementCorrelationsFound() {
	m.mu.Lock()
	m.correlationsFound++
	m.mu.Unlock()
}

func (m *mutexMetrics) IncrementStorageProcessed() {
	m.mu.Lock()
	m.storageProcessed++
	m.mu.Unlock()
}

func (m *mutexMetrics) UpdateEventQueueDepth(delta int64) {
	m.mu.Lock()
	m.eventQueueDepth += delta
	m.mu.Unlock()
}

func (m *mutexMetrics) AddProcessingTime(duration time.Duration) {
	m.mu.Lock()
	m.processingTimeNs += duration.Nanoseconds()
	m.processingCount++
	m.mu.Unlock()
}

// Helper functions
func createCorrelationResult(id int) *CorrelationResult {
	return &CorrelationResult{
		ID:         fmt.Sprintf("correlation-%d", id),
		Type:       "test",
		Confidence: 0.9,
		Message:    "Test correlation",
		Events:     []string{"event1", "event2", "event3"},
		Related: []*domain.UnifiedEvent{
			{ID: fmt.Sprintf("related-%d", id)},
		},
		StartTime: time.Now(),
		EndTime:   time.Now().Add(time.Second),
	}
}

func populateCorrelationResult(pool *CorrelationResultPool, result *CorrelationResult, id int) {
	result.ID = fmt.Sprintf("correlation-%d", id)
	result.Type = "test"
	result.Confidence = 0.9
	result.Message = "Test correlation"
	if pool != nil {
		result.Events = pool.GetStringSlice()
	} else {
		result.Events = make([]string, 0, 3)
	}
	result.Events = append(result.Events, "event1", "event2", "event3")
	result.StartTime = time.Now()
	result.EndTime = time.Now().Add(time.Second)
}

func simulateNeo4jQuery(key string) interface{} {
	// Simulate query latency
	time.Sleep(time.Microsecond * 500)
	return fmt.Sprintf("result-for-%s", key)
}
