package systemd

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
)

// TestSystemdCollectorStress tests the collector under high load
func TestSystemdCollectorStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	// Setup OTEL test infrastructure  
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	traceExporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(traceExporter))
	otel.SetTracerProvider(tp)

	defer func() {
		_ = provider.Shutdown(context.Background())
		_ = tp.Shutdown(context.Background())
	}()

	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false
	config.BufferSize = 10000

	collector, err := NewCollector("stress-systemd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Stress test parameters
	const numGoroutines = 50
	const eventsPerGoroutine = 1000

	var wg sync.WaitGroup
	startTime := time.Now()

	// Launch stress test goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				data := map[string]interface{}{
					"pid":      id*1000 + j,
					"comm":     fmt.Sprintf("test-process-%d", id),
					"unit":     fmt.Sprintf("test-%d.service", id),
					"filename": fmt.Sprintf("/usr/bin/test-%d", id),
				}

				// Simulate event creation stress
				event := collector.createEvent("systemd_exec", data)
				assert.NotNil(t, event)
			}
		}(i)
	}

	// Concurrent operations while stress testing
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				collector.IsHealthy()
				collector.Statistics()
			case <-ctx.Done():
				return
			}
		}
	}()

	wg.Wait()
	duration := time.Since(startTime)

	// Performance validation
	totalEvents := numGoroutines * eventsPerGoroutine
	eventsPerSecond := float64(totalEvents) / duration.Seconds()

	t.Logf("Stress test completed: %d events in %v (%.2f events/sec)", 
		totalEvents, duration, eventsPerSecond)

	// Should handle at least 10k events per second
	assert.Greater(t, eventsPerSecond, float64(10000), 
		"SystemD collector should handle at least 10k events/sec")

	// Collector should still be healthy
	assert.True(t, collector.IsHealthy())
}

// TestSystemdCollectorMemoryPressure tests behavior under memory pressure
func TestSystemdCollectorMemoryPressure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory pressure test in short mode")
	}

	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false
	config.BufferSize = 1000 // Small buffer to trigger pressure

	collector, err := NewCollector("memory-pressure-systemd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Track memory usage
	var memBefore, memAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	// Generate large number of events to stress memory
	const numEvents = 100000
	for i := 0; i < numEvents; i++ {
		data := map[string]interface{}{
			"pid":      i,
			"comm":     fmt.Sprintf("memory-test-process-%d", i),
			"unit":     fmt.Sprintf("memory-test-%d.service", i),
			"filename": fmt.Sprintf("/usr/bin/memory-test-%d", i),
			"large_data": make([]byte, 1024), // 1KB per event
		}

		event := collector.createEvent("systemd_exec", data)
		assert.NotNil(t, event)

		// Periodically drain events to prevent blocking
		if i%100 == 0 {
			select {
			case <-collector.Events():
				// Drain event
			default:
				// Channel full, continue
			}
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&memAfter)

	// Memory usage should be reasonable
	memoryIncrease := memAfter.Alloc - memBefore.Alloc
	t.Logf("Memory increase: %d bytes (%.2f MB)", 
		memoryIncrease, float64(memoryIncrease)/(1024*1024))

	// Should not leak excessive memory (allow 100MB increase max)
	assert.Less(t, memoryIncrease, uint64(100*1024*1024),
		"Memory increase should be reasonable")

	// Collector should remain healthy
	assert.True(t, collector.IsHealthy())
}

// TestSystemdCollectorConcurrentEventProcessing tests concurrent event handling
func TestSystemdCollectorConcurrentEventProcessing(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false
	config.BufferSize = 5000

	collector, err := NewCollector("concurrent-systemd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	const numProducers = 10
	const numConsumers = 5
	const eventsPerProducer = 500

	// Event tracking
	var eventsProduced, eventsConsumed int64
	var mu sync.Mutex

	var wg sync.WaitGroup

	// Start event consumers
	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func(consumerID int) {
			defer wg.Done()
			consumed := 0
			for {
				select {
				case event := <-collector.Events():
					if event.Type == "systemd" {
						consumed++
					}
				case <-ctx.Done():
					mu.Lock()
					eventsConsumed += int64(consumed)
					mu.Unlock()
					t.Logf("Consumer %d consumed %d events", consumerID, consumed)
					return
				}
			}
		}(i)
	}

	// Start event producers
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func(producerID int) {
			defer wg.Done()
			produced := 0
			for j := 0; j < eventsPerProducer; j++ {
				data := map[string]interface{}{
					"producer_id": producerID,
					"event_id":    j,
					"pid":         producerID*1000 + j,
					"comm":        fmt.Sprintf("producer-%d", producerID),
				}

				event := collector.createEvent("systemd_exec", data)
				if event != nil {
					produced++
				}

				// Small delay to avoid overwhelming
				if j%10 == 0 {
					time.Sleep(time.Microsecond)
				}
			}
			mu.Lock()
			eventsProduced += int64(produced)
			mu.Unlock()
			t.Logf("Producer %d produced %d events", producerID, produced)
		}(i)
	}

	wg.Wait()

	t.Logf("Total events produced: %d, consumed: %d", eventsProduced, eventsConsumed)

	// Should have produced expected number of events
	expectedEvents := int64(numProducers * eventsPerProducer)
	assert.Equal(t, expectedEvents, eventsProduced)

	// Should have consumed significant portion (allow for buffer limitations)
	consumptionRate := float64(eventsConsumed) / float64(eventsProduced)
	assert.Greater(t, consumptionRate, 0.7, "Should consume at least 70% of events")
}

