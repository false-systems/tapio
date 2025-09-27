//go:build performance
// +build performance

package dns

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
	"go.uber.org/zap/zaptest"
)

// Performance tests measure throughput, latency, and resource usage

func BenchmarkObserver_TrackProblem(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConfig()
	config.Name = "perf-track"
	config.EnableEBPF = false
	config.BufferSize = 10000

	obs, err := NewObserver("perf", config, logger)
	require.NoError(b, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(b, err)
	defer obs.Stop()

	// Create test event
	event := &DNSEvent{
		Timestamp:   time.Now().UnixNano(),
		ProblemType: DNS_PROBLEM_SLOW,
		QueryName:   "test.example.com",
		QueryType:   1,
		LatencyNs:   150_000_000,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		obs.trackProblem(event)
	}
}

func BenchmarkObserver_ConcurrentTracking(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConfig()
	config.Name = "perf-concurrent"
	config.EnableEBPF = false
	config.BufferSize = 10000

	obs, err := NewObserver("perf", config, logger)
	require.NoError(b, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(b, err)
	defer obs.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		event := &DNSEvent{
			Timestamp:   time.Now().UnixNano(),
			ProblemType: DNS_PROBLEM_SLOW,
			QueryName:   fmt.Sprintf("test-%d.example.com", runtime.Gettid()),
			QueryType:   1,
			LatencyNs:   150_000_000,
		}

		for pb.Next() {
			obs.trackProblem(event)
		}
	})
}

func TestPerformance_HighVolume(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high volume performance test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "perf-volume"
	config.EnableEBPF = false
	config.BufferSize = 10000
	config.MaxEventsPerSecond = 10000

	obs, err := NewObserver("perf", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Generate high volume of events
	numEvents := 100000
	numGoroutines := 10
	eventsPerGoroutine := numEvents / numGoroutines

	start := time.Now()
	var wg sync.WaitGroup
	var processed int64

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for i := 0; i < eventsPerGoroutine; i++ {
				event := &DNSEvent{
					Timestamp:   time.Now().UnixNano(),
					ProblemType: DNS_PROBLEM_SLOW,
					QueryName:   fmt.Sprintf("perf-%d-%d.test.com", id, i),
					QueryType:   1,
					LatencyNs:   100_000_000 + int64(i%100)*1_000_000,
				}

				obs.trackProblem(event)
				atomic.AddInt64(&processed, 1)
			}
		}(g)
	}

	wg.Wait()
	elapsed := time.Since(start)

	eventsPerSecond := float64(processed) / elapsed.Seconds()
	t.Logf("Processed %d events in %v", processed, elapsed)
	t.Logf("Throughput: %.2f events/second", eventsPerSecond)

	// Should maintain reasonable throughput
	assert.Greater(t, eventsPerSecond, 5000.0, "Should process at least 5000 events/second")

	stats := obs.GetStats()
	t.Logf("Final stats: %+v", stats)
}

func TestPerformance_MemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory performance test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "perf-memory"
	config.EnableEBPF = false
	config.BufferSize = 1000
	config.RepeatWindowSec = 60

	obs, err := NewObserver("perf", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Force GC and get baseline
	runtime.GC()
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// Generate events with unique domains
	numEvents := 10000
	for i := 0; i < numEvents; i++ {
		event := &DNSEvent{
			Timestamp:   time.Now().UnixNano(),
			ProblemType: DNS_PROBLEM_SLOW,
			QueryName:   fmt.Sprintf("memory-test-%d.example.com", i),
			QueryType:   1,
			LatencyNs:   150_000_000,
		}
		obs.trackProblem(event)
	}

	// Force GC and measure
	runtime.GC()
	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	memUsed := memAfter.Alloc - memBefore.Alloc
	memPerEvent := memUsed / uint64(numEvents)

	t.Logf("Memory used for %d events: %d bytes (%.2f MB)", numEvents, memUsed, float64(memUsed)/1024/1024)
	t.Logf("Memory per event: %d bytes", memPerEvent)

	// Memory should be reasonable
	assert.Less(t, memPerEvent, uint64(1024), "Should use less than 1KB per event")
	assert.Less(t, memUsed, uint64(50*1024*1024), "Should use less than 50MB total")
}

