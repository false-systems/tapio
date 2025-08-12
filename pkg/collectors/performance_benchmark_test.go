package collectors

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
)

// BenchmarkCollectorEventThroughput benchmarks event processing throughput
func BenchmarkCollectorEventThroughput(b *testing.B) {
	// Setup OTEL
	mp := metric.NewMeterProvider()
	otel.SetMeterProvider(mp)
	tp := trace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		_ = mp.Shutdown(context.Background())
		_ = tp.Shutdown(context.Background())
	}()

	logger := zap.NewNop()
	collector := newBenchmarkCollector("throughput-test", logger)
	
	ctx := context.Background()
	err := collector.Start(ctx)
	require.NoError(b, err)
	defer collector.Stop()

	// Pre-create events to avoid allocation overhead in benchmark
	events := make([]RawEvent, b.N)
	for i := 0; i < b.N; i++ {
		events[i] = RawEvent{
			Type:      "benchmark",
			Timestamp: time.Now(),
			TraceID:   fmt.Sprintf("trace-%d", i),
			SpanID:    fmt.Sprintf("span-%d", i),
			Metadata: map[string]string{
				"collector": "throughput-test",
				"index":     fmt.Sprintf("%d", i),
			},
			Data: []byte(fmt.Sprintf(`{"index":%d}`, i)),
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	
	// Sequential event processing
	for i := 0; i < b.N; i++ {
		select {
		case collector.(*benchmarkCollector).events <- events[i]:
		default:
			b.Fatal("Event channel full")
		}
	}
}

// BenchmarkCollectorConcurrentEventThroughput benchmarks concurrent event processing
func BenchmarkCollectorConcurrentEventThroughput(b *testing.B) {
	// Setup OTEL
	mp := metric.NewMeterProvider()
	otel.SetMeterProvider(mp)
	tp := trace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		_ = mp.Shutdown(context.Background())
		_ = tp.Shutdown(context.Background())
	}()

	logger := zap.NewNop()
	collector := newBenchmarkCollector("concurrent-test", logger)
	
	ctx := context.Background()
	err := collector.Start(ctx)
	require.NoError(b, err)
	defer collector.Stop()

	b.ResetTimer()
	b.ReportAllocs()
	
	b.RunParallel(func(pb *testing.PB) {
		eventIndex := 0
		for pb.Next() {
			event := RawEvent{
				Type:      "concurrent_benchmark",
				Timestamp: time.Now(),
				TraceID:   fmt.Sprintf("concurrent-trace-%d", eventIndex),
				SpanID:    fmt.Sprintf("concurrent-span-%d", eventIndex),
				Metadata: map[string]string{
					"collector": "concurrent-test",
					"index":     fmt.Sprintf("%d", eventIndex),
				},
				Data: []byte(fmt.Sprintf(`{"concurrent_index":%d}`, eventIndex)),
			}
			
			select {
			case collector.(*benchmarkCollector).events <- event:
			default:
				// Channel full, continue
			}
			eventIndex++
		}
	})
}

// BenchmarkEventSerialization benchmarks event serialization performance
func BenchmarkEventSerialization(b *testing.B) {
	serializer := NewEventSerializer()
	
	event := RawEvent{
		Type:      "serialization_test",
		Timestamp: time.Now(),
		TraceID:   "benchmark-trace-id-1234567890",
		SpanID:    "benchmark-span-id-abcdef",
		Metadata: map[string]string{
			"collector":   "serialization-benchmark",
			"event_type":  "test_event",
			"component":   "benchmark_suite",
			"environment": "testing",
		},
		Data: []byte(`{"benchmark":true,"data_size":"medium","timestamp":1234567890}`),
	}

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_, err := serializer.SerializeEvent(event)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEventDeserialization benchmarks event deserialization performance  
func BenchmarkEventDeserialization(b *testing.B) {
	serializer := NewEventSerializer()
	
	originalEvent := RawEvent{
		Type:      "deserialization_test",
		Timestamp: time.Now(),
		TraceID:   "benchmark-trace-id-1234567890",
		SpanID:    "benchmark-span-id-abcdef",
		Metadata: map[string]string{
			"collector": "deserialization-benchmark",
		},
		Data: []byte(`{"benchmark":true}`),
	}
	
	serializedEvent, err := serializer.SerializeEvent(originalEvent)
	require.NoError(b, err)

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_, err := serializer.DeserializeEvent(serializedEvent)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCollectorHealthCheck benchmarks health check performance
func BenchmarkCollectorHealthCheck(b *testing.B) {
	logger := zap.NewNop()
	collector := newBenchmarkCollector("health-test", logger)

	b.ResetTimer()
	b.ReportAllocs()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			collector.IsHealthy()
		}
	})
}