// TestSystemdCollectorResourceExhaustion tests behavior when resources are exhausted
func TestSystemdCollectorResourceExhaustion(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false
	config.BufferSize = 10 // Very small buffer

	collector, err := NewCollector("exhaustion-systemd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Don't consume events to exhaust buffer
	const numEvents = 1000
	eventsCreated := 0
	
	for i := 0; i < numEvents; i++ {
		data := map[string]interface{}{
			"pid":  i,
			"comm": fmt.Sprintf("exhaust-test-%d", i),
		}

		event := collector.createEvent("systemd_exec", data)
		if event != nil {
			eventsCreated++
		}

		// After buffer exhaustion, collector should remain functional
		if i%50 == 0 {
			assert.True(t, collector.IsHealthy(), 
				"Collector should remain healthy during resource exhaustion")
		}
	}

	t.Logf("Created %d events under resource exhaustion", eventsCreated)

	// Should have created some events before exhaustion
	assert.Greater(t, eventsCreated, 0, "Should create some events before exhaustion")

	// Collector should still be responsive
	stats := collector.Statistics()
	assert.NotNil(t, stats)
	assert.Greater(t, stats["events_dropped"], int64(0), 
		"Should have dropped events due to exhaustion")
}

// TestSystemdCollectorLongRunning tests collector behavior over extended period
func TestSystemdCollectorLongRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long running test in short mode")
	}

	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false

	collector, err := NewCollector("long-running-systemd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Track metrics over time
	startTime := time.Now()
	var statsHistory []map[string]interface{}
	var mu sync.Mutex

	// Background event consumer
	go func() {
		for {
			select {
			case <-collector.Events():
				// Consume events
			case <-ctx.Done():
				return
			}
		}
	}()

	// Background statistics collector
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				stats := collector.Statistics()
				mu.Lock()
				statsHistory = append(statsHistory, stats)
				mu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()

	// Continuous event generation
	eventCount := 0
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			data := map[string]interface{}{
				"pid":  eventCount,
				"comm": fmt.Sprintf("long-running-%d", eventCount),
				"time": time.Now().Unix(),
			}

			event := collector.createEvent("systemd_exec", data)
			if event != nil {
				eventCount++
			}

		case <-ctx.Done():
			goto finished
		}
	}

finished:
	duration := time.Since(startTime)
	t.Logf("Long running test completed: %d events over %v", eventCount, duration)

	// Verify collector remained stable
	assert.True(t, collector.IsHealthy(), "Collector should remain healthy")
	assert.Greater(t, eventCount, 1000, "Should generate significant events")

	// Verify statistics progression
	mu.Lock()
	require.Greater(t, len(statsHistory), 10, "Should have collected statistics")
	
	// Check that events are being processed over time
	firstStats := statsHistory[0]
	lastStats := statsHistory[len(statsHistory)-1]
	
	eventsGrowth := lastStats["events_collected"].(int64) - firstStats["events_collected"].(int64)
	assert.Greater(t, eventsGrowth, int64(500), "Should show event processing growth")
	mu.Unlock()
}

// BenchmarkSystemdEventCreation benchmarks event creation performance
func BenchmarkSystemdEventCreation(b *testing.B) {
	config := DefaultConfig()
	collector, err := NewCollector("bench-systemd", config)
	if err != nil {
		b.Fatal(err)
	}

	data := map[string]interface{}{
		"pid":      1234,
		"comm":     "systemd",
		"unit":     "test.service", 
		"filename": "/usr/bin/test",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			event := collector.createEvent("systemd_exec", data)
			if event == nil {
				b.Fatal("Event creation failed")
			}
		}
	})
}

// BenchmarkSystemdStringParsing benchmarks string parsing performance
func BenchmarkSystemdStringParsing(b *testing.B) {
	collector := &Collector{}
	
	// Various test strings
	testStrings := [][]byte{
		[]byte("systemd\x00"),
		[]byte("systemd-networkd.service\x00"),
		[]byte("/usr/lib/systemd/systemd\x00"),
		[]byte(""),
		make([]byte, 256), // All zeros
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, testStr := range testStrings {
			collector.nullTerminatedString(testStr)
		}
	}
}

// BenchmarkSystemdHealthCheck benchmarks health check performance  
func BenchmarkSystemdHealthCheck(b *testing.B) {
	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false

	collector, err := NewCollector("bench-health-systemd", config)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			collector.IsHealthy()
		}
	})
}

// BenchmarkSystemdStatistics benchmarks statistics collection performance
func BenchmarkSystemdStatistics(b *testing.B) {
	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false

	collector, err := NewCollector("bench-stats-systemd", config)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			stats := collector.Statistics()
			if stats == nil {
				b.Fatal("Statistics collection failed")
			}
		}
	})
}