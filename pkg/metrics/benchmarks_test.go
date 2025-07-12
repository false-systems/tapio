package metrics

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Comprehensive benchmarks for the Prometheus metrics package
// These benchmarks demonstrate enterprise-grade performance characteristics

// BenchmarkCounter benchmarks the type-safe counter implementation
func BenchmarkCounter(b *testing.B) {
	counter := NewCounter[int64](
		"test_counter",
		"Test counter for benchmarking",
		Labels{"benchmark": "true"},
		CounterConstraints[int64]{
			MinValue: 0,
			MaxValue: 1000000000,
		},
	)

	b.ResetTimer()
	b.ReportAllocs()

	b.Run("Sequential", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			counter.Add(1)
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				counter.Add(1)
			}
		})
	})

	b.Run("Batch", func(b *testing.B) {
		batchSize := 100
		b.ResetTimer()
		for i := 0; i < b.N; i += batchSize {
			for j := 0; j < batchSize && i+j < b.N; j++ {
				counter.Add(1)
			}
		}
	})
}

// BenchmarkGauge benchmarks the type-safe gauge implementation
func BenchmarkGauge(b *testing.B) {
	gauge := NewGauge[float64](
		"test_gauge",
		"Test gauge for benchmarking",
		Labels{"benchmark": "true"},
		GaugeConstraints[float64]{
			MinValue: -1000.0,
			MaxValue: 1000.0,
		},
	)

	b.ResetTimer()
	b.ReportAllocs()

	b.Run("Set", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			gauge.Set(float64(i % 1000))
		}
	})

	b.Run("Add", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			gauge.Add(1.0)
		}
	})

	b.Run("Parallel_Set", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			value := 0.0
			for pb.Next() {
				value += 1.0
				gauge.Set(value)
			}
		})
	})

	b.Run("Read_Heavy", func(b *testing.B) {
		// 90% reads, 10% writes
		b.RunParallel(func(pb *testing.PB) {
			writeCounter := 0
			for pb.Next() {
				writeCounter++
				if writeCounter%10 == 0 {
					gauge.Set(float64(writeCounter))
				} else {
					_ = gauge.Value()
				}
			}
		})
	})
}

// BenchmarkHistogram benchmarks the type-safe histogram implementation
func BenchmarkHistogram(b *testing.B) {
	buckets := []float64{0.1, 0.5, 1.0, 2.5, 5.0, 10.0}
	histogram := NewHistogram[float64](
		"test_histogram",
		"Test histogram for benchmarking",
		Labels{"benchmark": "true"},
		buckets,
		HistogramConstraints[float64]{
			MinValue: 0.0,
			MaxValue: 10.0,
		},
	)

	b.ResetTimer()
	b.ReportAllocs()

	b.Run("Observe", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			histogram.Observe(float64(i%10) + 0.5)
		}
	})

	b.Run("Parallel_Observe", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			value := 0.0
			for pb.Next() {
				value += 0.1
				if value > 10.0 {
					value = 0.1
				}
				histogram.Observe(value)
			}
		})
	})

	b.Run("High_Frequency", func(b *testing.B) {
		// Simulate high-frequency observations
		values := []float64{0.1, 0.5, 1.0, 2.5, 5.0, 10.0}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			histogram.Observe(values[i%len(values)])
		}
	})
}

// BenchmarkSummary benchmarks the type-safe summary implementation
func BenchmarkSummary(b *testing.B) {
	quantiles := []float64{0.5, 0.9, 0.95, 0.99}
	summary := NewSummary[float64](
		"test_summary",
		"Test summary for benchmarking",
		Labels{"benchmark": "true"},
		quantiles,
		SummaryConstraints[float64]{
			MinValue:   0.0,
			MaxValue:   1000.0,
			MaxAge:     time.Minute,
			AgeBuckets: 5,
		},
	)

	b.ResetTimer()
	b.ReportAllocs()

	b.Run("Observe", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			summary.Observe(float64(i % 1000))
		}
	})

	b.Run("Parallel_Observe", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			value := 0.0
			for pb.Next() {
				value += 1.0
				summary.Observe(value)
			}
		})
	})

	b.Run("Quantile_Calculation", func(b *testing.B) {
		// Pre-populate with data
		for i := 0; i < 1000; i++ {
			summary.Observe(float64(i))
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = summary.GetQuantile(0.95)
		}
	})
}

