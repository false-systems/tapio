package cri

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// BenchmarkSuite provides comprehensive performance testing for the CRI collector
func BenchmarkCRICollectorSuite(b *testing.B) {
	// Ensure consistent benchmark environment
	runtime.GC()
	runtime.GOMAXPROCS(runtime.NumCPU())

	b.Run("EventProcessing", BenchmarkEventProcessing)
	b.Run("ConcurrentAccess", BenchmarkConcurrentAccess)
	b.Run("MemoryEfficiency", BenchmarkMemoryEfficiency)
	b.Run("ThroughputUnderLoad", BenchmarkThroughputUnderLoad)
	b.Run("LatencyMeasurement", BenchmarkLatencyMeasurement)
}

func BenchmarkEventProcessing(b *testing.B) {
	collector := setupBenchmarkCollector(b)
	defer collector.Stop()

	status := createBenchmarkContainerStatus()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		collector.createEvent(status, EventOOM)

		// Consume event to prevent buffer overflow
		if event := collector.ringBuffer.Read(); event != nil {
			collector.eventPool.Put(event)
		}
	}

	reportBenchmarkMetrics(b, collector)
}

func BenchmarkConcurrentAccess(b *testing.B) {
	collector := setupBenchmarkCollector(b)
	defer collector.Stop()

	status := createBenchmarkContainerStatus()
	_ = runtime.NumCPU() // For reference, using parallel test runner instead

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Half producers, half consumers
			if pb.Next() {
				// Producer
				collector.createEvent(status, EventOOM)
			} else {
				// Consumer
				if event := collector.ringBuffer.Read(); event != nil {
					collector.eventPool.Put(event)
				}
			}
		}
	})

	reportBenchmarkMetrics(b, collector)
}

func BenchmarkMemoryEfficiency(b *testing.B) {
	collector := setupBenchmarkCollector(b)
	defer collector.Stop()

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	status := createBenchmarkContainerStatus()

	b.ResetTimer()
	b.ReportAllocs()

	// Process events and measure memory growth
	for i := 0; i < b.N; i++ {
		collector.createEvent(status, EventOOM)

		// Immediately consume to test pooling efficiency
		if event := collector.ringBuffer.Read(); event != nil {
			// Modify event to ensure it's not optimized away
			event.Timestamp = time.Now().UnixNano()
			collector.eventPool.Put(event)
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	// Report memory efficiency metrics
	memGrowth := m2.HeapInuse - m1.HeapInuse
	b.ReportMetric(float64(memGrowth)/float64(b.N), "bytes/op")
	b.ReportMetric(float64(m2.HeapObjects-m1.HeapObjects), "objects-delta")

	reportBenchmarkMetrics(b, collector)
}

func BenchmarkThroughputUnderLoad(b *testing.B) {
	collector := setupBenchmarkCollector(b)
	defer collector.Stop()

	// Start background consumer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if event := collector.ringBuffer.Read(); event != nil {
					collector.eventPool.Put(event)
				}
			}
		}
	}()

	status := createBenchmarkContainerStatus()
	numProducers := runtime.NumCPU()

	b.ResetTimer()
	b.ReportAllocs()

	// Use channels to coordinate producers
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			eventsPerWorker := b.N / numProducers
			for j := 0; j < eventsPerWorker; j++ {
				collector.createEvent(status, EventOOM)
			}
		}()
	}

	// Start all producers simultaneously
	close(start)
	startTime := time.Now()

	wg.Wait()
	duration := time.Since(startTime)

	// Report throughput metrics
	throughput := float64(b.N) / duration.Seconds()
	b.ReportMetric(throughput, "events/sec")

	reportBenchmarkMetrics(b, collector)
}

