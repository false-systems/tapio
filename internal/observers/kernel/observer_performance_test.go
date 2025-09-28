package kernel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// BenchmarkEventProcessing benchmarks event processing throughput
func BenchmarkEventProcessing(b *testing.B) {
	// Use mock mode for consistent benchmarking
	b.Setenv("TAPIO_MOCK_MODE", "true")

	config := &Config{
		Name:       "bench-kernel",
		BufferSize: 10000,
		EnableEBPF: false,
	}

	observer, err := NewObserver("bench", config)
	require.NoError(b, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(b, err)
	defer observer.Stop()

	events := observer.Events()
	var processedCount int64

	// Consumer goroutine
	done := make(chan bool)
	go func() {
		for {
			select {
			case event := <-events:
				if event != nil {
					atomic.AddInt64(&processedCount, 1)
				}
			case <-done:
				return
			}
		}
	}()

	b.ResetTimer()

	// Run for benchmark duration
	time.Sleep(time.Duration(b.N) * time.Millisecond)

	done <- true
	processed := atomic.LoadInt64(&processedCount)

	b.ReportMetric(float64(processed)/b.Elapsed().Seconds(), "events/sec")
	stats := observer.Statistics()
	var drops int64
	if droppedStr, ok := stats.CustomMetrics["events_dropped"]; ok {
		drops, _ = strconv.ParseInt(droppedStr, 10, 64)
	}
	b.ReportMetric(float64(drops), "drops")
}

// BenchmarkEventConversion benchmarks kernel event to domain event conversion
func BenchmarkEventConversion(b *testing.B) {
	observer, err := NewObserver("bench", nil)
	require.NoError(b, err)

	// Create sample kernel event
	kernelEvent := &KernelEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1234,
		TID:       5678,
		EventType: uint32(EventTypeConfigMapAccess),
		CgroupID:  999888,
		Comm:      [16]byte{'n', 'g', 'i', 'n', 'x'},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = observer.convertKernelEvent(kernelEvent)
		}
	})

	b.ReportAllocs()
}

// BenchmarkPathParsing benchmarks config path parsing
func BenchmarkPathParsing(b *testing.B) {
	observer, err := NewObserver("bench", nil)
	require.NoError(b, err)

	testPaths := []string{
		"/var/lib/kubelet/pods/abc-123-def/volumes/kubernetes.io~configmap/app-config",
		"/var/lib/kubelet/pods/xyz-789/volumes/kubernetes.io~secret/db-credentials",
		"/var/lib/kubelet/pods/pod-123/volume-subpaths/kubernetes.io~projected/config",
		"/etc/passwd",
		"/var/lib/kubelet/pods/test-456/volumes/kubernetes.io~configmap/nginx-conf",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _, _ = observer.parseConfigPath(testPaths[i%len(testPaths)])
			i++
		}
	})

	b.ReportAllocs()
}

// BenchmarkConcurrentEventSending benchmarks concurrent event sending
func BenchmarkConcurrentEventSending(b *testing.B) {
	config := &Config{
		Name:       "bench-concurrent",
		BufferSize: 10000,
		EnableEBPF: false,
	}

	observer, err := NewObserver("bench", config)
	require.NoError(b, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(b, err)
	defer observer.Stop()

	// Create sample event
	event := &domain.CollectorEvent{
		EventID:   "bench-1",
		Type:      domain.EventTypeKernelSyscall,
		Timestamp: time.Now(),
		Source:    "bench",
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Kernel: &domain.KernelData{
				PID:       1234,
				Command:   "nginx",
				CgroupID:  999,
				EventType: "config_access",
			},
		},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			observer.EventChannelManager.SendEvent(event)
		}
	})

	stats := observer.Statistics()
	b.ReportMetric(float64(stats.EventsProcessed), "processed")
	b.ReportMetric(float64(stats.ErrorCount), "errors")
	b.ReportAllocs()
}

// BenchmarkMemoryUsage benchmarks memory usage under load
func BenchmarkMemoryUsage(b *testing.B) {
	b.Setenv("TAPIO_MOCK_MODE", "true")

	config := &Config{
		Name:       "bench-memory",
		BufferSize: 1000,
		EnableEBPF: false,
	}

	observer, err := NewObserver("bench", config)
	require.NoError(b, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(b, err)
	defer observer.Stop()

	events := observer.Events()

	// Consumer to prevent buffer overflow
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-events:
				// Consume events
			case <-done:
				return
			}
		}
	}()

	// Measure memory before
	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	b.ResetTimer()

	// Run for benchmark duration
	time.Sleep(time.Duration(b.N) * time.Millisecond)

	// Measure memory after
	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	done <- true

	// Report memory metrics
	allocBytes := mAfter.TotalAlloc - mBefore.TotalAlloc
	b.ReportMetric(float64(allocBytes)/float64(b.N), "bytes/op")
	b.ReportMetric(float64(mAfter.HeapAlloc), "heap_bytes")
	b.ReportMetric(float64(mAfter.HeapObjects), "heap_objects")
}

// BenchmarkStatisticsRetrieval benchmarks Statistics() method performance
func BenchmarkStatisticsRetrieval(b *testing.B) {
	observer, err := NewObserver("bench", nil)
	require.NoError(b, err)

	// Record some events to make statistics meaningful
	for i := 0; i < 1000; i++ {
		observer.RecordEvent()
		if i%10 == 0 {
			observer.RecordError(nil)
		}
		if i%20 == 0 {
			observer.RecordDrop()
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = observer.Statistics()
		}
	})

	b.ReportAllocs()
}

