package pkg

import (
	"context"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/falseyair/tapio/pkg/unified"
	"github.com/falseyair/tapio/pkg/sources"
	"github.com/falseyair/tapio/pkg/performance"
)

// TestPerformanceTargets validates the 165k+ events/sec performance target
func TestPerformanceTargets(t *testing.T) {
	// Test high-performance ring buffer
	buffer, err := performance.NewRingBuffer(65536) // Large buffer for high throughput
	require.NoError(t, err)

	// Performance test parameters
	duration := 1 * time.Second
	numProducers := 8
	numConsumers := 4
	targetEventsPerSecond := 165000

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	var totalProduced, totalConsumed int64

	// Start producers
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			produced := int64(0)
			for {
				select {
				case <-ctx.Done():
					totalProduced += produced
					return
				default:
					data := "event-data"
					if buffer.TryPut(unsafe.Pointer(&data)) {
						produced++
					}
				}
			}
		}(i)
	}

	// Start consumers
	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			consumed := int64(0)
			for {
				select {
				case <-ctx.Done():
					totalConsumed += consumed
					return
				default:
					if _, ok := buffer.TryGet(); ok {
						consumed++
					}
				}
			}
		}()
	}

	// Wait for test completion
	<-ctx.Done()
	wg.Wait()

	// Calculate events per second
	eventsPerSecond := totalConsumed // Since we ran for 1 second

	t.Logf("Performance Test Results:")
	t.Logf("  Total Produced: %d events", totalProduced)
	t.Logf("  Total Consumed: %d events", totalConsumed)
	t.Logf("  Events/Second: %d", eventsPerSecond)
	t.Logf("  Target: %d events/sec", targetEventsPerSecond)

	// Verify we met the performance target
	assert.GreaterOrEqual(t, eventsPerSecond, int64(targetEventsPerSecond),
		"Performance target not met: got %d events/sec, expected >= %d events/sec",
		eventsPerSecond, targetEventsPerSecond)
}

// TestSystemPerformance tests the unified system performance
func TestSystemPerformance(t *testing.T) {
	config := &unified.SystemConfig{
		EnableNetworkMonitoring: false,
		EnableSystemd:           false,
		EnableJournald:          false,
		EventBufferSize:         100000,
		MaxEventsPerSecond:      200000, // Set high limit for testing
		BatchSize:               1000,
		EnableCircuitBreaker:    false,
		EnableSelfHealing:       false,
		EnableLoadShedding:      false,
		MaxMemoryMB:            200,
		MaxCPUPercent:          80,
	}

	system, err := unified.NewUnifiedSystem(config)
	require.NoError(t, err)

	startTime := time.Now()
	err = system.Start()
	require.NoError(t, err)
	defer system.Stop()

	// Measure startup time
	startupTime := time.Since(startTime)
	t.Logf("System startup time: %v", startupTime)

	// Startup should be fast
	assert.Less(t, startupTime, 5*time.Second, "System startup took too long")

	// Run system for a short time and measure metrics
	time.Sleep(500 * time.Millisecond)

	metrics := system.GetMetrics()
	assert.True(t, metrics.IsRunning)
	assert.Greater(t, metrics.Uptime, time.Duration(0))
	assert.GreaterOrEqual(t, metrics.CPUUsage, 0.0)
	assert.GreaterOrEqual(t, metrics.MemoryUsage, 0.0)

	t.Logf("System Performance Metrics:")
	t.Logf("  Uptime: %v", metrics.Uptime)
	t.Logf("  CPU Usage: %.2f%%", metrics.CPUUsage)
	t.Logf("  Memory Usage: %.2f MB", metrics.MemoryUsage)
}

// TestEBPFSourcePerformance tests eBPF source performance
func TestEBPFSourcePerformance(t *testing.T) {
	source := sources.NewEBPFSource()
	require.NotNil(t, source)

	ctx := context.Background()
	err := source.Start(ctx)
	require.NoError(t, err)
	defer source.Stop(ctx)

	// Measure data collection performance
	numIterations := 10000
	startTime := time.Now()

	for i := 0; i < numIterations; i++ {
		_, err := source.GetData(ctx, "process_stats", nil)
		require.NoError(t, err)
	}

	duration := time.Since(startTime)
	operationsPerSecond := float64(numIterations) / duration.Seconds()

	t.Logf("eBPF Source Performance:")
	t.Logf("  Operations: %d", numIterations)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Operations/Second: %.0f", operationsPerSecond)

	// Should be able to handle at least 1000 operations per second
	assert.GreaterOrEqual(t, operationsPerSecond, 1000.0,
		"eBPF source performance too low: %.0f ops/sec", operationsPerSecond)
}

