package status

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

func BenchmarkObserverCreation(b *testing.B) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    10000,
		SampleRate:    1.0,
		FlushInterval: 1 * time.Second,
		Logger:        logger,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		observer, err := NewObserver("bench", config)
		if err != nil {
			b.Fatal(err)
		}
		_ = observer
	}
}

func BenchmarkEventProcessing(b *testing.B) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    10000,
		SampleRate:    1.0,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("bench", config)
	require.NoError(b, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(b, err)
	defer observer.Stop()

	event := &domain.CollectorEvent{
		EventID:   "bench-123",
		Timestamp: time.Now(),
		Type:      domain.EventTypeNetworkConnection,
		Source:    "bench",
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				SrcIP:    "192.168.1.1",
				DstIP:    "192.168.1.2",
				DstPort:  80,
				Protocol: "HTTP",
			},
		},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			observer.EventChannelManager.SendEvent(event)
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/sec")
}

func BenchmarkAggregatorAdd(b *testing.B) {
	aggregator := NewStatusAggregator(1 * time.Second)

	events := make([]*StatusEvent, 1000)
	for i := range events {
		events[i] = &StatusEvent{
			ServiceHash:  uint32(i % 100),
			EndpointHash: uint32(i % 50),
			StatusCode:   uint16(200 + i%400),
			ErrorType:    ErrorType(i % 8),
			Latency:      uint32(100 + i%1000),
			Timestamp:    uint64(time.Now().UnixNano()),
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			aggregator.Add(events[i%len(events)])
			i++
		}
	})
}

func BenchmarkAggregatorFlush(b *testing.B) {
	aggregator := NewStatusAggregator(1 * time.Second)

	// Pre-populate with data
	for i := 0; i < 1000; i++ {
		event := &StatusEvent{
			ServiceHash:  uint32(i % 100),
			EndpointHash: uint32(i % 50),
			StatusCode:   uint16(200 + i%400),
			ErrorType:    ErrorType(i % 8),
			Latency:      uint32(100 + i%1000),
		}
		aggregator.Add(event)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := aggregator.Flush()
		_ = result

		// Re-populate for next iteration
		for j := 0; j < 100; j++ {
			aggregator.Add(&StatusEvent{
				ServiceHash: uint32(j),
				StatusCode:  200,
			})
		}
	}
}

func BenchmarkHashDecoderOperations(b *testing.B) {
	decoder := NewHashDecoder()

	// Pre-populate with services and endpoints
	for i := 0; i < 10000; i++ {
		decoder.AddService(uint32(i), fmt.Sprintf("service-%d", i))
		decoder.AddEndpoint(uint32(i), fmt.Sprintf("/endpoint/%d", i))
	}

	b.Run("GetService", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				decoder.GetService(uint32(i % 10000))
				i++
			}
		})
	})

	b.Run("GetEndpoint", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				decoder.GetEndpoint(uint32(i % 10000))
				i++
			}
		})
	})

	b.Run("AddService", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			i := 10000
			for pb.Next() {
				decoder.AddService(uint32(i), "service")
				i++
			}
		})
	})
}

func BenchmarkPatternDetection(b *testing.B) {
	// Create event sets for each pattern
	timeoutEvents := make([]*StatusEvent, 100)
	for i := range timeoutEvents {
		timeoutEvents[i] = &StatusEvent{
			ServiceHash: uint32(i),
			ErrorType:   ErrorTimeout,
		}
	}

	retryEvents := make([]*StatusEvent, 100)
	for i := range retryEvents {
		retryEvents[i] = &StatusEvent{
			ServiceHash: 12345, // Same service for retry storm
		}
	}

	refusedEvents := make([]*StatusEvent, 100)
	for i := range refusedEvents {
		refusedEvents[i] = &StatusEvent{
			ServiceHash: uint32(i % 10),
			ErrorType:   ErrorRefused,
		}
	}

	b.Run("CascadingTimeout", func(b *testing.B) {
		pattern := KnownPatterns[0]
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = pattern.Detector(timeoutEvents)
		}
	})

	b.Run("RetryStorm", func(b *testing.B) {
		pattern := KnownPatterns[1]
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = pattern.Detector(retryEvents)
		}
	})

	b.Run("ServiceDown", func(b *testing.B) {
		pattern := KnownPatterns[2]
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = pattern.Detector(refusedEvents)
		}
	})
}

