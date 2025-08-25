//go:build stress
// +build stress

package network

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// TestExtremeLoad tests collector under extreme load conditions
func TestExtremeLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping extreme load test in short mode")
	}

	logger := zap.NewNop() // Use nop logger for maximum performance

	config := &IntelligenceCollectorConfig{
		NetworkCollectorConfig: &NetworkCollectorConfig{
			BufferSize:         100000, // Large buffer
			EnableIPv4:         true,
			EnableTCP:          true,
			EnableHTTP:         true,
			MaxEventsPerSecond: 1000000, // 1M events/sec
			SamplingRate:       0.01,    // Heavy sampling under extreme load
		},
		EnableIntelligenceMode:   true,
		IntelligenceSamplingRate: 0.001, // Only analyze 0.1% under extreme load
	}

	collector, err := NewIntelligenceCollector("extreme-load", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Performance metrics
	var totalSent int64
	var totalReceived int64
	var totalDropped int64
	var totalErrors int64

	// Memory stats
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Start multiple consumers to drain the channel
	numConsumers := runtime.NumCPU()
	var consumerWg sync.WaitGroup

	for i := 0; i < numConsumers; i++ {
		consumerWg.Add(1)
		go func(id int) {
			defer consumerWg.Done()
			for {
				select {
				case <-collector.Events():
					atomic.AddInt64(&totalReceived, 1)
				case <-ctx.Done():
					return
				}
			}
		}(i)
	}

	// Start extreme load generators
	numProducers := runtime.NumCPU() * 4 // More producers than consumers
	eventsPerProducer := 250000          // 1M total events

	start := time.Now()
	var producerWg sync.WaitGroup

	for i := 0; i < numProducers; i++ {
		producerWg.Add(1)
		go func(producerId int) {
			defer producerWg.Done()

			// Pre-allocate event templates for performance
			templates := make([]*domain.CollectorEvent, 10)
			for j := 0; j < 10; j++ {
				templates[j] = &domain.CollectorEvent{
					Type:      domain.EventTypeNetworkConnection,
					Source:    "stress-test",
					Severity:  domain.SeverityInfo,
					Timestamp: time.Now(),
					DataContainer: &domain.NetworkConnectionEvent{
						Protocol:      "tcp",
						SourceService: fmt.Sprintf("service-%d", producerId),
						DestService:   fmt.Sprintf("dest-%d", j),
						BytesSent:     1024,
						BytesReceived: 2048,
					},
					Metadata: domain.EventMetadata{
						CollectorName: "extreme-load",
					},
				}
			}

			for eventNum := 0; eventNum < eventsPerProducer; eventNum++ {
				// Reuse templates for performance
				event := templates[eventNum%10]
				event.EventID = fmt.Sprintf("extreme-%d-%d", producerId, eventNum)
				event.Timestamp = time.Now()

				select {
				case collector.events <- event:
					atomic.AddInt64(&totalSent, 1)
				default:
					// Channel full - this is expected under extreme load
					atomic.AddInt64(&totalDropped, 1)
				case <-ctx.Done():
					return
				}

				// Micro-pause every 1000 events to prevent CPU starvation
				if eventNum%1000 == 0 {
					runtime.Gosched()
				}
			}
		}(i)
	}

	// Wait for producers
	producerDone := make(chan bool)
	go func() {
		producerWg.Wait()
		close(producerDone)
	}()

	select {
	case <-producerDone:
		t.Log("All producers finished")
	case <-time.After(90 * time.Second):
		t.Log("Producer timeout - extreme load achieved")
		cancel()
	}

	// Give consumers time to drain
	time.Sleep(5 * time.Second)
	cancel()

	// Wait for consumers
	consumerTimeout := time.After(10 * time.Second)
	consumerDone := make(chan bool)
	go func() {
		consumerWg.Wait()
		close(consumerDone)
	}()

	select {
	case <-consumerDone:
	case <-consumerTimeout:
		t.Log("Consumer timeout")
	}

	duration := time.Since(start)

	// Memory stats after test
	runtime.GC()
	runtime.ReadMemStats(&m2)

	// Calculate metrics
	sent := atomic.LoadInt64(&totalSent)
	received := atomic.LoadInt64(&totalReceived)
	dropped := atomic.LoadInt64(&totalDropped)
	errors := atomic.LoadInt64(&totalErrors)

	throughput := float64(sent) / duration.Seconds()
	dropRate := float64(dropped) / float64(sent+dropped) * 100
	memoryGrowth := float64(m2.Alloc-m1.Alloc) / 1024 / 1024 // MB

	t.Logf("Extreme Load Test Results:")
	t.Logf("  Test Duration: %v", duration)
	t.Logf("  Events Sent: %d", sent)
	t.Logf("  Events Received: %d", received)
	t.Logf("  Events Dropped: %d", dropped)
	t.Logf("  Errors: %d", errors)
	t.Logf("  Send Throughput: %.2f events/sec", throughput)
	t.Logf("  Drop Rate: %.2f%%", dropRate)
	t.Logf("  Memory Growth: %.2f MB", memoryGrowth)
	t.Logf("  Producers: %d", numProducers)
	t.Logf("  Consumers: %d", numConsumers)

	// Assertions for extreme load
	assert.Greater(t, sent, int64(500000), "Should send at least 500K events")
	assert.Greater(t, throughput, float64(5000), "Should achieve >5K events/sec under extreme load")
	assert.Less(t, memoryGrowth, float64(100), "Memory growth should be <100MB")

	// Drop rate can be high under extreme load - this is expected
	t.Logf("Drop rate of %.2f%% is acceptable under extreme load", dropRate)
}

