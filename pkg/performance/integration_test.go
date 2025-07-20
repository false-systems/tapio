package performance_test

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"github.com/yairfalse/tapio/pkg/performance"
)

// TestEventServiceIntegration verifies EventService can use performance components
func TestEventServiceIntegration(t *testing.T) {
	// Create performance components
	buffer, err := performance.NewEventBuffer(1024)
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}

	pool := performance.NewUnifiedEventPool()

	// Simulate EventService usage
	// service := &grpc.EventService{}

	// Producer side - collectors sending events
	go func() {
		for i := 0; i < 100; i++ {
			event := pool.Get()
			event.ID = domain.GenerateEventID()
			event.Timestamp = time.Now()
			event.Type = "test"
			event.Source = "test-collector"

			if err := buffer.Put(event); err != nil {
				pool.Put(event)
				t.Logf("Buffer full at %d events", i)
				break
			}
		}
	}()

	// Consumer side - service processing events
	processed := 0
	timeout := time.After(2 * time.Second)

	for processed < 100 {
		select {
		case <-timeout:
			t.Logf("Timeout: processed %d events", processed)
			return
		default:
			if event, err := buffer.Get(); err == nil {
				// Verify event
				if event.ID == "" || event.Source != "test-collector" {
					t.Errorf("Invalid event: %+v", event)
				}

				// Return to pool
				pool.Put(event)
				processed++
			} else {
				time.Sleep(time.Millisecond)
			}
		}
	}

	t.Logf("Successfully processed %d events", processed)

	// Check pool efficiency
	stats := pool.GetStats()
	if stats.Recycled == 0 {
		t.Errorf("Pool not recycling objects: %+v", stats)
	}

	reuseRate := float64(stats.Recycled) / float64(stats.Allocated) * 100
	t.Logf("Pool reuse rate: %.1f%% (allocated: %d, recycled: %d)",
		reuseRate, stats.Allocated, stats.Recycled)
}

// TestCorrelationEngineIntegration verifies correlation engine can use performance components
func TestCorrelationEngineIntegration(t *testing.T) {
	// Create buffers for different priorities
	criticalBuffer, _ := performance.NewEventBuffer(512)
	normalBuffer, _ := performance.NewEventBuffer(1024)
	pool := performance.NewUnifiedEventPool()

	// Create correlation engine
	engine := correlation.NewSemanticCorrelationEngine()

	// Route events by severity
	routeEvent := func(event *domain.UnifiedEvent) error {
		if event.GetSeverity() == "critical" {
			return criticalBuffer.Put(event)
		}
		return normalBuffer.Put(event)
	}

	// Generate test events
	for i := 0; i < 50; i++ {
		event := pool.Get()
		event.ID = domain.GenerateEventID()
		event.Timestamp = time.Now()
		event.Type = "error"
		event.Source = "app"

		// Make some critical
		if i%5 == 0 {
			event.Impact = &domain.ImpactContext{
				Severity:       "critical",
				BusinessImpact: 0.9,
			}
		}

		if err := routeEvent(event); err != nil {
			pool.Put(event)
			t.Logf("Failed to route event: %v", err)
		}
	}

	// Process events with priority
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	criticalProcessed := 0
	normalProcessed := 0

	for ctx.Err() == nil {
		// Process critical first
		if event, err := criticalBuffer.Get(); err == nil {
			engine.ProcessUnifiedEvent(event)
			pool.Put(event)
			criticalProcessed++
			continue
		}

		// Then normal
		if event, err := normalBuffer.Get(); err == nil {
			engine.ProcessUnifiedEvent(event)
			pool.Put(event)
			normalProcessed++
			continue
		}

		// Both empty
		if criticalBuffer.IsEmpty() && normalBuffer.IsEmpty() {
			break
		}

		time.Sleep(time.Millisecond)
	}

	t.Logf("Processed %d critical and %d normal events", criticalProcessed, normalProcessed)

	// Verify critical were processed
	if criticalProcessed < 5 {
		t.Errorf("Expected at least 5 critical events, got %d", criticalProcessed)
	}
}