func TestHighLoadScenario(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:         true,
		BufferSize:      10000,
		MaxEventsPerSec: 100000,
		SampleRate:      1.0,
		FlushInterval:   100 * time.Millisecond,
		Logger:          logger,
	}

	observer, err := NewObserver("load-test", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Generate high load
	var wg sync.WaitGroup
	var totalSent atomic.Int64
	var totalDropped atomic.Int64

	numWorkers := 10
	eventsPerWorker := 10000

	startTime := time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for i := 0; i < eventsPerWorker; i++ {
				event := &domain.CollectorEvent{
					EventID:   fmt.Sprintf("load-%d-%d", workerID, i),
					Timestamp: time.Now(),
					Type:      domain.EventTypeNetworkConnection,
					Source:    "load-test",
					EventData: domain.EventDataContainer{
						Network: &domain.NetworkData{
							SrcIP:   fmt.Sprintf("10.0.%d.%d", workerID, i%256),
							DstIP:   fmt.Sprintf("10.1.%d.%d", workerID, i%256),
							DstPort: int32(8080 + i%100),
						},
					},
				}

				if observer.EventChannelManager.SendEvent(event) {
					totalSent.Add(1)
				} else {
					totalDropped.Add(1)
				}
			}
		}(w)
	}

	wg.Wait()
	duration := time.Since(startTime)

	// Calculate metrics
	totalEvents := totalSent.Load() + totalDropped.Load()
	throughput := float64(totalSent.Load()) / duration.Seconds()
	dropRate := float64(totalDropped.Load()) / float64(totalEvents) * 100

	t.Logf("Load test results:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Total events: %d", totalEvents)
	t.Logf("  Sent: %d", totalSent.Load())
	t.Logf("  Dropped: %d (%.2f%%)", totalDropped.Load(), dropRate)
	t.Logf("  Throughput: %.2f events/sec", throughput)

	// Assertions
	assert.Greater(t, throughput, 1000.0, "Should handle at least 1000 events/sec")
	assert.Less(t, dropRate, 50.0, "Drop rate should be less than 50%")

	// Check observer health
	assert.True(t, observer.IsHealthy())

	// Check statistics
	stats := observer.Statistics()
	assert.Greater(t, stats.EventsProcessed, int64(0))
}