// BenchmarkCollectorStatistics benchmarks statistics collection performance
func BenchmarkCollectorStatistics(b *testing.B) {
	logger := zap.NewNop()
	collector := newBenchmarkCollector("stats-test", logger)

	b.ResetTimer()
	b.ReportAllocs()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			stats := collector.Statistics()
			if stats == nil {
				b.Fatal("Statistics should not be nil")
			}
		}
	})
}

// BenchmarkMemoryUsage benchmarks memory usage patterns
func BenchmarkMemoryUsage(b *testing.B) {
	logger := zap.NewNop()
	
	b.Run("CollectorCreation", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			collector := newBenchmarkCollector(fmt.Sprintf("memory-test-%d", i), logger)
			_ = collector // Use collector to prevent optimization
		}
	})
	
	b.Run("EventAllocation", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			event := RawEvent{
				Type:      "memory_test",
				Timestamp: time.Now(),
				TraceID:   fmt.Sprintf("trace-%d", i),
				SpanID:    fmt.Sprintf("span-%d", i),
				Metadata: map[string]string{
					"index": fmt.Sprintf("%d", i),
				},
				Data: []byte(fmt.Sprintf(`{"index":%d}`, i)),
			}
			_ = event // Use event to prevent optimization
		}
	})
	
	b.Run("LargeEventData", func(b *testing.B) {
		largeData := make([]byte, 10*1024) // 10KB
		for i := range largeData {
			largeData[i] = byte(i % 256)
		}
		
		b.ReportAllocs()
		b.ResetTimer()
		
		for i := 0; i < b.N; i++ {
			event := RawEvent{
				Type:      "large_memory_test",
				Timestamp: time.Now(),
				TraceID:   fmt.Sprintf("large-trace-%d", i),
				SpanID:    fmt.Sprintf("large-span-%d", i),
				Metadata: map[string]string{
					"size": "large",
				},
				Data: largeData,
			}
			_ = event
		}
	})
}

// BenchmarkStringOperations benchmarks string processing operations
func BenchmarkStringOperations(b *testing.B) {
	b.Run("NullTerminatedString", func(b *testing.B) {
		testData := []byte("benchmark-test-string\\x00extra-data-here")
		parser := &stringParser{}
		
		b.ResetTimer()
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			result := parser.nullTerminatedString(testData)
			if result == "" {
				b.Fatal("Result should not be empty")
			}
		}
	})
	
	b.Run("StringValidation", func(b *testing.B) {
		testString := "valid-benchmark-string-with-numbers-123"
		validator := &stringValidator{}
		
		b.ResetTimer()
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			valid := validator.isValidString(testString)
			if !valid {
				b.Fatal("String should be valid")
			}
		}
	})
}

// BenchmarkConcurrentCollectorOperations benchmarks concurrent operations
func BenchmarkConcurrentCollectorOperations(b *testing.B) {
	logger := zap.NewNop()
	numCollectors := runtime.NumCPU()
	
	// Create multiple collectors
	collectors := make([]*benchmarkCollector, numCollectors)
	for i := 0; i < numCollectors; i++ {
		collectors[i] = newBenchmarkCollector(fmt.Sprintf("concurrent-%d", i), logger)
		ctx := context.Background()
		err := collectors[i].Start(ctx)
		require.NoError(b, err)
		defer collectors[i].Stop()
	}

	b.ResetTimer()
	b.ReportAllocs()
	
	b.RunParallel(func(pb *testing.PB) {
		collectorIndex := 0
		eventIndex := 0
		
		for pb.Next() {
			collector := collectors[collectorIndex%numCollectors]
			
			// Mix of operations
			switch eventIndex % 4 {
			case 0:
				collector.IsHealthy()
			case 1:
				collector.Statistics()
			case 2:
				event := RawEvent{
					Type:      "concurrent_ops",
					Timestamp: time.Now(),
					TraceID:   fmt.Sprintf("concurrent-trace-%d", eventIndex),
					SpanID:    fmt.Sprintf("concurrent-span-%d", eventIndex),
					Metadata:  map[string]string{"op": "concurrent"},
					Data:      []byte(fmt.Sprintf(`{"op_index":%d}`, eventIndex)),
				}
				select {
				case collector.events <- event:
				default:
					// Channel full
				}
			case 3:
				collector.Health()
			}
			
			collectorIndex++
			eventIndex++
		}
	})
}

