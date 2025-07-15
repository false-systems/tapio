package benchmarks

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/otel/core"
	"github.com/yairfalse/tapio/pkg/otel/domain"
	"github.com/yairfalse/tapio/pkg/otel/encoding"
)

// Comprehensive benchmarks for OTEL components with PGO optimization support
// Run with: go test -bench=. -benchmem -cpuprofile=cpu.prof -memprofile=mem.prof

// BenchmarkTraceAggregateCreation benchmarks trace aggregate creation performance
func BenchmarkTraceAggregateCreation(b *testing.B) {
	serviceName := "benchmark-service"
	spanName := "benchmark-span"
	attributes := map[string]string{
		"service.name":    serviceName,
		"service.version": "1.0.0",
		"environment":     "benchmark",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		traceID := domain.TraceID{
			High: uint64(i),
			Low:  uint64(i << 32),
		}

		aggregate, err := domain.NewTraceAggregate[string](
			traceID,
			serviceName,
			spanName,
			domain.SpanKindServer,
			attributes,
			nil, // correlation service
			nil, // sampling service
		)

		if err != nil {
			b.Fatalf("Failed to create trace aggregate: %v", err)
		}

		// Prevent compiler optimization
		_ = aggregate
	}
}

// BenchmarkSpanCreation benchmarks individual span creation within traces
func BenchmarkSpanCreation(b *testing.B) {
	// Pre-create trace aggregate
	traceID := domain.TraceID{High: 1, Low: 1}
	aggregate, err := domain.NewTraceAggregate[string](
		traceID,
		"benchmark-service",
		"root-span",
		domain.SpanKindServer,
		map[string]string{"test": "value"},
		nil,
		nil,
	)
	if err != nil {
		b.Fatalf("Failed to create trace aggregate: %v", err)
	}

	rootSpan := aggregate.GetRootSpan()
	if rootSpan == nil {
		b.Fatal("No root span found")
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		spanName := "child-span"
		attributes := map[string]string{
			"span.index": string(rune(i)),
			"operation":  "benchmark",
		}

		span, err := aggregate.CreateChildSpan(
			context.Background(),
			rootSpan.GetSpanID(),
			spanName,
			domain.SpanKindInternal,
			attributes,
		)

		if err != nil {
			b.Fatalf("Failed to create child span: %v", err)
		}

		// Prevent compiler optimization
		_ = span
	}
}

// BenchmarkArenaSpanAllocation benchmarks zero-allocation span creation
func BenchmarkArenaSpanAllocation(b *testing.B) {
	arena := core.NewMemoryArena(1024 * 1024) // 1MB arena

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		span := core.NewArenaSpan[string](
			arena,
			domain.TraceID{High: uint64(i), Low: uint64(i)},
			domain.SpanID{ID: uint64(i)},
		)

		// Set some attributes to test allocation
		span.SetAttribute("key1", "value1")
		span.SetAttribute("key2", "value2")
		span.SetAttribute("key3", "value3")

		// Prevent compiler optimization
		_ = span
	}
}

// BenchmarkRingBufferOperations benchmarks lock-free ring buffer performance
func BenchmarkRingBufferOperations(b *testing.B) {
	buffer := core.NewLockFreeRingBuffer[*domain.SpanSnapshot[string]](1024)

	// Pre-create span snapshots
	snapshots := make([]*domain.SpanSnapshot[string], 1000)
	for i := range snapshots {
		snapshots[i] = &domain.SpanSnapshot[string]{
			TraceID: domain.TraceID{High: uint64(i), Low: uint64(i)},
			SpanID:  domain.SpanID{ID: uint64(i)},
		}
	}

	b.Run("Push", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			snapshot := snapshots[i%len(snapshots)]
			buffer.TryPush(snapshot)
		}
	})

	b.Run("Pop", func(b *testing.B) {
		// Fill buffer first
		for i := 0; i < 500; i++ {
			buffer.TryPush(snapshots[i%len(snapshots)])
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = buffer.TryPop()
		}
	})

	b.Run("ConcurrentPushPop", func(b *testing.B) {
		var wg sync.WaitGroup

		b.ResetTimer()
		b.ReportAllocs()

		// Producers
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < b.N/2; i++ {
				snapshot := snapshots[i%len(snapshots)]
				buffer.TryPush(snapshot)
			}
		}()

		// Consumers
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < b.N/2; i++ {
				_, _ = buffer.TryPop()
			}
		}()

		wg.Wait()
	})
}