func BenchmarkLatencyMeasurement(b *testing.B) {
	collector := setupBenchmarkCollector(b)
	defer collector.Stop()

	status := createBenchmarkContainerStatus()
	latencies := make([]time.Duration, b.N)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		start := time.Now()

		collector.createEvent(status, EventOOM)

		// Wait for event to be available and consume it
		for {
			if event := collector.ringBuffer.Read(); event != nil {
				latency := time.Since(start)
				latencies[i] = latency
				collector.eventPool.Put(event)
				break
			}
		}
	}

	// Calculate latency statistics
	var totalLatency time.Duration
	minLatency := latencies[0]
	maxLatency := latencies[0]

	for _, lat := range latencies {
		totalLatency += lat
		if lat < minLatency {
			minLatency = lat
		}
		if lat > maxLatency {
			maxLatency = lat
		}
	}

	avgLatency := totalLatency / time.Duration(len(latencies))

	// Report latency metrics
	b.ReportMetric(float64(avgLatency.Nanoseconds()), "avg-latency-ns")
	b.ReportMetric(float64(minLatency.Nanoseconds()), "min-latency-ns")
	b.ReportMetric(float64(maxLatency.Nanoseconds()), "max-latency-ns")

	reportBenchmarkMetrics(b, collector)
}

func BenchmarkRingBufferOperations(b *testing.B) {
	sizes := []int{1024, 4096, 8192, 16384}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size-%d", size), func(b *testing.B) {
			// Create custom ring buffer with specific size
			buffer := &RingBuffer{
				mask: uint64(size - 1),
			}

			events := make([]*Event, size/2)
			for i := range events {
				events[i] = &Event{Type: EventOOM}
			}

			b.ResetTimer()
			b.ReportAllocs()

			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					event := events[i%len(events)]

					if !buffer.Write(event) {
						// Buffer full, read one
						buffer.Read()
						buffer.Write(event)
					}

					i++
				}
			})

			b.ReportMetric(buffer.Usage(), "buffer-usage-%")
		})
	}
}

func BenchmarkEventPoolScaling(b *testing.B) {
	pool := NewEventPool()

	// Pre-warm pool
	events := make([]*Event, 100)
	for i := range events {
		events[i] = pool.Get()
	}
	for _, event := range events {
		pool.Put(event)
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			event := pool.Get()

			// Simulate event processing
			event.SetContainerID("bench-container-123456789")
			event.SetPodUID("bench-pod-uid-123456789")
			event.Type = EventOOM
			event.ExitCode = 137
			event.Timestamp = time.Now().UnixNano()

			pool.Put(event)
		}
	})
}

func BenchmarkContainerStateChange(b *testing.B) {
	collector := setupBenchmarkCollector(b)
	defer collector.Stop()

	oldStatus := &cri.ContainerStatus{
		Id:    "benchmark-container",
		State: cri.ContainerState_CONTAINER_RUNNING,
	}

	newStatus := &cri.ContainerStatus{
		Id:       "benchmark-container",
		State:    cri.ContainerState_CONTAINER_EXITED,
		ExitCode: 137,
		Reason:   "OOMKilled",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		hasChanged := collector.hasStateChanged(oldStatus, newStatus)
		if !hasChanged {
			b.Error("Expected state change not detected")
		}

		eventType := collector.determineEventType(oldStatus, newStatus)
		if eventType != EventOOM {
			b.Error("Expected OOM event type")
		}
	}
}

func BenchmarkFilteringPerformance(b *testing.B) {
	config := DefaultConfig()

	containers := []*ContainerInfo{
		createTestContainer("app-1", "app-container", "default", false),
		createTestContainer("system-1", "pause", "kube-system", true),
		createTestContainer("app-2", "web-server", "production", false),
		createTestContainer("system-2", "kube-proxy", "kube-system", true),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		container := containers[i%len(containers)]
		_ = config.ShouldIncludeContainer(container)
	}
}

func BenchmarkUnifiedEventConversion(b *testing.B) {
	event := createOOMEvent("bench-container-123", "bench-pod", "benchmark")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		unifiedEvent := event.ToUnifiedEvent()

		// Access fields to prevent optimization
		_ = unifiedEvent.ID
		_ = unifiedEvent.Type
		_ = unifiedEvent.Severity
		_ = unifiedEvent.Message
		_ = len(unifiedEvent.Attributes)
	}
}

// Benchmark helper functions

func setupBenchmarkCollector(b *testing.B) *Collector {
	b.Helper()

	config := DefaultConfig()
	config.EventBufferSize = 50000 // Large buffer for benchmarks
	config.RingBufferSize = 16384  // Large ring buffer

	collector, err := NewCollector("benchmark", config)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}

	// Don't actually start the collector for benchmarks
	collector.isRunning.Store(true)

	return collector
}

