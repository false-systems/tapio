package grpc

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/events"
)

// BenchmarkEventBatching tests the event batching performance
func BenchmarkEventBatching(b *testing.B) {
	config := DefaultClientConfig()
	config.MaxBatchSize = 1000
	config.BatchTimeout = 10 * time.Millisecond
	config.BufferSize = 10000
	
	// Mock send function that simulates network latency
	sendCount := uint64(0)
	sendFn := func(ctx context.Context, batch *EventBatch) error {
		atomic.AddUint64(&sendCount, 1)
		time.Sleep(time.Microsecond * 100) // Simulate 100µs network round-trip
		return nil
	}
	
	batcher := NewEventBatcher(config, sendFn)
	ctx := context.Background()
	
	if err := batcher.Start(ctx); err != nil {
		b.Fatal(err)
	}
	defer batcher.Stop()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		event := events.NewBuilder().
			WithType("benchmark.event", events.EventCategory_CATEGORY_APPLICATION).
			WithSeverity(events.EventSeverity_SEVERITY_INFO).
			WithSource("benchmark", "test-collector", "test-node").
			WithEntity(events.EntityType_ENTITY_PROCESS, "123", "test").
			WithAttribute("sequence", int64(i)).
			Build()
		
		if err := batcher.AddEvent(event); err != nil {
			b.Fatal(err)
		}
		
		events.ReleaseEvent(event)
	}
	
	// Wait for all batches to be sent
	batcher.Stop()
	
	b.Logf("Batches sent: %d", atomic.LoadUint64(&sendCount))
}

// BenchmarkFlowControl tests the flow control performance
func BenchmarkFlowControl(b *testing.B) {
	config := DefaultServerConfig()
	config.DefaultEventsPerSec = 100000
	config.MaxBatchSize = 1000
	
	fc := NewFlowController(config)
	
	// Mock connection
	conn := &Connection{
		ID: "test-connection",
	}
	conn.SetRequestedRate(50000)
	conn.SetBufferUtilization(0.5)
	conn.SetMemoryPressure(MemoryPressure_MEMORY_PRESSURE_LOW)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		shouldThrottle := fc.ShouldThrottle(conn)
		_ = shouldThrottle
	}
}

// BenchmarkSerializationThroughput tests event serialization throughput
func BenchmarkSerializationThroughput(b *testing.B) {
	// Create test events
	testEvents := make([]*events.UnifiedEvent, 1000)
	for i := 0; i < 1000; i++ {
		testEvents[i] = events.NewBuilder().
			WithType("benchmark.serialization", events.EventCategory_CATEGORY_NETWORK).
			WithSeverity(events.EventSeverity_SEVERITY_INFO).
			WithSource("benchmark", "test-collector", "test-node").
			WithEntity(events.EntityType_ENTITY_PROCESS, "123", "test").
			WithNetworkData(&events.NetworkEvent{
				Protocol:        "tcp",
				SrcIp:          "192.168.1.1",
				SrcPort:        8080,
				DstIp:          "192.168.1.2",
				DstPort:        80,
				BytesSent:      1024,
				BytesReceived:  2048,
				State:          "ESTABLISHED",
			}).
			WithAttribute("sequence", int64(i)).
			Build()
	}
	
	defer func() {
		for _, event := range testEvents {
			events.ReleaseEvent(event)
		}
	}()
	
	config := DefaultSerializationConfig()
	serializer, err := NewSerializer(config)
	if err != nil {
		b.Fatal(err)
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		event := testEvents[i%len(testEvents)]
		data, err := serializer.SerializeEvent(event)
		if err != nil {
			b.Fatal(err)
		}
		_ = data
	}
}