func TestPerformance_EventChannelThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping channel throughput test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "perf-channel"
	config.EnableEBPF = false
	config.BufferSize = 10000

	obs, err := NewObserver("perf", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	events := obs.Events()

	// Consumer goroutine
	var received int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case event := <-events:
				if event != nil {
					atomic.AddInt64(&received, 1)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Producer - generate events
	numEvents := 50000
	start := time.Now()

	for i := 0; i < numEvents; i++ {
		event := &DNSEvent{
			Timestamp:   time.Now().UnixNano(),
			ProblemType: DNS_PROBLEM_SLOW,
			QueryName:   fmt.Sprintf("channel-%d.test.com", i),
			QueryType:   1,
			LatencyNs:   150_000_000,
		}
		obs.trackProblem(event)
	}

	// Wait a bit for consumption
	time.Sleep(100 * time.Millisecond)
	elapsed := time.Since(start)

	cancel() // Stop consumer
	<-done

	throughput := float64(atomic.LoadInt64(&received)) / elapsed.Seconds()
	t.Logf("Channel throughput: %.2f events/second", throughput)
	t.Logf("Received %d of %d events", atomic.LoadInt64(&received), numEvents)
}

func TestPerformance_CleanupEfficiency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cleanup efficiency test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "perf-cleanup"
	config.EnableEBPF = false
	config.RepeatWindowSec = 1 // Very short window for fast cleanup

	obs, err := NewObserver("perf", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Add many problems
	numProblems := 10000
	for i := 0; i < numProblems; i++ {
		event := &DNSEvent{
			Timestamp:   time.Now().UnixNano(),
			ProblemType: DNS_PROBLEM_SLOW,
			QueryName:   fmt.Sprintf("cleanup-%d.test.com", i),
			QueryType:   1,
			LatencyNs:   150_000_000,
		}
		obs.trackProblem(event)
	}

	// Verify all tracked
	obs.mu.RLock()
	trackedBefore := len(obs.recentProblems)
	obs.mu.RUnlock()
	assert.Equal(t, numProblems, trackedBefore)

	// Wait for cleanup window
	time.Sleep(1100 * time.Millisecond)

	// Measure cleanup performance
	start := time.Now()
	obs.doCleanup()
	cleanupTime := time.Since(start)

	// Verify cleanup worked
	obs.mu.RLock()
	trackedAfter := len(obs.recentProblems)
	obs.mu.RUnlock()

	t.Logf("Cleaned up %d problems in %v", trackedBefore-trackedAfter, cleanupTime)
	assert.Equal(t, 0, trackedAfter, "All old problems should be cleaned")
	assert.Less(t, cleanupTime, 100*time.Millisecond, "Cleanup should be fast")
}

func BenchmarkObserver_GetStats(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConfig()
	config.Name = "perf-stats"
	config.EnableEBPF = false

	obs, err := NewObserver("perf", config, logger)
	require.NoError(b, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(b, err)
	defer obs.Stop()

	// Add some data
	for i := 0; i < 1000; i++ {
		event := &DNSEvent{
			Timestamp:   time.Now().UnixNano(),
			ProblemType: DNS_PROBLEM_SLOW,
			QueryName:   fmt.Sprintf("stats-%d.test.com", i),
			QueryType:   1,
			LatencyNs:   150_000_000,
		}
		obs.trackProblem(event)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = obs.GetStats()
	}
}

func TestPerformance_CPUUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping CPU usage test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "perf-cpu"
	config.EnableEBPF = false

	obs, err := NewObserver("perf", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Monitor CPU usage during load
	numGoroutines := runtime.NumCPU()
	duration := 2 * time.Second
	stop := make(chan struct{})

	// Start load generators
	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			i := 0
			for {
				select {
				case <-stop:
					return
				default:
					event := &DNSEvent{
						Timestamp:   time.Now().UnixNano(),
						ProblemType: DNS_PROBLEM_SLOW,
						QueryName:   fmt.Sprintf("cpu-%d-%d.test.com", id, i),
						QueryType:   1,
						LatencyNs:   150_000_000,
					}
					obs.trackProblem(event)
					i++
				}
			}
		}(g)
	}

	// Run for duration
	time.Sleep(duration)
	close(stop)
	wg.Wait()

	stats := obs.GetStats()
	eventsPerSecond := float64(stats.TotalProblems) / duration.Seconds()
	t.Logf("Sustained throughput with %d goroutines: %.2f events/second", numGoroutines, eventsPerSecond)

	// Should maintain good throughput even under CPU load
	assert.Greater(t, eventsPerSecond, 1000.0, "Should maintain at least 1000 events/second under load")
}

func TestPerformance_LatencyDistribution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping latency distribution test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "perf-latency"
	config.EnableEBPF = false

	obs, err := NewObserver("perf", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Track operation latencies
	numOperations := 10000
	latencies := make([]time.Duration, 0, numOperations)

	for i := 0; i < numOperations; i++ {
		event := &DNSEvent{
			Timestamp:   time.Now().UnixNano(),
			ProblemType: DNS_PROBLEM_SLOW,
			QueryName:   fmt.Sprintf("latency-%d.test.com", i),
			QueryType:   1,
			LatencyNs:   150_000_000,
		}

		start := time.Now()
		obs.trackProblem(event)
		latency := time.Since(start)
		latencies = append(latencies, latency)
	}

	// Calculate percentiles
	var total time.Duration
	for _, l := range latencies {
		total += l
	}
	avg := total / time.Duration(numOperations)

	// Find p50, p95, p99
	p50 := latencies[len(latencies)*50/100]
	p95 := latencies[len(latencies)*95/100]
	p99 := latencies[len(latencies)*99/100]

	t.Logf("Latency distribution for %d operations:", numOperations)
	t.Logf("  Average: %v", avg)
	t.Logf("  P50: %v", p50)
	t.Logf("  P95: %v", p95)
	t.Logf("  P99: %v", p99)

	// Latency should be low
	assert.Less(t, avg, 100*time.Microsecond, "Average latency should be under 100µs")
	assert.Less(t, p99, 1*time.Millisecond, "P99 latency should be under 1ms")
}