// BenchmarkMetricEventPublisher benchmarks the observer pattern implementation
func BenchmarkMetricEventPublisher(b *testing.B) {
	logger := slog.Default()
	config := PublisherConfig{
		DefaultBufferSize:     10000,
		DefaultFlushInterval:  time.Millisecond,
		DefaultFlushThreshold: 1000,
		EnableBatching:        true,
		WorkerPoolSize:        4,
	}

	publisher := NewMetricEventPublisher[*Counter[int64]](config, logger)
	defer publisher.Close(context.Background())

	// Create test observers
	numObservers := 5
	for i := 0; i < numObservers; i++ {
		observer := &benchmarkObserver{id: i}
		publisher.Subscribe(observer)
	}

	// Create test counter for events
	counter := NewCounter[int64](
		"benchmark_counter",
		"Counter for benchmark events",
		Labels{},
		CounterConstraints[int64]{},
	)

	b.ResetTimer()
	b.ReportAllocs()

	b.Run("Single_Event", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			event := MetricEvent[*Counter[int64]]{
				Type:      EventTypeUpdated,
				Metric:    counter,
				NewValue:  int64(i),
				Timestamp: time.Now(),
			}
			publisher.Publish(context.Background(), event)
		}
	})

	b.Run("Batch_Events", func(b *testing.B) {
		batchSize := 100
		events := make([]MetricEvent[*Counter[int64]], batchSize)
		for i := 0; i < batchSize; i++ {
			events[i] = MetricEvent[*Counter[int64]]{
				Type:      EventTypeUpdated,
				Metric:    counter,
				NewValue:  int64(i),
				Timestamp: time.Now(),
			}
		}

		b.ResetTimer()
		for i := 0; i < b.N; i += batchSize {
			publisher.PublishBatch(context.Background(), events)
		}
	})

	b.Run("High_Frequency", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			eventCount := 0
			for pb.Next() {
				event := MetricEvent[*Counter[int64]]{
					Type:      EventTypeUpdated,
					Metric:    counter,
					NewValue:  int64(eventCount),
					Timestamp: time.Now(),
				}
				publisher.Publish(context.Background(), event)
				eventCount++
			}
		})
	})
}

// BenchmarkMetricCollector benchmarks the metric collection with rate limiting
func BenchmarkMetricCollector(b *testing.B) {
	logger := slog.Default()
	
	config := CollectorConfig[*Counter[int64]]{
		CollectorName:      "benchmark_collector",
		CollectionInterval: time.Millisecond,
		Timeout:           time.Second,
		BufferSize:        10000,
		BatchSize:         100,
		MaxConcurrency:    10,
		CollectionFunc: func(ctx context.Context, opts CollectionOptions) ([]*Counter[int64], error) {
			// Simulate metric collection
			counter := NewCounter[int64](
				"collected_metric",
				"Collected metric",
				Labels{},
				CounterConstraints[int64]{},
			)
			return []*Counter[int64]{counter}, nil
		},
		RateLimit: RateLimitSettings{
			RequestsPerSecond: 1000,
			BurstSize:        100,
			Enabled:          true,
		},
		Backpressure: BackpressureSettings{
			Strategy:      BackpressureStrategyDrop,
			BufferSize:    5000,
			DropThreshold: 0.8,
		},
	}

	collector, err := NewPrometheusMetricCollector(config, logger)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}
	defer collector.Close(context.Background())

	b.ResetTimer()
	b.ReportAllocs()

	b.Run("Single_Collection", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			resultCh, err := collector.Collect(ctx, CollectionOptions{
				Timeout:    time.Second,
				MaxMetrics: 1000,
			})
			if err != nil {
				cancel()
				continue
			}

			// Consume one result
			select {
			case <-resultCh:
			case <-ctx.Done():
			}
			cancel()
		}
	})

	b.Run("Batch_Collection", func(b *testing.B) {
		batchSize := 50
		b.ResetTimer()
		for i := 0; i < b.N; i += batchSize {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			batchCh, err := collector.CollectBatch(ctx, batchSize, CollectionOptions{
				Timeout:    time.Second,
				MaxMetrics: 1000,
			})
			if err != nil {
				cancel()
				continue
			}

			// Consume one batch
			select {
			case <-batchCh:
			case <-ctx.Done():
			}
			cancel()
		}
	})

	b.Run("Concurrent_Collection", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				resultCh, err := collector.Collect(ctx, CollectionOptions{
					Timeout:    time.Second,
					MaxMetrics: 100,
				})
				if err != nil {
					cancel()
					continue
				}

				// Consume result
				select {
				case <-resultCh:
				case <-ctx.Done():
				}
				cancel()
			}
		})
	})
}