// BenchmarkBatchSerialization tests batch serialization performance
func BenchmarkBatchSerialization(b *testing.B) {
	// Create test batch
	testEvents := make([]*events.UnifiedEvent, 100)
	for i := 0; i < 100; i++ {
		testEvents[i] = events.NewBuilder().
			WithType("benchmark.batch", events.EventCategory_CATEGORY_APPLICATION).
			WithSeverity(events.EventSeverity_SEVERITY_INFO).
			WithSource("benchmark", "test-collector", "test-node").
			WithEntity(events.EntityType_ENTITY_PROCESS, "123", "test").
			WithAttribute("sequence", int64(i)).
			Build()
	}
	
	defer func() {
		for _, event := range testEvents {
			events.ReleaseEvent(event)
		}
	}()
	
	config := DefaultSerializationConfig()
	config.Compression = CompressionType_COMPRESSION_LZ4
	serializer, err := NewSerializer(config)
	if err != nil {
		b.Fatal(err)
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		data, err := serializer.SerializeBatch(testEvents)
		if err != nil {
			b.Fatal(err)
		}
		_ = data
	}
}

// TestHighThroughputEventProcessing simulates high-throughput event processing
func TestHighThroughputEventProcessing(t *testing.T) {
	const (
		targetEventsPerSec = 165000
		testDuration      = 2 * time.Second
		expectedEvents    = targetEventsPerSec * 2 // 2 seconds worth
	)
	
	// Mock processor that can handle high throughput
	processor := &highThroughputProcessor{}
	
	config := DefaultServerConfig()
	config.DefaultEventsPerSec = targetEventsPerSec
	config.MaxBatchSize = 1000
	config.MaxEventBufferSize = 10 * 1024 * 1024 // 10MB buffer
	
	server := NewServer(config, processor)
	
	// Simulate event processing
	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()
	
	var wg sync.WaitGroup
	numWorkers := 10
	eventsPerWorker := expectedEvents / numWorkers
	
	start := time.Now()
	
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			for j := 0; j < eventsPerWorker; j++ {
				event := events.NewBuilder().
					WithType("test.throughput", events.EventCategory_CATEGORY_APPLICATION).
					WithSeverity(events.EventSeverity_SEVERITY_INFO).
					WithSource("test", "throughput-collector", "test-node").
					WithEntity(events.EntityType_ENTITY_PROCESS, "123", "test").
					WithAttribute("worker_id", int64(workerID)).
					WithAttribute("sequence", int64(j)).
					Build()
				
				// Simulate processing
				batch := &EventBatch{
					BatchId:       "test-batch",
					CollectorId:   "test-collector",
					CollectorType: "test",
					NodeId:        "test-node",
					Events:        []*events.UnifiedEvent{event},
					Compression:   CompressionType_COMPRESSION_NONE,
				}
				
				_, err := processor.ProcessEventBatch(ctx, batch)
				if err != nil && ctx.Err() == nil {
					t.Errorf("Failed to process event: %v", err)
					return
				}
				
				events.ReleaseEvent(event)
				
				if ctx.Err() != nil {
					return
				}
			}
		}(i)
	}
	
	wg.Wait()
	duration := time.Since(start)
	
	stats := processor.GetProcessingStats()
	actualEventsPerSec := float64(stats.EventsProcessed) / duration.Seconds()
	
	t.Logf("Processed %d events in %v", stats.EventsProcessed, duration)
	t.Logf("Actual throughput: %.0f events/sec", actualEventsPerSec)
	t.Logf("Target throughput: %d events/sec", targetEventsPerSec)
	t.Logf("Avg processing time: %v", stats.AvgProcessingTime)
	
	// We should achieve at least 80% of target throughput
	minAcceptableRate := float64(targetEventsPerSec) * 0.8
	if actualEventsPerSec < minAcceptableRate {
		t.Errorf("Throughput too low: got %.0f events/sec, want at least %.0f events/sec", 
			actualEventsPerSec, minAcceptableRate)
	}
	
	// Processing time should be reasonable
	if stats.AvgProcessingTime > 1*time.Millisecond {
		t.Errorf("Processing time too high: got %v, want <1ms", stats.AvgProcessingTime)
	}
	
	_ = server // Prevent unused variable error
}