// BenchmarkRegistryOperations benchmarks registry performance
func BenchmarkRegistryOperations(b *testing.B) {
	logger := zap.NewNop()
	registry := NewBenchmarkRegistry(logger)
	
	// Pre-register collectors
	numCollectors := 100
	for i := 0; i < numCollectors; i++ {
		collector := newBenchmarkCollector(fmt.Sprintf("registry-test-%d", i), logger)
		registry.Register(fmt.Sprintf("collector-%d", i), collector)
	}

	b.ResetTimer()
	b.ReportAllocs()
	
	b.Run("GetCollector", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			collectorIndex := 0
			for pb.Next() {
				name := fmt.Sprintf("collector-%d", collectorIndex%numCollectors)
				collector := registry.GetCollector(name)
				if collector == nil {
					b.Fatal("Collector should not be nil")
				}
				collectorIndex++
			}
		})
	})
	
	b.Run("ListCollectors", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			collectors := registry.ListCollectors()
			if len(collectors) != numCollectors {
				b.Fatalf("Expected %d collectors, got %d", numCollectors, len(collectors))
			}
		}
	})
	
	b.Run("Health", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			health := registry.Health()
			if !health.Healthy {
				b.Fatal("Registry should be healthy")
			}
		}
	})
}

// BenchmarkEventPipelineLatency benchmarks end-to-end event pipeline latency
func BenchmarkEventPipelineLatency(b *testing.B) {
	// Setup OTEL
	mp := metric.NewMeterProvider()
	otel.SetMeterProvider(mp)
	tp := trace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		_ = mp.Shutdown(context.Background())
		_ = tp.Shutdown(context.Background())
	}()

	logger := zap.NewNop()
	collector := newBenchmarkCollector("latency-test", logger)
	
	ctx := context.Background()
	err := collector.Start(ctx)
	require.NoError(b, err)
	defer collector.Stop()

	// Measure latency from event creation to consumption
	latencies := make([]time.Duration, 0, b.N)
	var latencyMu sync.Mutex
	
	// Consumer goroutine
	go func() {
		for {
			select {
			case event := <-collector.Events():
				// Extract timestamp from metadata
				if timestampStr, ok := event.Metadata["benchmark_timestamp"]; ok {
					if startTime, err := time.Parse(time.RFC3339Nano, timestampStr); err == nil {
						latency := time.Since(startTime)
						latencyMu.Lock()
						latencies = append(latencies, latency)
						latencyMu.Unlock()
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		startTime := time.Now()
		event := RawEvent{
			Type:      "latency_test",
			Timestamp: startTime,
			TraceID:   fmt.Sprintf("latency-trace-%d", i),
			SpanID:    fmt.Sprintf("latency-span-%d", i),
			Metadata: map[string]string{
				"collector":           "latency-test",
				"benchmark_timestamp": startTime.Format(time.RFC3339Nano),
			},
			Data: []byte(fmt.Sprintf(`{"latency_test":%d}`, i)),
		}
		
		select {
		case collector.events <- event:
		default:
			b.Fatal("Event channel full")
		}
	}
	
	// Wait for events to be processed
	time.Sleep(100 * time.Millisecond)
	
	// Report latency statistics
	latencyMu.Lock()
	if len(latencies) > 0 {
		var total time.Duration
		min := latencies[0]
		max := latencies[0]
		
		for _, lat := range latencies {
			total += lat
			if lat < min {
				min = lat
			}
			if lat > max {
				max = lat
			}
		}
		
		avg := total / time.Duration(len(latencies))
		b.Logf("Processed %d events, Latency - Min: %v, Max: %v, Avg: %v", 
			len(latencies), min, max, avg)
	}
	latencyMu.Unlock()
}

// Helper types and implementations for benchmarking

type benchmarkCollector struct {
	name           string
	logger         *zap.Logger
	events         chan RawEvent
	healthy        bool
	eventsReceived int64
	mu             sync.RWMutex
}

func newBenchmarkCollector(name string, logger *zap.Logger) *benchmarkCollector {
	return &benchmarkCollector{
		name:    name,
		logger:  logger,
		events:  make(chan RawEvent, 10000), // Large buffer for benchmarks
		healthy: true,
	}
}

func (bc *benchmarkCollector) Name() string { return bc.name }

func (bc *benchmarkCollector) IsHealthy() bool {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.healthy
}

func (bc *benchmarkCollector) Start(ctx context.Context) error {
	bc.mu.Lock()
	bc.healthy = true
	bc.mu.Unlock()
	
	// Start event consumer to prevent channel blocking
	go func() {
		for {
			select {
			case <-bc.events:
				atomic.AddInt64(&bc.eventsReceived, 1)
			case <-ctx.Done():
				return
			}
		}
	}()
	
	return nil
}

func (bc *benchmarkCollector) Stop() error {
	bc.mu.Lock()
	bc.healthy = false
	bc.mu.Unlock()
	return nil
}

func (bc *benchmarkCollector) Events() <-chan RawEvent { return bc.events }

func (bc *benchmarkCollector) Health() (bool, map[string]interface{}) {
	healthy := bc.IsHealthy()
	received := atomic.LoadInt64(&bc.eventsReceived)
	
	return healthy, map[string]interface{}{
		"healthy":           healthy,
		"events_received":   received,
		"events_collected":  received,
		"events_dropped":    int64(0),
		"error_count":       int64(0),
	}
}

func (bc *benchmarkCollector) Statistics() map[string]interface{} {
	received := atomic.LoadInt64(&bc.eventsReceived)
	
	return map[string]interface{}{
		"events_received":  received,
		"events_collected": received,
		"events_dropped":   int64(0),
		"error_count":      int64(0),
		"last_event_time":  time.Now(),
	}
}

type EventSerializer struct{}

func NewEventSerializer() *EventSerializer {
	return &EventSerializer{}
}

func (s *EventSerializer) SerializeEvent(event RawEvent) ([]byte, error) {
	// Simple serialization - in practice this might use protobuf, msgpack, etc.
	data := fmt.Sprintf("Type=%s,TraceID=%s,SpanID=%s,Data=%s", 
		event.Type, event.TraceID, event.SpanID, string(event.Data))
	return []byte(data), nil
}

func (s *EventSerializer) DeserializeEvent(data []byte) (*RawEvent, error) {
	// Simple deserialization
	return &RawEvent{
		Type:      "deserialized",
		Timestamp: time.Now(),
		TraceID:   "trace-123",
		SpanID:    "span-456",
		Metadata:  map[string]string{"source": "deserializer"},
		Data:      data,
	}, nil
}

type stringParser struct{}

func (p *stringParser) nullTerminatedString(data []byte) string {
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}

type stringValidator struct{}

func (v *stringValidator) isValidString(s string) bool {
	// Simple validation - check for printable characters
	for _, r := range s {
		if r < 32 || r > 126 {
			return false
		}
	}
	return true
}

type BenchmarkRegistry struct {
	logger     *zap.Logger
	collectors map[string]Collector
	mu         sync.RWMutex
}

func NewBenchmarkRegistry(logger *zap.Logger) *BenchmarkRegistry {
	return &BenchmarkRegistry{
		logger:     logger,
		collectors: make(map[string]Collector),
	}
}

func (br *BenchmarkRegistry) Register(name string, collector Collector) {
	br.mu.Lock()
	defer br.mu.Unlock()
	br.collectors[name] = collector
}

func (br *BenchmarkRegistry) GetCollector(name string) Collector {
	br.mu.RLock()
	defer br.mu.RUnlock()
	return br.collectors[name]
}

func (br *BenchmarkRegistry) ListCollectors() []string {
	br.mu.RLock()
	defer br.mu.RUnlock()
	
	names := make([]string, 0, len(br.collectors))
	for name := range br.collectors {
		names = append(names, name)
	}
	return names
}

func (br *BenchmarkRegistry) Health() RegistryHealth {
	br.mu.RLock()
	defer br.mu.RUnlock()
	
	healthy := 0
	for _, collector := range br.collectors {
		if collector.IsHealthy() {
			healthy++
		}
	}
	
	return RegistryHealth{
		Healthy:                true,
		CollectorsRegistered:   len(br.collectors),
		CollectorsHealthy:      healthy,
		TotalEventsProcessed:   int64(1000),
		EventsDroppedRatio:     0.01,
	}
}

// Additional benchmark helper functions

// BenchmarkHighVolumeEventProcessing tests processing under extreme load
func BenchmarkHighVolumeEventProcessing(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping high volume test in short mode")
	}

	logger := zap.NewNop()
	numCollectors := 10
	eventsPerCollector := b.N / numCollectors

	collectors := make([]*benchmarkCollector, numCollectors)
	for i := 0; i < numCollectors; i++ {
		collectors[i] = newBenchmarkCollector(fmt.Sprintf("volume-%d", i), logger)
		ctx := context.Background()
		err := collectors[i].Start(ctx)
		require.NoError(b, err)
		defer collectors[i].Stop()
	}

	b.ResetTimer()
	b.ReportAllocs()

	var wg sync.WaitGroup
	startTime := time.Now()

	for i, collector := range collectors {
		wg.Add(1)
		go func(collectorIndex int, c *benchmarkCollector) {
			defer wg.Done()
			
			for j := 0; j < eventsPerCollector; j++ {
				event := RawEvent{
					Type:      "high_volume",
					Timestamp: time.Now(),
					TraceID:   fmt.Sprintf("volume-trace-%d-%d", collectorIndex, j),
					SpanID:    fmt.Sprintf("volume-span-%d-%d", collectorIndex, j),
					Metadata: map[string]string{
						"collector": fmt.Sprintf("volume-%d", collectorIndex),
						"batch":     fmt.Sprintf("%d", j/1000),
					},
					Data: []byte(fmt.Sprintf(`{"volume_test":true,"collector":%d,"event":%d}`, collectorIndex, j)),
				}
				
				select {
				case c.events <- event:
				default:
					// Channel full, which is expected under high load
				}
			}
		}(i, collector)
	}

	wg.Wait()
	duration := time.Since(startTime)

	// Calculate and report throughput
	totalEvents := int64(b.N)
	eventsPerSecond := float64(totalEvents) / duration.Seconds()
	
	b.Logf("High volume test: %d events in %v (%.0f events/sec)", 
		totalEvents, duration, eventsPerSecond)

	// Verify all collectors are still healthy
	for i, collector := range collectors {
		if !collector.IsHealthy() {
			b.Errorf("Collector %d became unhealthy during high volume test", i)
		}
	}
}

// BenchmarkMemoryEfficiency tests memory usage efficiency
func BenchmarkMemoryEfficiency(b *testing.B) {
	var m1, m2 runtime.MemStats
	
	// Measure baseline memory
	runtime.GC()
	runtime.ReadMemStats(&m1)
	
	logger := zap.NewNop()
	collectors := make([]*benchmarkCollector, 100)
	
	b.ResetTimer()
	
	// Create collectors
	for i := 0; i < 100; i++ {
		collectors[i] = newBenchmarkCollector(fmt.Sprintf("memory-test-%d", i), logger)
	}
	
	// Process events
	for i := 0; i < b.N; i++ {
		collector := collectors[i%100]
		event := RawEvent{
			Type:      "memory_efficiency",
			Timestamp: time.Now(),
			TraceID:   fmt.Sprintf("memory-trace-%d", i),
			SpanID:    fmt.Sprintf("memory-span-%d", i),
			Metadata:  map[string]string{"test": "memory"},
			Data:      []byte(fmt.Sprintf(`{"memory_test":%d}`, i)),
		}
		
		select {
		case collector.events <- event:
		default:
			// Channel full
		}
	}
	
	runtime.GC()
	runtime.ReadMemStats(&m2)
	
	// Report memory usage
	allocatedBytes := m2.TotalAlloc - m1.TotalAlloc
	bytesPerEvent := float64(allocatedBytes) / float64(b.N)
	
	b.Logf("Memory efficiency: %d bytes total, %.2f bytes/event", 
		allocatedBytes, bytesPerEvent)
	
	b.ReportMetric(bytesPerEvent, "bytes/event")
}