// TestConcurrencyStress tests extreme concurrency scenarios
func TestConcurrencyStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrency stress test in short mode")
	}

	logger := zap.NewNop()

	config := &IntelligenceCollectorConfig{
		NetworkCollectorConfig: &NetworkCollectorConfig{
			BufferSize: 50000,
			EnableIPv4: true,
			EnableTCP:  true,
		},
	}

	collector, err := NewIntelligenceCollector("concurrency-stress", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Extreme concurrency: 1000 goroutines
	numGoroutines := 1000
	operationsPerGoroutine := 10000

	var wg sync.WaitGroup
	var operations int64
	var panics int64

	start := time.Now()

	// Mixed workload goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt64(&panics, 1)
					t.Errorf("Goroutine %d panicked: %v", id, r)
				}
			}()

			for j := 0; j < operationsPerGoroutine; j++ {
				atomic.AddInt64(&operations, 1)

				switch j % 4 {
				case 0:
					// Send event
					event := &domain.CollectorEvent{
						EventID:   fmt.Sprintf("conc-%d-%d", id, j),
						Timestamp: time.Now(),
						Type:      domain.EventTypeNetworkConnection,
						Source:    "concurrency-stress",
					}
					select {
					case collector.events <- event:
					default:
					}

				case 1:
					// Health check
					_ = collector.IsHealthy()

				case 2:
					// Get stats (intelligence collector)
					_ = collector.GetIntelligenceStats()

				case 3:
					// Get dependencies
					_ = collector.GetServiceDependencies()
				}

				// Yield occasionally
				if j%100 == 0 {
					runtime.Gosched()
				}
			}
		}(i)
	}

	// Consumer goroutines
	numConsumers := 50
	var consumed int64

	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-collector.Events():
					atomic.AddInt64(&consumed, 1)
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Wait for completion
	done := make(chan bool)
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(50 * time.Second):
		t.Log("Concurrency stress test timeout")
		cancel()
		wg.Wait()
	}

	duration := time.Since(start)
	totalOps := atomic.LoadInt64(&operations)
	totalConsumed := atomic.LoadInt64(&consumed)
	totalPanics := atomic.LoadInt64(&panics)

	opsPerSecond := float64(totalOps) / duration.Seconds()

	t.Logf("Concurrency Stress Test Results:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Goroutines: %d", numGoroutines)
	t.Logf("  Total Operations: %d", totalOps)
	t.Logf("  Operations/sec: %.2f", opsPerSecond)
	t.Logf("  Events Consumed: %d", totalConsumed)
	t.Logf("  Panics: %d", totalPanics)

	// Assertions
	assert.Equal(t, int64(0), totalPanics, "No panics should occur")
	assert.Greater(t, totalOps, int64(numGoroutines*operationsPerGoroutine*0.95), "Should complete most operations")
	assert.Greater(t, opsPerSecond, float64(10000), "Should achieve >10K ops/sec")
}

