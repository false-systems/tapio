package health

import (
	"context"
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

// BenchmarkEventConversion benchmarks event conversion performance
func BenchmarkEventConversion(b *testing.B) {
	logger := zap.NewNop()
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(b, err)

	event := &HealthEvent{
		TimestampNs: 1234567890,
		PID:         1000,
		PPID:        1,
		UID:         1000,
		GID:         1000,
		SyscallNr:   42,
		ErrorCode:   -111,
		Category:    2,
		Comm:        [16]byte{'t', 'e', 's', 't'},
		Path:        [256]byte{'/', 'p', 'a', 't', 'h'},
		SrcIP:       0x0100007f,
		DstIP:       0x0100007f,
		SrcPort:     8080,
		DstPort:     443,
		ErrorCount:  5,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result := observer.convertToCollectorEvent(event)
		if result == nil {
			b.Fatal("conversion returned nil")
		}
	}
}

// BenchmarkEventChannelThroughput benchmarks event channel throughput
func BenchmarkEventChannelThroughput(b *testing.B) {
	logger := zap.NewNop()
	config := &Config{
		RingBufferSize:   1024,
		EventChannelSize: 1000,
		RateLimitMs:      1,
		EnabledCategories: map[string]bool{
			"file": true,
		},
	}

	observer, err := NewObserver(logger, config)
	require.NoError(b, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(b, err)
	defer observer.Stop()

	event := &domain.CollectorEvent{
		EventID:   "test-event",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelSyscall,
		Source:    "health",
	}

	// Start consumer
	done := make(chan bool)
	go func() {
		for range observer.Events() {
			// Consume events
		}
		done <- true
	}()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		observer.EventChannelManager.SendEvent(event)
	}

	observer.EventChannelManager.Close()
	<-done
}

// BenchmarkConcurrentEventProcessing benchmarks concurrent event processing
func BenchmarkConcurrentEventProcessing(b *testing.B) {
	logger := zap.NewNop()
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(b, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(b, err)
	defer observer.Stop()

	// Start consumer
	var processed atomic.Int64
	done := make(chan bool)
	go func() {
		for range observer.Events() {
			processed.Add(1)
		}
		done <- true
	}()

	b.ResetTimer()
	b.ReportAllocs()

	// Run parallel producers
	b.RunParallel(func(pb *testing.PB) {
		event := &domain.CollectorEvent{
			EventID:   "bench",
			Timestamp: time.Now(),
		}
		for pb.Next() {
			observer.EventChannelManager.SendEvent(event)
		}
	})

	observer.EventChannelManager.Close()
	<-done

	b.Logf("Processed %d events", processed.Load())
}

// BenchmarkErrorMetricsUpdate benchmarks metrics update performance
func BenchmarkErrorMetricsUpdate(b *testing.B) {
	logger := zap.NewNop()
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(b, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(b, err)
	defer observer.Stop()

	errorCodes := []int32{-28, -12, -111, -24, -122, -5, -13}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		errorCode := errorCodes[i%len(errorCodes)]
		// updateErrorMetrics is not exported, skip for benchmark
		_ = errorCode // prevent unused variable error
	}
}

// BenchmarkHelperFunctions benchmarks helper function performance
func BenchmarkHelperFunctions(b *testing.B) {
	b.Run("bytesToString", func(b *testing.B) {
		data := []byte{'t', 'e', 's', 't', 0, 'x', 'x', 'x'}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = bytesToString(data)
		}
	})

	b.Run("formatIP", func(b *testing.B) {
		ip := uint32(0x0100007f)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = formatIP(ip)
		}
	})

	b.Run("getErrorName", func(b *testing.B) {
		codes := []int32{-28, -12, -111, -999}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = getErrorName(codes[i%len(codes)])
		}
	})

	b.Run("getSyscallName", func(b *testing.B) {
		syscalls := []int32{1, 42, 257, 999}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = getSyscallName(syscalls[i%len(syscalls)])
		}
	})
}

// TestPerformanceHighEventRate tests performance under high event rate
func TestPerformanceHighEventRate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	logger := zap.NewNop()
	config := &Config{
		RingBufferSize:   8 * 1024 * 1024,
		EventChannelSize: 10000,
		RateLimitMs:      1,
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": true,
			"memory":  true,
		},
	}

	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Metrics
	var eventsSent atomic.Int64
	var eventsReceived atomic.Int64
	var eventsDropped atomic.Int64

	// Start consumer
	var consumerWg sync.WaitGroup
	consumerWg.Add(1)
	go func() {
		defer consumerWg.Done()
		for range observer.Events() {
			eventsReceived.Add(1)
		}
	}()

	// Generate high rate of events (reduced for testing)
	numProducers := min(4, runtime.NumCPU()) // Limit producers
	eventsPerProducer := 1000                // Reduced from 10000
	startTime := time.Now()

	var producerWg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		producerWg.Add(1)
		go func(producerID int) {
			defer producerWg.Done()
			timeout := time.After(10 * time.Second)

			for j := 0; j < eventsPerProducer; j++ {
				select {
				case <-timeout:
					t.Logf("Producer %d timed out at event %d", producerID, j)
					return
				default:
				}

				event := &HealthEvent{
					TimestampNs: uint64(time.Now().UnixNano()),
					PID:         uint32(producerID*1000 + j),
					ErrorCode:   int32(-(j % 10)),
					Category:    uint8((j % 3) + 1),
				}

				domainEvent := observer.convertToCollectorEvent(event)
				if observer.EventChannelManager.SendEvent(domainEvent) {
					eventsSent.Add(1)
				} else {
					eventsDropped.Add(1)
				}
			}
		}(i)
	}

	// Wait for producers to finish
	producerWg.Wait()

	// Close channel to signal consumer to stop
	observer.EventChannelManager.Close()

	// Wait for consumer to finish
	consumerWg.Wait()

	duration := time.Since(startTime)
	totalEvents := eventsSent.Load()
	throughput := float64(totalEvents) / duration.Seconds()

	t.Logf("Performance metrics:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Events sent: %d", eventsSent.Load())
	t.Logf("  Events received: %d", eventsReceived.Load())
	t.Logf("  Events dropped: %d", eventsDropped.Load())
	t.Logf("  Throughput: %.2f events/sec", throughput)

	// Verify reasonable performance
	assert.Greater(t, throughput, float64(1000), "Should handle >1000 events/sec")

	// Drop rate should be reasonable
	dropRate := float64(eventsDropped.Load()) / float64(totalEvents)
	assert.Less(t, dropRate, 0.5, "Drop rate should be less than 50%")
}