// BenchmarkMetricStreamer benchmarks the memory-efficient streaming implementation
func BenchmarkMetricStreamer(b *testing.B) {
	logger := slog.Default()
	
	config := StreamerConfig{
		WorkerCount:         4,
		WorkerQueueSize:     10000,
		DefaultBufferSize:   5000,
		DefaultFlushInterval: time.Millisecond,
		MaxStreams:          100,
	}

	streamer := NewPrometheusMetricStreamer[*Counter[int64]](config, logger)
	defer streamer.Close(context.Background())

	b.ResetTimer()
	b.ReportAllocs()

	b.Run("Stream_Creation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			streamCh, err := streamer.StartStream(ctx, StreamOptions{
				BufferSize:    1000,
				FlushInterval: time.Millisecond,
				Compression:   false,
			})
			if err != nil {
				cancel()
				continue
			}

			// Stop stream immediately
			streamID := "test-stream-" + string(rune(i))
			streamer.StopStream(streamID)
			cancel()

			// Drain channel
			select {
			case <-streamCh:
			default:
			}
		}
	})

	b.Run("Stream_Throughput", func(b *testing.B) {
		ctx := context.Background()
		streamCh, err := streamer.StartStream(ctx, StreamOptions{
			BufferSize:    10000,
			FlushInterval: 10 * time.Millisecond,
			Compression:   false,
		})
		if err != nil {
			b.Fatalf("Failed to start stream: %v", err)
		}

		// Create test counter
		counter := NewCounter[int64](
			"stream_counter",
			"Counter for streaming",
			Labels{},
			CounterConstraints[int64]{},
		)

		b.ResetTimer()
		
		// Send metrics to stream
		go func() {
			for i := 0; i < b.N; i++ {
				counter.Add(1)
			}
		}()

		// Consume stream results
		consumed := 0
		for consumed < b.N {
			select {
			case result := <-streamCh:
				if result.Error == nil {
					consumed += len(result.Metrics)
				}
			case <-time.After(time.Second):
				b.Fatalf("Stream timeout")
			}
		}
	})

	b.Run("Compression_Overhead", func(b *testing.B) {
		ctx := context.Background()
		
		// Without compression
		b.Run("NoCompression", func(b *testing.B) {
			streamCh, err := streamer.StartStream(ctx, StreamOptions{
				BufferSize:    1000,
				FlushInterval: time.Millisecond,
				Compression:   false,
			})
			if err != nil {
				b.Fatalf("Failed to start stream: %v", err)
			}

			counter := NewCounter[int64](
				"no_compression_counter",
				"Counter without compression",
				Labels{},
				CounterConstraints[int64]{},
			)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				counter.Add(1)
			}

			// Consume results
			for i := 0; i < b.N/100; i++ { // Expect batched results
				select {
				case <-streamCh:
				case <-time.After(time.Second):
					return
				}
			}
		})

		// With compression
		b.Run("WithCompression", func(b *testing.B) {
			streamCh, err := streamer.StartStream(ctx, StreamOptions{
				BufferSize:    1000,
				FlushInterval: time.Millisecond,
				Compression:   true,
			})
			if err != nil {
				b.Fatalf("Failed to start stream: %v", err)
			}

			counter := NewCounter[int64](
				"compression_counter",
				"Counter with compression",
				Labels{},
				CounterConstraints[int64]{},
			)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				counter.Add(1)
			}

			// Consume results
			for i := 0; i < b.N/100; i++ { // Expect batched results
				select {
				case <-streamCh:
				case <-time.After(time.Second):
					return
				}
			}
		})
	})
}