// BenchmarkBinaryEncoding benchmarks custom binary encoding performance
func BenchmarkBinaryEncoding(b *testing.B) {
	encoder := encoding.NewBinaryEncoder()
	compressor := encoding.NewCompressor(encoding.CompressionAlgorithmLZ4)

	// Create test data
	testData := &domain.SpanSnapshot[string]{
		TraceID: domain.TraceID{High: 12345, Low: 67890},
		SpanID:  domain.SpanID{ID: 11111},
		Name:    "benchmark-span",
		Attributes: map[string]string{
			"service.name":    "benchmark-service",
			"service.version": "1.0.0",
			"environment":     "production",
			"operation":       "database-query",
			"database.type":   "postgresql",
		},
	}

	b.Run("Encode", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			encoded, err := encoder.EncodeSpan(testData)
			if err != nil {
				b.Fatalf("Encoding failed: %v", err)
			}
			_ = encoded
		}
	})

	b.Run("EncodeWithCompression", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			encoded, err := encoder.EncodeSpan(testData)
			if err != nil {
				b.Fatalf("Encoding failed: %v", err)
			}

			compressed, err := compressor.Compress(encoded)
			if err != nil {
				b.Fatalf("Compression failed: %v", err)
			}
			_ = compressed
		}
	})

	// Pre-encode data for decode benchmarks
	encoded, _ := encoder.EncodeSpan(testData)
	compressed, _ := compressor.Compress(encoded)

	b.Run("Decode", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			decoded, err := encoder.DecodeSpan(encoded)
			if err != nil {
				b.Fatalf("Decoding failed: %v", err)
			}
			_ = decoded
		}
	})

	b.Run("DecodeWithDecompression", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			decompressed, err := compressor.Decompress(compressed)
			if err != nil {
				b.Fatalf("Decompression failed: %v", err)
			}

			decoded, err := encoder.DecodeSpan(decompressed)
			if err != nil {
				b.Fatalf("Decoding failed: %v", err)
			}
			_ = decoded
		}
	})
}

// BenchmarkBatchSpanProcessing benchmarks batch processing performance
func BenchmarkBatchSpanProcessing(b *testing.B) {
	// Create test spans
	spans := make([]*domain.SpanSnapshot[string], 1000)
	for i := range spans {
		spans[i] = &domain.SpanSnapshot[string]{
			TraceID: domain.TraceID{High: uint64(i / 100), Low: uint64(i)},
			SpanID:  domain.SpanID{ID: uint64(i)},
			Name:    "batch-span",
			Attributes: map[string]string{
				"batch.index": string(rune(i)),
				"batch.size":  "1000",
			},
		}
	}

	b.Run("Sequential", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			for _, span := range spans {
				// Simulate processing
				_ = span.TraceID
				_ = span.SpanID
				_ = span.Name
			}
		}
	})

	b.Run("Concurrent", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			var wg sync.WaitGroup
			numWorkers := runtime.NumCPU()
			spanChan := make(chan *domain.SpanSnapshot[string], len(spans))

			// Start workers
			for w := 0; w < numWorkers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for span := range spanChan {
						// Simulate processing
						_ = span.TraceID
						_ = span.SpanID
						_ = span.Name
					}
				}()
			}

			// Send spans to workers
			for _, span := range spans {
				spanChan <- span
			}
			close(spanChan)

			wg.Wait()
		}
	})
}

// BenchmarkSIMDOperations benchmarks SIMD-optimized operations
func BenchmarkSIMDOperations(b *testing.B) {
	// Create test data for SIMD operations
	data1 := make([]uint64, 1024)
	data2 := make([]uint64, 1024)
	result := make([]uint64, 1024)

	for i := range data1 {
		data1[i] = uint64(i)
		data2[i] = uint64(i * 2)
	}

	b.Run("ScalarXOR", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			for j := range data1 {
				result[j] = data1[j] ^ data2[j]
			}
		}
	})

	b.Run("SIMDXOR", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			core.SIMDXORUint64(data1, data2, result)
		}
	})

	b.Run("ScalarSum", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			var sum uint64
			for _, v := range data1 {
				sum += v
			}
			_ = sum
		}
	})

	b.Run("SIMDSum", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			sum := core.SIMDSumUint64(data1)
			_ = sum
		}
	})
}