// BenchmarkHealthCheck benchmarks Health() method performance
func BenchmarkHealthCheck(b *testing.B) {
	observer, err := NewObserver("bench", nil)
	require.NoError(b, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(b, err)
	defer observer.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = observer.Health()
		}
	})

	b.ReportAllocs()
}

// TestPerformanceUnderLoad tests observer performance under heavy load
func TestPerformanceUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	// Use mock mode for consistent testing
	t.Setenv("TAPIO_MOCK_MODE", "true")

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "perf-load",
		BufferSize: 10000,
		EnableEBPF: false,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	events := observer.Events()

	// Metrics
	var processedCount int64
	var processingTimes []time.Duration
	var mu sync.Mutex

	// Event processor
	done := make(chan bool)
	go func() {
		for {
			select {
			case event := <-events:
				if event != nil {
					start := time.Now()
					// Simulate processing
					time.Sleep(time.Microsecond)
					elapsed := time.Since(start)

					atomic.AddInt64(&processedCount, 1)
					mu.Lock()
					processingTimes = append(processingTimes, elapsed)
					mu.Unlock()
				}
			case <-done:
				return
			}
		}
	}()

	// Run for 5 seconds
	testDuration := 5 * time.Second
	time.Sleep(testDuration)

	done <- true
	cancel()

	// Calculate statistics
	processed := atomic.LoadInt64(&processedCount)
	stats := observer.Statistics()

	t.Logf("Performance test results:")
	t.Logf("  Duration: %v", testDuration)
	t.Logf("  Events processed: %d", processed)
	t.Logf("  Event errors: %d", stats.ErrorCount)
	t.Logf("  Throughput: %.2f events/sec", float64(processed)/testDuration.Seconds())

	// Calculate processing time statistics
	if len(processingTimes) > 0 {
		var total time.Duration
		var max time.Duration
		min := processingTimes[0]

		for _, d := range processingTimes {
			total += d
			if d > max {
				max = d
			}
			if d < min {
				min = d
			}
		}

		avg := total / time.Duration(len(processingTimes))
		t.Logf("  Avg processing time: %v", avg)
		t.Logf("  Min processing time: %v", min)
		t.Logf("  Max processing time: %v", max)
	}

	// Performance assertions
	throughput := float64(processed) / testDuration.Seconds()
	require.Greater(t, throughput, 0.5, "Throughput should be at least 0.5 events/sec in mock mode")

	// Check health remained good
	health := observer.Health()
	require.Equal(t, domain.HealthHealthy, health.Status)
}

// TestPerformanceWithFileOperations tests performance with real file operations
func TestPerformanceWithFileOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	if runtime.GOOS != "linux" {
		t.Skip("File operation performance test requires Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("Performance test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "perf-files",
		BufferSize: 10000,
		EnableEBPF: true,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Create test directory
	tempDir, err := os.MkdirTemp("", "perf-files")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Metrics
	start := time.Now()
	numOperations := 1000

	// Perform file operations
	for i := 0; i < numOperations; i++ {
		filename := filepath.Join(tempDir, fmt.Sprintf("file_%d.txt", i))

		// Write
		if err := os.WriteFile(filename, []byte(fmt.Sprintf("data_%d", i)), 0644); err != nil {
			continue
		}

		// Read
		if _, err := os.ReadFile(filename); err != nil {
			continue
		}

		// Delete
		os.Remove(filename)
	}

	elapsed := time.Since(start)
	opsPerSec := float64(numOperations*3) / elapsed.Seconds()

	// Get statistics
	stats := observer.Statistics()

	t.Logf("File operation performance:")
	t.Logf("  Operations: %d", numOperations*3)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Ops/sec: %.2f", opsPerSec)
	t.Logf("  Events captured: %d", stats.EventsProcessed)
	t.Logf("  Event errors: %d", stats.ErrorCount)
	t.Logf("  Capture rate: %.2f%%",
		float64(stats.EventsProcessed)/float64(numOperations*3)*100)

	// Performance assertions
	require.Greater(t, opsPerSec, 100.0, "Should handle at least 100 ops/sec")

	// Check health
	health := observer.Health()
	require.Equal(t, domain.HealthHealthy, health.Status)
}

// TestMemoryLeakCheck checks for memory leaks during extended operation
func TestMemoryLeakCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	t.Setenv("TAPIO_MOCK_MODE", "true")

	config := &Config{
		Name:       "mem-leak",
		BufferSize: 1000,
		EnableEBPF: false,
	}

	observer, err := NewObserver("mem", config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	events := observer.Events()

	// Consumer
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-events:
				// Consume
			case <-done:
				return
			}
		}
	}()

	// Force GC and get initial memory
	runtime.GC()
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Run for a while
	time.Sleep(10 * time.Second)

	// Force GC and get final memory
	runtime.GC()
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	done <- true

	// Calculate memory growth
	heapGrowth := int64(m2.HeapAlloc) - int64(m1.HeapAlloc)
	objectGrowth := int64(m2.HeapObjects) - int64(m1.HeapObjects)

	t.Logf("Memory leak check:")
	t.Logf("  Initial heap: %d bytes", m1.HeapAlloc)
	t.Logf("  Final heap: %d bytes", m2.HeapAlloc)
	t.Logf("  Heap growth: %d bytes", heapGrowth)
	t.Logf("  Object growth: %d", objectGrowth)

	// Allow some growth but flag excessive leaks
	maxAllowedGrowth := int64(10 * 1024 * 1024) // 10MB
	if heapGrowth > maxAllowedGrowth {
		t.Errorf("Excessive heap growth detected: %d bytes (max allowed: %d)",
			heapGrowth, maxAllowedGrowth)
	}
}