// BenchmarkMetricFactory benchmarks the factory pattern implementation
func BenchmarkMetricFactory(b *testing.B) {
	logger := slog.Default()
	config := FactoryConfig{
		DefaultTimeout:         time.Second,
		DefaultShutdownTimeout: 5 * time.Second,
		MaxClients:            1000,
		EnableMetrics:         true,
	}

	factory := NewPrometheusMetricFactory(config, logger)
	defer factory.Shutdown(context.Background())

	b.ResetTimer()
	b.ReportAllocs()

	b.Run("Create_Push_Client", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			pushConfig := PushClientConfig{
				GatewayURL: "http://localhost:9091",
				JobName:    "benchmark_job",
				Instance:   "benchmark_instance",
				Timeout:    time.Second,
			}

			client, err := factory.CreatePushClient(pushConfig)
			if err != nil {
				b.Fatalf("Failed to create push client: %v", err)
			}

			// Clean up
			client.Close(context.Background())
		}
	})

	b.Run("Create_Pull_Client", func(b *testing.B) {
		basePort := 19090
		for i := 0; i < b.N; i++ {
			pullConfig := PullClientConfig{
				ListenAddress: "localhost",
				ListenPort:    basePort + i,
				MetricsPath:   "/metrics",
			}

			client, err := factory.CreatePullClient(pullConfig)
			if err != nil {
				// Port might be in use, skip
				continue
			}

			// Clean up
			client.Close(context.Background())
		}
	})

	b.Run("Client_Registration", func(b *testing.B) {
		clients := make([]interface{}, 0, b.N)
		defer func() {
			for _, client := range clients {
				if closer, ok := client.(interface{ Close(context.Context) error }); ok {
					closer.Close(context.Background())
				}
			}
		}()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pushConfig := PushClientConfig{
				GatewayURL: "http://localhost:9091",
				JobName:    "benchmark_job",
				Instance:   "benchmark_instance",
				Timeout:    time.Second,
			}

			client, err := factory.CreatePushClient(pushConfig)
			if err != nil {
				continue
			}
			clients = append(clients, client)
		}
	})
}

// BenchmarkMemoryEfficiency tests memory usage patterns
func BenchmarkMemoryEfficiency(b *testing.B) {
	b.Run("Counter_Memory_Usage", func(b *testing.B) {
		counters := make([]*Counter[int64], b.N)
		
		b.ResetTimer()
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			counters[i] = NewCounter[int64](
				"memory_counter",
				"Counter for memory testing",
				Labels{"index": string(rune(i))},
				CounterConstraints[int64]{},
			)
			counters[i].Add(int64(i))
		}

		// Keep references to prevent GC
		runtime.KeepAlive(counters)
	})

	b.Run("Publisher_Memory_Usage", func(b *testing.B) {
		config := PublisherConfig{
			DefaultBufferSize:     1000,
			DefaultFlushInterval:  time.Second,
			EnableBatching:        true,
			WorkerPoolSize:        2,
		}

		publisher := NewMetricEventPublisher[*Counter[int64]](config, slog.Default())
		defer publisher.Close(context.Background())

		counter := NewCounter[int64](
			"memory_test_counter",
			"Counter for memory test",
			Labels{},
			CounterConstraints[int64]{},
		)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			event := MetricEvent[*Counter[int64]]{
				Type:      EventTypeUpdated,
				Metric:    counter,
				NewValue:  int64(i),
				Timestamp: time.Now(),
			}
			publisher.Publish(context.Background(), event)
		}
	})

	b.Run("Garbage_Collection_Pressure", func(b *testing.B) {
		// Test GC pressure under high allocation rates
		counters := make([]*Counter[int64], 0, 1000)
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if i%1000 == 0 {
				// Periodically clear to trigger GC
				counters = counters[:0]
				runtime.GC()
			}

			counter := NewCounter[int64](
				"gc_test_counter",
				"Counter for GC testing",
				Labels{"batch": string(rune(i / 1000))},
				CounterConstraints[int64]{},
			)
			counter.Add(1)
			counters = append(counters, counter)
		}

		runtime.KeepAlive(counters)
	})
}

// BenchmarkConcurrentAccess tests performance under high concurrency
func BenchmarkConcurrentAccess(b *testing.B) {
	counter := NewCounter[int64](
		"concurrent_counter",
		"Counter for concurrency testing",
		Labels{},
		CounterConstraints[int64]{},
	)

	gauge := NewGauge[float64](
		"concurrent_gauge",
		"Gauge for concurrency testing",
		Labels{},
		GaugeConstraints[float64]{MinValue: -1000, MaxValue: 1000},
	)

	b.Run("Counter_High_Concurrency", func(b *testing.B) {
		b.SetParallelism(runtime.NumCPU() * 4)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				counter.Add(1)
			}
		})
	})

	b.Run("Gauge_High_Concurrency", func(b *testing.B) {
		b.SetParallelism(runtime.NumCPU() * 4)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			value := 0.0
			for pb.Next() {
				value += 1.0
				if value > 1000 {
					value = -1000
				}
				gauge.Set(value)
			}
		})
	})

	b.Run("Mixed_Workload", func(b *testing.B) {
		b.SetParallelism(runtime.NumCPU() * 2)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			operation := 0
			gaugeValue := 0.0
			for pb.Next() {
				operation++
				switch operation % 4 {
				case 0:
					counter.Add(1)
				case 1:
					_ = counter.Value()
				case 2:
					gaugeValue += 1.0
					gauge.Set(gaugeValue)
				case 3:
					_ = gauge.Value()
				}
			}
		})
	})
}