// TestMemoryPressure tests behavior under memory pressure
func TestMemoryPressure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory pressure test in short mode")
	}

	logger := zap.NewNop()

	// Small buffer to create pressure
	config := &IntelligenceCollectorConfig{
		NetworkCollectorConfig: &NetworkCollectorConfig{
			BufferSize: 100, // Very small buffer
			EnableIPv4: true,
			EnableTCP:  true,
		},
		EnableIntelligenceMode: true,
	}

	collector, err := NewIntelligenceCollector("memory-pressure", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Memory tracking
	var m1, m2, m3 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Slow consumer to create backpressure
	var consumed int64
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond) // Slow consumption
		defer ticker.Stop()

		for {
			select {
			case <-collector.Events():
				atomic.AddInt64(&consumed, 1)
			case <-ticker.C:
				// Throttle consumption
			case <-ctx.Done():
				return
			}
		}
	}()

	// Fast producers to create memory pressure
	var produced int64
	var dropped int64

	numProducers := 10
	var wg sync.WaitGroup

	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < 10000; j++ {
				// Large event to increase memory pressure
				event := &domain.CollectorEvent{
					EventID:   fmt.Sprintf("pressure-%d-%d", id, j),
					Timestamp: time.Now(),
					Type:      domain.EventTypeNetworkConnection,
					Source:    "memory-pressure",
					DataContainer: &domain.NetworkConnectionEvent{
						Protocol:      "tcp",
						SourceService: fmt.Sprintf("large-service-name-to-increase-memory-usage-%d", id),
						DestService:   fmt.Sprintf("another-large-destination-service-name-%d", j),
						BytesSent:     uint64(j * 1000),
						BytesReceived: uint64(j * 2000),
					},
					Metadata: domain.EventMetadata{
						CollectorName: "memory-pressure",
						Attributes: map[string]string{
							"large_attribute_1": fmt.Sprintf("large-value-to-consume-memory-%d-%d", id, j),
							"large_attribute_2": fmt.Sprintf("another-large-attribute-value-%d", j),
							"timestamp":         time.Now().Format(time.RFC3339Nano),
						},
					},
				}

				select {
				case collector.events <- event:
					atomic.AddInt64(&produced, 1)
				default:
					atomic.AddInt64(&dropped, 1)
				case <-ctx.Done():
					return
				}

				// Check memory periodically
				if j%1000 == 0 {
					runtime.ReadMemStats(&m2)
					// Force GC if memory usage is too high (>500MB)
					if m2.Alloc > 500*1024*1024 {
						runtime.GC()
					}
				}
			}
		}(i)
	}

	// Wait for producers
	wg.Wait()

	// Force final GC and measure
	runtime.GC()
	runtime.ReadMemStats(&m3)

	producedCount := atomic.LoadInt64(&produced)
	droppedCount := atomic.LoadInt64(&dropped)
	consumedCount := atomic.LoadInt64(&consumed)

	memoryStart := float64(m1.Alloc) / 1024 / 1024
	memoryPeak := float64(m2.Alloc) / 1024 / 1024
	memoryEnd := float64(m3.Alloc) / 1024 / 1024

	t.Logf("Memory Pressure Test Results:")
	t.Logf("  Events Produced: %d", producedCount)
	t.Logf("  Events Dropped: %d", droppedCount)
	t.Logf("  Events Consumed: %d", consumedCount)
	t.Logf("  Drop Rate: %.2f%%", float64(droppedCount)/float64(producedCount+droppedCount)*100)
	t.Logf("  Memory Start: %.2f MB", memoryStart)
	t.Logf("  Memory Peak: %.2f MB", memoryPeak)
	t.Logf("  Memory End: %.2f MB", memoryEnd)
	t.Logf("  Memory Growth: %.2f MB", memoryEnd-memoryStart)

	// Assertions
	assert.Greater(t, producedCount, int64(50000), "Should produce significant events")
	assert.Less(t, memoryEnd-memoryStart, float64(50), "Memory growth should be bounded")
	assert.Greater(t, droppedCount, int64(0), "Should drop events due to backpressure")

	// System should remain stable under pressure
	assert.True(t, collector.IsHealthy(), "Collector should remain healthy")
}

// TestLongRunningStability tests stability over extended periods
func TestLongRunningStability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running stability test in short mode")
	}

	logger := zap.NewNop()

	config := &IntelligenceCollectorConfig{
		NetworkCollectorConfig: &NetworkCollectorConfig{
			BufferSize: 10000,
			EnableIPv4: true,
			EnableTCP:  true,
		},
		EnableIntelligenceMode: true,
	}

	collector, err := NewIntelligenceCollector("stability", config, logger)
	require.NoError(t, err)

	// 10 minute stability test
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Metrics tracking
	var totalEvents int64
	var healthChecks int64
	var errors int64

	// Consumer
	go func() {
		for {
			select {
			case <-collector.Events():
				atomic.AddInt64(&totalEvents, 1)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Producer with variable load
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()

		eventId := 0
		for {
			select {
			case <-ticker.C:
				eventId++
				event := &domain.CollectorEvent{
					EventID:   fmt.Sprintf("stability-%d", eventId),
					Timestamp: time.Now(),
					Type:      domain.EventTypeNetworkConnection,
					Source:    "stability-test",
				}

				select {
				case collector.events <- event:
				default:
					atomic.AddInt64(&errors, 1)
				}

				// Variable load: sometimes burst, sometimes slow
				if eventId%10000 == 0 {
					time.Sleep(100 * time.Millisecond)
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	// Health monitor
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if !collector.IsHealthy() {
					atomic.AddInt64(&errors, 1)
					t.Error("Collector became unhealthy")
				}
				atomic.AddInt64(&healthChecks, 1)

			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for test completion
	<-ctx.Done()

	events := atomic.LoadInt64(&totalEvents)
	checks := atomic.LoadInt64(&healthChecks)
	errs := atomic.LoadInt64(&errors)

	t.Logf("Long-Running Stability Test Results:")
	t.Logf("  Test Duration: 10 minutes")
	t.Logf("  Total Events: %d", events)
	t.Logf("  Health Checks: %d", checks)
	t.Logf("  Errors: %d", errs)
	t.Logf("  Average Events/sec: %.2f", float64(events)/600)

	// Assertions
	assert.Greater(t, events, int64(100000), "Should process >100K events in 10 minutes")
	assert.Less(t, errs, int64(100), "Should have minimal errors")
	assert.True(t, collector.IsHealthy(), "Should remain healthy")
}