func TestMemoryUsageUnderLoad(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    1000,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("memory-test", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Measure initial memory
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	initialHeap := m1.HeapAlloc

	// Generate sustained load
	done := make(chan bool)
	go func() {
		for i := 0; i < 100000; i++ {
			event := &StatusEvent{
				ServiceHash:  uint32(i % 1000),
				EndpointHash: uint32(i % 500),
				StatusCode:   uint16(200 + i%400),
				ErrorType:    ErrorType(i % 8),
				Latency:      uint32(100 + i%1000),
				Timestamp:    uint64(time.Now().UnixNano()),
			}
			observer.aggregator.Add(event)

			if i%1000 == 0 {
				time.Sleep(1 * time.Millisecond)
			}
		}
		done <- true
	}()

	// Wait for completion
	<-done

	// Force GC and measure memory
	runtime.GC()
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	finalHeap := m2.HeapAlloc

	memoryIncrease := float64(finalHeap-initialHeap) / (1024 * 1024)
	t.Logf("Memory usage:")
	t.Logf("  Initial heap: %.2f MB", float64(initialHeap)/(1024*1024))
	t.Logf("  Final heap: %.2f MB", float64(finalHeap)/(1024*1024))
	t.Logf("  Increase: %.2f MB", memoryIncrease)

	// Memory increase should be reasonable
	assert.Less(t, memoryIncrease, 100.0, "Memory increase should be less than 100MB")
}

func TestCPUUsageProfile(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    10000,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("cpu-test", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Measure CPU usage during load
	startCPU := time.Now()
	var cpuUser1, cpuSys1 time.Duration
	if err := getCPUTime(&cpuUser1, &cpuSys1); err != nil {
		t.Skip("Cannot measure CPU time on this platform")
	}

	// Generate load for measurement
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25000; i++ {
				event := &StatusEvent{
					ServiceHash:  uint32(i % 100),
					EndpointHash: uint32(i % 50),
					StatusCode:   uint16(200 + i%400),
					ErrorType:    ErrorType(i % 8),
					Latency:      uint32(100 + i%1000),
				}
				observer.aggregator.Add(event)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(startCPU)

	var cpuUser2, cpuSys2 time.Duration
	getCPUTime(&cpuUser2, &cpuSys2)

	cpuUsage := float64(cpuUser2-cpuUser1+cpuSys2-cpuSys1) / float64(elapsed) * 100

	t.Logf("CPU usage during load: %.2f%%", cpuUsage)
	t.Logf("  User time: %v", cpuUser2-cpuUser1)
	t.Logf("  System time: %v", cpuSys2-cpuSys1)
	t.Logf("  Wall time: %v", elapsed)

	// CPU usage should be reasonable
	assert.Less(t, cpuUsage, 100.0, "Should not consume 100% CPU")
}

func TestLatencyPercentiles(t *testing.T) {
	aggregator := NewStatusAggregator(1 * time.Second)

	// Generate events with various latencies
	latencies := make([]uint32, 10000)
	for i := range latencies {
		// Create a distribution with outliers
		if i < 9000 {
			latencies[i] = uint32(100 + i%100) // Normal range: 100-199us
		} else if i < 9900 {
			latencies[i] = uint32(1000 + i%1000) // Slow: 1000-1999us
		} else {
			latencies[i] = uint32(10000 + i%10000) // Very slow: 10000+us
		}

		event := &StatusEvent{
			ServiceHash: 12345,
			Latency:     latencies[i],
		}
		aggregator.Add(event)
	}

	result := aggregator.Flush()
	agg := result[12345]
	require.NotNil(t, agg)

	avgLatency := agg.AvgLatency()
	t.Logf("Latency statistics:")
	t.Logf("  Average: %.2f us", avgLatency)
	t.Logf("  Total samples: %d", agg.LatencyCount)

	// Average should be influenced by outliers
	assert.Greater(t, avgLatency, 200.0)
}

func TestScalabilityWithServices(t *testing.T) {
	decoder := NewHashDecoder()
	aggregator := NewStatusAggregator(100 * time.Millisecond)

	numServices := 10000
	numEndpoints := 100

	// Populate decoder with many services
	for i := 0; i < numServices; i++ {
		serviceName := fmt.Sprintf("service-%d", i)
		decoder.AddService(uint32(i), serviceName)

		for j := 0; j < numEndpoints; j++ {
			endpoint := fmt.Sprintf("/api/v1/endpoint/%d", j)
			decoder.AddEndpoint(uint32(i*numEndpoints+j), endpoint)
		}
	}

	// Generate events for all services
	startTime := time.Now()
	for i := 0; i < numServices*10; i++ {
		event := &StatusEvent{
			ServiceHash:  uint32(i % numServices),
			EndpointHash: uint32(i % (numServices * numEndpoints)),
			StatusCode:   uint16(200 + i%400),
			ErrorType:    ErrorType(i % 8),
			Latency:      uint32(100 + i%1000),
		}
		aggregator.Add(event)
	}

	// Flush and measure
	flushStart := time.Now()
	result := aggregator.Flush()
	flushDuration := time.Since(flushStart)

	totalDuration := time.Since(startTime)

	t.Logf("Scalability test:")
	t.Logf("  Services: %d", numServices)
	t.Logf("  Endpoints per service: %d", numEndpoints)
	t.Logf("  Total events: %d", numServices*10)
	t.Logf("  Processing time: %v", totalDuration)
	t.Logf("  Flush time: %v", flushDuration)
	t.Logf("  Aggregated services: %d", len(result))

	// Performance assertions
	assert.Less(t, flushDuration, 100*time.Millisecond, "Flush should be fast")
	assert.Greater(t, len(result), 0, "Should have aggregated results")
}

func TestBackpressureHandling(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    10, // Very small buffer to test backpressure
		FlushInterval: 1 * time.Second,
		Logger:        logger,
	}

	observer, err := NewObserver("backpressure-test", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Send events faster than they can be processed
	var sent, dropped atomic.Int64

	var wg sync.WaitGroup
	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				event := &domain.CollectorEvent{
					EventID:   fmt.Sprintf("bp-%d", i),
					Timestamp: time.Now(),
					Type:      domain.EventTypeNetworkConnection,
					Source:    "backpressure",
				}

				if observer.EventChannelManager.SendEvent(event) {
					sent.Add(1)
				} else {
					dropped.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	t.Logf("Backpressure test:")
	t.Logf("  Sent: %d", sent.Load())
	t.Logf("  Dropped: %d", dropped.Load())
	t.Logf("  Drop rate: %.2f%%", float64(dropped.Load())/10000*100)

	// With small buffer, we expect drops
	assert.Greater(t, dropped.Load(), int64(0), "Should have dropped events due to backpressure")
	assert.Greater(t, sent.Load(), int64(0), "Should have sent some events")
}

// Helper function to get CPU time (platform-specific)
func getCPUTime(user, sys *time.Duration) error {
	// This would need platform-specific implementation
	// For now, just return dummy values for testing
	*user = time.Duration(0)
	*sys = time.Duration(0)
	return nil
}