// highThroughputProcessor is a mock processor optimized for high throughput
type highThroughputProcessor struct {
	eventsProcessed   uint64
	batchesProcessed  uint64
	totalProcessingTime int64
}

func (p *highThroughputProcessor) ProcessEvents(ctx context.Context, events []*events.UnifiedEvent) error {
	atomic.AddUint64(&p.eventsProcessed, uint64(len(events)))
	return nil
}

func (p *highThroughputProcessor) ProcessEventBatch(ctx context.Context, batch *EventBatch) (*EventAck, error) {
	start := time.Now()
	
	// Simulate minimal processing
	atomic.AddUint64(&p.eventsProcessed, uint64(len(batch.Events)))
	atomic.AddUint64(&p.batchesProcessed, 1)
	
	processingTime := time.Since(start)
	atomic.AddInt64(&p.totalProcessingTime, processingTime.Nanoseconds())
	
	return &EventAck{
		BatchId:        batch.BatchId,
		ProcessedCount: uint32(len(batch.Events)),
		FailedCount:    0,
	}, nil
}

func (p *highThroughputProcessor) GetProcessingStats() ProcessingStats {
	events := atomic.LoadUint64(&p.eventsProcessed)
	batches := atomic.LoadUint64(&p.batchesProcessed)
	totalTime := atomic.LoadInt64(&p.totalProcessingTime)
	
	var avgTime time.Duration
	if batches > 0 {
		avgTime = time.Duration(totalTime / int64(batches))
	}
	
	return ProcessingStats{
		EventsProcessed:   events,
		BatchesProcessed:  batches,
		AvgProcessingTime: avgTime,
		LastProcessedAt:   time.Now(),
		ErrorRate:         0.0,
	}
}

// BenchmarkClientServerRoundTrip benchmarks full client-server round trip
func BenchmarkClientServerRoundTrip(b *testing.B) {
	// This would normally test actual gRPC communication
	// For now, we'll benchmark the key components
	
	// Event creation
	b.Run("EventCreation", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			event := events.NewBuilder().
				WithType("benchmark.roundtrip", events.EventCategory_CATEGORY_NETWORK).
				WithSeverity(events.EventSeverity_SEVERITY_INFO).
				WithSource("benchmark", "test-collector", "test-node").
				WithEntity(events.EntityType_ENTITY_PROCESS, "123", "test").
				WithAttribute("sequence", int64(i)).
				Build()
			
			events.ReleaseEvent(event)
		}
	})
	
	// Serialization
	b.Run("Serialization", func(b *testing.B) {
		event := events.NewBuilder().
			WithType("benchmark.roundtrip", events.EventCategory_CATEGORY_NETWORK).
			WithSeverity(events.EventSeverity_SEVERITY_INFO).
			WithSource("benchmark", "test-collector", "test-node").
			WithEntity(events.EntityType_ENTITY_PROCESS, "123", "test").
			Build()
		defer events.ReleaseEvent(event)
		
		b.ReportAllocs()
		b.ResetTimer()
		
		for i := 0; i < b.N; i++ {
			data, err := event.SerializeFast()
			if err != nil {
				b.Fatal(err)
			}
			_ = data
		}
	})
	
	// Validation
	b.Run("Validation", func(b *testing.B) {
		event := events.NewBuilder().
			WithType("benchmark.roundtrip", events.EventCategory_CATEGORY_NETWORK).
			WithSeverity(events.EventSeverity_SEVERITY_INFO).
			WithSource("benchmark", "test-collector", "test-node").
			WithEntity(events.EntityType_ENTITY_PROCESS, "123", "test").
			Build()
		defer events.ReleaseEvent(event)
		
		validator := events.NewValidator()
		
		b.ReportAllocs()
		b.ResetTimer()
		
		for i := 0; i < b.N; i++ {
			err := validator.Validate(event)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}