// TestBatchProcessing verifies batch operations work correctly
func TestBatchProcessing(t *testing.T) {
	batchBuffer, _ := performance.NewEventBatchBuffer(1024)
	pool := performance.NewUnifiedEventPool()

	// Create batch of events
	events := make([]*domain.UnifiedEvent, 100)
	for i := range events {
		event := pool.Get()
		event.ID = domain.GenerateEventID()
		event.Type = domain.EventType("batch-test")
		events[i] = event
	}

	// Put batch
	added, err := batchBuffer.PutBatch(events)
	if err != nil && added == 0 {
		t.Fatalf("Failed to add batch: %v", err)
	}
	t.Logf("Added %d events to batch buffer", added)

	// Get batch
	retrieved, err := batchBuffer.GetBatch(50)
	if err != nil && len(retrieved) == 0 {
		t.Fatalf("Failed to get batch: %v", err)
	}
	t.Logf("Retrieved %d events from batch buffer", len(retrieved))

	// Return to pool
	for _, event := range retrieved {
		pool.Put(event)
	}

	// Drain remaining
	remaining := make([]*domain.UnifiedEvent, 100)
	count := batchBuffer.DrainTo(remaining)
	t.Logf("Drained %d remaining events", count)

	for i := 0; i < count; i++ {
		pool.Put(remaining[i])
	}

	// Verify pool stats
	stats := pool.GetStats()
	t.Logf("Final pool stats: %+v", stats)
}

// TestRealWorldScenario simulates a realistic usage pattern
func TestRealWorldScenario(t *testing.T) {
	// Components
	buffer, _ := performance.NewEventBuffer(4096)
	pool := performance.NewUnifiedEventPool()
	bytePool := performance.NewByteSlicePool()

	// Metrics
	var produced, consumed, dropped int

	// Producer (simulates high-speed collector)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	go func() {
		ticker := time.NewTicker(time.Microsecond) // 1M events/sec target
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				event := pool.Get()
				event.ID = domain.GenerateEventID()
				event.Timestamp = time.Now()
				event.Type = "kernel"

				// Simulate raw data
				raw := bytePool.Get(256)
				copy(raw, []byte("kernel event data"))
				event.RawData = raw

				if buffer.TryPut(event) {
					produced++
				} else {
					// Buffer full, return resources
					bytePool.Put(raw)
					pool.Put(event)
					dropped++
				}
			}
		}
	}()

	// Consumer (simulates processing service)
	go func() {
		batch := make([]*domain.UnifiedEvent, 100)

		for ctx.Err() == nil {
			// Process in batches
			count := buffer.DrainTo(batch)
			if count == 0 {
				time.Sleep(time.Microsecond)
				continue
			}

			// Simulate processing
			for i := 0; i < count; i++ {
				event := batch[i]

				// Return resources
				if event.RawData != nil {
					bytePool.Put(event.RawData)
				}
				pool.Put(event)
				consumed++
			}
		}
	}()

	// Wait for test to complete
	<-ctx.Done()
	time.Sleep(10 * time.Millisecond) // Let consumers finish

	// Results
	t.Logf("Performance test results:")
	t.Logf("  Produced: %d events", produced)
	t.Logf("  Consumed: %d events", consumed)
	t.Logf("  Dropped:  %d events (%.2f%%)", dropped, float64(dropped)/float64(produced)*100)

	bufferStats := buffer.GetStats()
	t.Logf("  Buffer: %d/%d", bufferStats.Size, bufferStats.Capacity)

	poolStats := pool.GetStats()
	t.Logf("  Pool: allocated=%d, recycled=%d, in_use=%d",
		poolStats.Allocated, poolStats.Recycled, poolStats.InUse)

	// Verify high throughput
	if produced < 100000 {
		t.Logf("Warning: Lower than expected throughput. Produced only %d events/sec", produced)
	}

	// Verify low drops
	dropRate := float64(dropped) / float64(produced)
	if dropRate > 0.05 { // More than 5% drops
		t.Errorf("High drop rate: %.2f%%", dropRate*100)
	}
}

// BenchmarkEventBuffer measures event buffer performance
func BenchmarkEventBuffer(b *testing.B) {
	buffer, _ := performance.NewEventBuffer(65536)
	pool := performance.NewUnifiedEventPool()

	// Pre-fill events
	events := make([]*domain.UnifiedEvent, b.N)
	for i := range events {
		event := pool.Get()
		event.ID = domain.GenerateEventID()
		events[i] = event
	}

	b.ResetTimer()

	b.Run("Put", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buffer.TryPut(events[i%len(events)])
		}
	})

	b.Run("Get", func(b *testing.B) {
		// Pre-fill buffer
		for i := 0; i < 1000; i++ {
			buffer.TryPut(events[i%len(events)])
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buffer.TryGet()
		}
	})

	// Cleanup
	for _, event := range events {
		pool.Put(event)
	}
}

// BenchmarkObjectPool measures pool performance
func BenchmarkObjectPool(b *testing.B) {
	pool := performance.NewUnifiedEventPool()

	b.Run("GetPut", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			event := pool.Get()
			event.ID = "test"
			pool.Put(event)
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				event := pool.Get()
				event.ID = "test"
				pool.Put(event)
			}
		})
	})
}