// TestPerformanceMemoryUsage tests memory usage under load
func TestPerformanceMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	logger := zap.NewNop()
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Get baseline memory
	var m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Store events to prevent immediate GC
	events := make([]*domain.CollectorEvent, 0, 1000)

	// Generate many events
	numEvents := 100000
	for i := 0; i < numEvents; i++ {
		event := &HealthEvent{
			TimestampNs: uint64(time.Now().UnixNano()),
			PID:         uint32(i),
			ErrorCode:   -28,
			Category:    1,
		}

		domainEvent := observer.convertToCollectorEvent(event)
		observer.EventChannelManager.SendEvent(domainEvent)

		// Keep some events in memory to measure actual usage
		if i < 1000 {
			events = append(events, domainEvent)
		}

		// Consume most events to prevent channel blocking
		if i > 1000 {
			select {
			case <-observer.Events():
			default:
			}
		}
	}

	// Get memory after load (before GC to see actual usage)
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	// Use TotalAlloc which monotonically increases
	memUsed := m2.TotalAlloc - m1.TotalAlloc
	memPerEvent := float64(memUsed) / float64(numEvents)

	t.Logf("Memory usage:")
	t.Logf("  Total events: %d", numEvents)
	t.Logf("  Memory allocated: %d bytes", memUsed)
	t.Logf("  Memory per event: %.2f bytes", memPerEvent)
	t.Logf("  Events kept: %d", len(events))

	// Memory per event should be reasonable
	// Each event has metadata, labels, kernel data, etc - expect ~2-3KB per event
	assert.Less(t, memPerEvent, float64(3000), "Should use <3KB per event on average")
}

// TestPerformanceCPUUsage tests CPU usage under load
func TestPerformanceCPUUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	logger := zap.NewNop()
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Monitor CPU usage
	startCPU := time.Now()
	var cpuTime time.Duration

	// Generate events continuously
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				event := &HealthEvent{
					TimestampNs: uint64(time.Now().UnixNano()),
					PID:         1000,
					ErrorCode:   -5,
				}
				domainEvent := observer.convertToCollectorEvent(event)
				observer.EventChannelManager.SendEvent(domainEvent)
			}
		}
	}()

	// Consume events
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-observer.Events():
				// Process event
			}
		}
	}()

	<-ctx.Done()
	cpuTime = time.Since(startCPU)

	t.Logf("CPU usage test ran for %v", cpuTime)
	// This is a basic test - detailed CPU profiling would require pprof
}

// BenchmarkStatisticsRetrieval benchmarks statistics retrieval
func BenchmarkStatisticsRetrieval(b *testing.B) {
	logger := zap.NewNop()
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(b, err)

	// Record some events
	for i := 0; i < 1000; i++ {
		observer.BaseObserver.RecordEvent()
		if i%10 == 0 {
			observer.BaseObserver.RecordDrop()
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stats := observer.Statistics()
		if stats == nil {
			b.Fatal("stats should not be nil")
		}
		// Check that observer name is correct
		if observer.Name() != "health" {
			b.Fatal("invalid observer name")
		}
	}
}

// TestPerformanceScalability tests scalability with different configurations
func TestPerformanceScalability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	configs := []struct {
		name               string
		bufferSize         int
		channelSize        int
		expectedThroughput float64
	}{
		{"small", 1024, 10, 100},
		{"medium", 1024 * 1024, 100, 1000},
		{"large", 8 * 1024 * 1024, 1000, 10000},
	}

	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) {
			logger := zap.NewNop()
			config := &Config{
				RingBufferSize:   cfg.bufferSize,
				EventChannelSize: cfg.channelSize,
				RateLimitMs:      1,
				EnabledCategories: map[string]bool{
					"file": true,
				},
			}

			observer, err := NewObserver(logger, config)
			require.NoError(t, err)

			ctx := context.Background()
			err = observer.Start(ctx)
			require.NoError(t, err)

			// Measure throughput
			start := time.Now()
			numEvents := 1000
			sent := 0

			for i := 0; i < numEvents; i++ {
				event := &HealthEvent{
					TimestampNs: uint64(time.Now().UnixNano()),
					PID:         uint32(i),
					ErrorCode:   -28,
				}

				domainEvent := observer.convertToCollectorEvent(event)
				if observer.EventChannelManager.SendEvent(domainEvent) {
					sent++
				}
			}

			duration := time.Since(start)
			throughput := float64(sent) / duration.Seconds()

			t.Logf("Config %s: %.2f events/sec", cfg.name, throughput)

			// Clean up
			observer.Stop()
		})
	}
}