func createBenchmarkContainerStatus() *cri.ContainerStatus {
	return &cri.ContainerStatus{
		Id:       "benchmark-container-1234567890abcdef",
		State:    cri.ContainerState_CONTAINER_EXITED,
		ExitCode: 137,
		Reason:   "OOMKilled",
		Labels: map[string]string{
			"io.kubernetes.pod.uid":       "benchmark-pod-uid-1234567890abcdef",
			"io.kubernetes.pod.name":      "benchmark-pod-name",
			"io.kubernetes.pod.namespace": "benchmark-namespace",
		},
		Annotations: map[string]string{
			"memory.usage": "2147483648", // 2GB
			"memory.limit": "1073741824", // 1GB
		},
	}
}

func reportBenchmarkMetrics(b *testing.B, collector *Collector) {
	b.Helper()

	// Report collector-specific metrics
	metrics := collector.metrics.GetMetrics()

	b.ReportMetric(float64(metrics["events_processed"].(uint64)), "events-processed")
	b.ReportMetric(float64(metrics["events_dropped"].(uint64)), "events-dropped")
	b.ReportMetric(float64(metrics["oom_kills_detected"].(uint64)), "oom-kills")
	b.ReportMetric(collector.ringBuffer.Usage(), "buffer-usage-%")

	// Calculate drop rate
	processed := metrics["events_processed"].(uint64)
	dropped := metrics["events_dropped"].(uint64)
	if processed+dropped > 0 {
		dropRate := float64(dropped) / float64(processed+dropped) * 100
		b.ReportMetric(dropRate, "drop-rate-%")
	}
}

// Stress test functions

func BenchmarkStressTest(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping stress test in short mode")
	}

	collector := setupBenchmarkCollector(b)
	defer collector.Stop()

	// Simulate high load scenario
	numProducers := runtime.NumCPU() * 2
	numConsumers := runtime.NumCPU()
	duration := 10 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	var totalEvents int64

	// Start consumers
	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			consumed := 0

			for {
				select {
				case <-ctx.Done():
					b.Logf("Consumer processed %d events", consumed)
					return
				default:
					if event := collector.ringBuffer.Read(); event != nil {
						consumed++
						collector.eventPool.Put(event)
					}
				}
			}
		}()
	}

	// Start producers
	status := createBenchmarkContainerStatus()
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			produced := 0

			for {
				select {
				case <-ctx.Done():
					b.Logf("Producer %d generated %d events", id, produced)
					totalEvents += int64(produced)
					return
				default:
					collector.createEvent(status, EventOOM)
					produced++
				}
			}
		}(i)
	}

	wg.Wait()

	metrics := collector.metrics.GetMetrics()
	b.ReportMetric(float64(totalEvents)/duration.Seconds(), "events/sec")
	b.ReportMetric(collector.ringBuffer.Usage(), "final-buffer-usage-%")

	if dropped := metrics["events_dropped"].(uint64); dropped > 0 {
		b.Logf("Dropped %d events under stress", dropped)
	}
}

func BenchmarkMemoryStressTest(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping memory stress test in short mode")
	}

	collector := setupBenchmarkCollector(b)
	defer collector.Stop()

	var initialMem runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&initialMem)

	status := createBenchmarkContainerStatus()

	b.ResetTimer()

	// Generate many events without consuming to test memory behavior
	for i := 0; i < 100000; i++ {
		collector.createEvent(status, EventOOM)

		// Periodically consume some events to prevent immediate buffer overflow
		if i%1000 == 0 {
			for j := 0; j < 100; j++ {
				if event := collector.ringBuffer.Read(); event != nil {
					collector.eventPool.Put(event)
				}
			}
		}
	}

	var finalMem runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&finalMem)

	memGrowth := finalMem.HeapInuse - initialMem.HeapInuse
	b.ReportMetric(float64(memGrowth), "heap-growth-bytes")
	b.ReportMetric(float64(finalMem.HeapObjects-initialMem.HeapObjects), "object-growth")
	b.ReportMetric(collector.ringBuffer.Usage(), "buffer-usage-%")

	metrics := collector.metrics.GetMetrics()
	b.Logf("Final metrics: processed=%d, dropped=%d, oom_kills=%d",
		metrics["events_processed"], metrics["events_dropped"], metrics["oom_kills_detected"])
}