// Benchmark observer implementation for testing
type benchmarkObserver struct {
	id           int
	eventCount   int64
	errorCount   int64
	mu           sync.Mutex
}

func (bo *benchmarkObserver) OnMetricCreated(ctx context.Context, metric *Counter[int64]) error {
	bo.mu.Lock()
	bo.eventCount++
	bo.mu.Unlock()
	return nil
}

func (bo *benchmarkObserver) OnMetricUpdated(ctx context.Context, metric *Counter[int64], oldValue, newValue interface{}) error {
	bo.mu.Lock()
	bo.eventCount++
	bo.mu.Unlock()
	return nil
}

func (bo *benchmarkObserver) OnMetricDeleted(ctx context.Context, metric *Counter[int64]) error {
	bo.mu.Lock()
	bo.eventCount++
	bo.mu.Unlock()
	return nil
}

func (bo *benchmarkObserver) OnError(ctx context.Context, err error, metric *Counter[int64]) error {
	bo.mu.Lock()
	bo.errorCount++
	bo.mu.Unlock()
	return nil
}

func (bo *benchmarkObserver) GetID() string {
	return "benchmark_observer_" + string(rune(bo.id))
}

func (bo *benchmarkObserver) GetPriority() ObserverPriority {
	return ObserverPriorityMedium
}

// Performance regression tests
func BenchmarkPerformanceRegression(b *testing.B) {
	// These benchmarks help detect performance regressions over time
	
	b.Run("Counter_Baseline", func(b *testing.B) {
		counter := NewCounter[int64](
			"baseline_counter",
			"Baseline counter",
			Labels{},
			CounterConstraints[int64]{},
		)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			counter.Add(1)
		}

		// Expected: ~10ns per operation on modern hardware
		if testing.Verbose() {
			nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			b.Logf("Counter.Add: %.2f ns/op", nsPerOp)
		}
	})

	b.Run("Publisher_Baseline", func(b *testing.B) {
		config := PublisherConfig{
			DefaultBufferSize:     10000,
			DefaultFlushInterval:  time.Second,
			WorkerPoolSize:        1,
		}

		publisher := NewMetricEventPublisher[*Counter[int64]](config, slog.Default())
		defer publisher.Close(context.Background())

		counter := NewCounter[int64](
			"baseline_publisher_counter",
			"Baseline publisher counter",
			Labels{},
			CounterConstraints[int64]{},
		)

		event := MetricEvent[*Counter[int64]]{
			Type:      EventTypeUpdated,
			Metric:    counter,
			NewValue:  int64(1),
			Timestamp: time.Now(),
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			publisher.Publish(context.Background(), event)
		}

		// Expected: ~100ns per operation including buffering
		if testing.Verbose() {
			nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			b.Logf("Publisher.Publish: %.2f ns/op", nsPerOp)
		}
	})
}

// Utility function to run benchmarks with memory profiling
func BenchmarkWithMemProfile(b *testing.B) {
	if !testing.Short() {
		// Only run comprehensive memory profiling in long tests
		b.Run("MemoryProfile", func(b *testing.B) {
			// Force GC before starting
			runtime.GC()
			
			var m1, m2 runtime.MemStats
			runtime.ReadMemStats(&m1)

			// Run a representative workload
			counter := NewCounter[int64](
				"profile_counter",
				"Counter for memory profiling",
				Labels{},
				CounterConstraints[int64]{},
			)

			for i := 0; i < b.N; i++ {
				counter.Add(1)
			}

			runtime.GC()
			runtime.ReadMemStats(&m2)

			if testing.Verbose() {
				b.Logf("Memory allocated per operation: %d bytes", 
					(m2.TotalAlloc-m1.TotalAlloc)/uint64(b.N))
				b.Logf("Heap objects per operation: %.2f",
					float64(m2.HeapObjects-m1.HeapObjects)/float64(b.N))
			}
		})
	}
}