// BenchmarkEventProcessing benchmarks domain event processing
func BenchmarkEventProcessing(b *testing.B) {
	// Create test events
	traceID := domain.TraceID{High: 1, Low: 1}
	spanID := domain.SpanID{ID: 1}

	events := make([]domain.TraceEvent, 100)
	for i := range events {
		if i%3 == 0 {
			events[i] = domain.NewTraceStartedEvent(traceID, "service", "span", map[string]string{"key": "value"})
		} else if i%3 == 1 {
			events[i] = domain.NewSpanCreatedEvent(traceID, spanID, domain.SpanID{ID: 0}, "child-span")
		} else {
			events[i] = domain.NewSpanFinishedEvent(traceID, spanID, time.Now(), time.Millisecond)
		}
	}

	b.Run("EventSerialization", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			for _, event := range events {
				serialized, err := domain.SerializeEvent(event)
				if err != nil {
					b.Fatalf("Event serialization failed: %v", err)
				}
				_ = serialized
			}
		}
	})

	b.Run("EventFiltering", func(b *testing.B) {
		filter := domain.EventFilter{
			TraceIDs:   []domain.TraceID{traceID},
			EventTypes: []domain.TraceEventType{domain.TraceEventTypeSpanStarted},
			Limit:      50,
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			filtered := domain.FilterEvents(events, filter)
			_ = filtered
		}
	})

	b.Run("EventAggregation", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			aggregates := domain.AggregateEvents(events)
			_ = aggregates
		}
	})
}

// BenchmarkMemoryUsage benchmarks memory usage patterns
func BenchmarkMemoryUsage(b *testing.B) {
	b.Run("WithObjectPooling", func(b *testing.B) {
		pool := sync.Pool{
			New: func() interface{} {
				return &domain.SpanSnapshot[string]{}
			},
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			span := pool.Get().(*domain.SpanSnapshot[string])
			span.TraceID = domain.TraceID{High: uint64(i), Low: uint64(i)}
			span.SpanID = domain.SpanID{ID: uint64(i)}
			span.Name = "pooled-span"

			// Reset and return to pool
			*span = domain.SpanSnapshot[string]{}
			pool.Put(span)
		}
	})

	b.Run("WithoutObjectPooling", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			span := &domain.SpanSnapshot[string]{
				TraceID: domain.TraceID{High: uint64(i), Low: uint64(i)},
				SpanID:  domain.SpanID{ID: uint64(i)},
				Name:    "new-span",
			}
			_ = span
		}
	})
}

// Benchmark results analysis and PGO optimization helpers

// RunProfileGuidedOptimization runs benchmarks with profiling for PGO
func RunProfileGuidedOptimization(b *testing.B) {
	b.Log("Running Profile-Guided Optimization benchmarks...")

	// This benchmark generates profiles that can be used for PGO compilation
	// Run with: go test -bench=BenchmarkPGO -cpuprofile=pgo.prof

	b.Run("CPUIntensiveTraceProcessing", func(b *testing.B) {
		// CPU-intensive operations that benefit from PGO
		for i := 0; i < b.N; i++ {
			// Create and process multiple traces
			for j := 0; j < 100; j++ {
				traceID := domain.TraceID{High: uint64(i), Low: uint64(j)}
				aggregate, _ := domain.NewTraceAggregate[string](
					traceID,
					"pgo-service",
					"pgo-span",
					domain.SpanKindServer,
					map[string]string{"iteration": string(rune(i))},
					nil,
					nil,
				)

				// Create multiple child spans
				if aggregate != nil {
					rootSpan := aggregate.GetRootSpan()
					if rootSpan != nil {
						for k := 0; k < 10; k++ {
							aggregate.CreateChildSpan(
								context.Background(),
								rootSpan.GetSpanID(),
								"child-span",
								domain.SpanKindInternal,
								map[string]string{"child": string(rune(k))},
							)
						}
					}
				}
			}
		}
	})
}

// BenchmarkComparison compares different implementation approaches
func BenchmarkComparison(b *testing.B) {
	b.Run("MapVsSlice_AttributeStorage", func(b *testing.B) {
		// Compare map vs slice for attribute storage
		attrs := map[string]string{
			"key1": "value1",
			"key2": "value2",
			"key3": "value3",
		}

		b.Run("Map", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for k, v := range attrs {
					_ = k
					_ = v
				}
			}
		})

		type KeyValue struct {
			Key   string
			Value string
		}
		kvSlice := []KeyValue{
			{"key1", "value1"},
			{"key2", "value2"},
			{"key3", "value3"},
		}

		b.Run("Slice", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, kv := range kvSlice {
					_ = kv.Key
					_ = kv.Value
				}
			}
		})
	})
}

// Regression test to ensure performance doesn't degrade
func BenchmarkRegressionTest(b *testing.B) {
	// These benchmarks establish performance baselines
	// Any significant regression should be investigated

	const expectedMinTraceCreationsPerSec = 100000
	const expectedMinSpanCreationsPerSec = 500000

	b.Run("TraceCreationThroughput", func(b *testing.B) {
		start := time.Now()
		BenchmarkTraceAggregateCreation(b)
		duration := time.Since(start)

		throughput := float64(b.N) / duration.Seconds()
		if throughput < expectedMinTraceCreationsPerSec {
			b.Errorf("Trace creation throughput regression: got %.0f/sec, expected >%.0f/sec",
				throughput, float64(expectedMinTraceCreationsPerSec))
		}
	})
}