// TestMemoryUsage tests system memory efficiency
func TestMemoryUsage(t *testing.T) {
	config := &unified.SystemConfig{
		EnableNetworkMonitoring: false,
		EnableSystemd:           false,
		EnableJournald:          false,
		EventBufferSize:         10000,
		MaxEventsPerSecond:      50000,
		BatchSize:               500,
		EnableCircuitBreaker:    true,
		EnableSelfHealing:       true,
		EnableLoadShedding:      true,
		MaxMemoryMB:            100, // Strict memory limit
		MaxCPUPercent:          50,
	}

	system, err := unified.NewUnifiedSystem(config)
	require.NoError(t, err)

	err = system.Start()
	require.NoError(t, err)
	defer system.Stop()

	// Let system run and check memory usage
	time.Sleep(1 * time.Second)

	metrics := system.GetMetrics()
	t.Logf("Memory Usage Test:")
	t.Logf("  Current Memory: %.2f MB", metrics.MemoryUsage)
	t.Logf("  Memory Limit: %d MB", config.MaxMemoryMB)

	// Verify memory usage is within configured limits
	assert.LessOrEqual(t, metrics.MemoryUsage, float64(config.MaxMemoryMB),
		"Memory usage exceeded limit: %.2f MB > %d MB", metrics.MemoryUsage, config.MaxMemoryMB)
}

// TestLatencyTargets tests system response latency
func TestLatencyTargets(t *testing.T) {
	source := sources.NewEBPFSource()
	require.NotNil(t, source)

	ctx := context.Background()
	err := source.Start(ctx)
	require.NoError(t, err)
	defer source.Stop(ctx)

	// Measure operation latency
	numSamples := 1000
	latencies := make([]time.Duration, numSamples)

	for i := 0; i < numSamples; i++ {
		start := time.Now()
		_, err := source.GetData(ctx, "process_stats", nil)
		require.NoError(t, err)
		latencies[i] = time.Since(start)
	}

	// Calculate statistics
	var totalLatency time.Duration
	maxLatency := time.Duration(0)
	for _, latency := range latencies {
		totalLatency += latency
		if latency > maxLatency {
			maxLatency = latency
		}
	}
	avgLatency := totalLatency / time.Duration(numSamples)

	t.Logf("Latency Test Results:")
	t.Logf("  Samples: %d", numSamples)
	t.Logf("  Average Latency: %v", avgLatency)
	t.Logf("  Maximum Latency: %v", maxLatency)

	// Verify latency targets (should be < 500µs average)
	targetLatency := 500 * time.Microsecond
	assert.Less(t, avgLatency, targetLatency,
		"Average latency too high: %v > %v", avgLatency, targetLatency)

	// Maximum latency should be reasonable (< 10ms)
	maxTargetLatency := 10 * time.Millisecond
	assert.Less(t, maxLatency, maxTargetLatency,
		"Maximum latency too high: %v > %v", maxLatency, maxTargetLatency)
}

// Benchmark tests for performance validation
func BenchmarkRingBufferThroughput(b *testing.B) {
	// Use a fixed large power-of-2 size for consistent benchmarking
	bufferSize := uint64(1048576) // 1M entries
	buffer, err := performance.NewRingBuffer(bufferSize)
	if err != nil {
		b.Fatal(err)
	}

	data := "benchmark-data"

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buffer.TryPut(unsafe.Pointer(&data))
			buffer.TryGet()
		}
	})
}

func BenchmarkSystemMetrics(b *testing.B) {
	config := &unified.SystemConfig{
		EnableNetworkMonitoring: false,
		EnableSystemd:           false,
		EnableJournald:          false,
		EventBufferSize:         1000,
		MaxEventsPerSecond:      1000000,
		BatchSize:               100,
	}

	system, err := unified.NewUnifiedSystem(config)
	if err != nil {
		b.Fatal(err)
	}

	err = system.Start()
	if err != nil {
		b.Fatal(err)
	}
	defer system.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = system.GetMetrics()
	}
}

func BenchmarkEBPFDataCollection(b *testing.B) {
	source := sources.NewEBPFSource()
	ctx := context.Background()

	err := source.Start(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer source.Stop(ctx)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := source.GetData(ctx, "process_stats", nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHighThroughputScenario tests the system under high load
func BenchmarkHighThroughputScenario(b *testing.B) {
	// Use a large power-of-2 buffer size
	bufferSize := uint64(1048576) // 1M entries, power of 2
	if uint64(b.N*2) > bufferSize {
		// Find next power of 2 if needed
		size := uint64(b.N * 2)
		for i := uint64(1); i < size; i <<= 1 {
			bufferSize = i
		}
	}

	buffer, err := performance.NewRingBuffer(bufferSize)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	// Simulate high-throughput scenario
	data := "high-throughput-event"
	successCount := 0

	for i := 0; i < b.N; i++ {
		if buffer.TryPut(unsafe.Pointer(&data)) {
			successCount++
		}
	}

	// Verify we achieved high throughput
	if successCount < b.N/2 {
		b.Fatalf("Low throughput: only %d/%d operations succeeded", successCount, b.N)
	}

	b.Logf("High throughput test: %d/%d operations succeeded", successCount, b.N)
}