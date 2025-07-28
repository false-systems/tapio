package internal

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

// BenchmarkRateLimiter tests the performance of rate limiting
func BenchmarkRateLimiter_Allow(b *testing.B) {
	rl := NewRateLimiterSimple(100000) // 100k events/sec
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rl.Allow(ctx)
		}
	})
}

// BenchmarkCircuitBreaker tests circuit breaker performance
func BenchmarkCircuitBreaker_Call(b *testing.B) {
	cb := NewCircuitBreaker(1000, 30*time.Second)

	// Success function
	fn := func() error {
		return nil
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cb.Call(fn)
		}
	})
}

// BenchmarkEventValidator tests event validation performance
func BenchmarkEventValidator_ValidateEvent(b *testing.B) {
	validator := NewEventValidator()

	event := core.RawEvent{
		Type:      "network",
		Timestamp: time.Now(),
		CPU:       0,
		PID:       1234,
		TID:       1234,
		UID:       1000,
		GID:       1000,
		Comm:      "test-process",
		Data:      []byte("test event data with some payload"),
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			validator.ValidateEvent(event)
		}
	})
}

// BenchmarkBackpressureController tests backpressure decisions
func BenchmarkBackpressureController_ShouldAccept(b *testing.B) {
	bp := NewBackpressureController()
	bp.UpdateLoad(50.0) // 50% load

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			priority := EventPriority(i % 4)
			bp.ShouldAccept(priority)
			i++
		}
	})
}

// BenchmarkEventProcessor tests event processing performance
func BenchmarkEventProcessor_ProcessEvent(b *testing.B) {
	processor := newEventProcessor()
	ctx := context.Background()

	rawEvent := core.RawEvent{
		Type:      "network",
		Timestamp: time.Now(),
		CPU:       0,
		PID:       1234,
		TID:       1234,
		UID:       1000,
		GID:       1000,
		Comm:      "nginx",
		Data:      []byte("TCP connection from 192.168.1.100:54321 to 10.0.0.1:80"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := processor.ProcessEvent(ctx, rawEvent)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCollector_HighThroughput simulates high-throughput event processing
func BenchmarkCollector_HighThroughput(b *testing.B) {
	config := core.Config{
		Name:               "bench-high-throughput",
		Enabled:            true,
		EventBufferSize:    8192,
		MaxEventsPerSecond: 0, // No limit
	}

	collector, err := NewCollector(config)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		b.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Event consumer
	var eventsReceived atomic.Uint64
	go func() {
		for range collector.Events() {
			eventsReceived.Add(1)
		}
	}()

	// Let system stabilize
	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()

	// Run for fixed duration
	duration := 5 * time.Second
	timer := time.NewTimer(duration)
	<-timer.C

	totalEvents := eventsReceived.Load()
	eventsPerSec := float64(totalEvents) / duration.Seconds()

	b.ReportMetric(eventsPerSec, "events/sec")
	b.ReportMetric(float64(totalEvents), "total_events")
}

// BenchmarkProductionHardening_Combined tests all hardening features together
func BenchmarkProductionHardening_Combined(b *testing.B) {
	// Create components
	rateLimiter := NewRateLimiterSimple(50000)
	circuitBreaker := NewCircuitBreaker(100, 30*time.Second)
	validator := NewEventValidator()
	backpressure := NewBackpressureController()

	ctx := context.Background()
	backpressure.UpdateLoad(30.0) // 30% load

	// Test event
	event := core.RawEvent{
		Type:      "syscall",
		Timestamp: time.Now(),
		CPU:       0,
		PID:       5678,
		TID:       5678,
		UID:       1000,
		GID:       1000,
		Comm:      "test-app",
		Data:      []byte("syscall: open(/etc/hosts, O_RDONLY)"),
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Full hardening pipeline
			if !rateLimiter.Allow(ctx) {
				continue
			}

			if err := validator.ValidateEvent(event); err != nil {
				continue
			}

			priority := DetermineEventPriority(event.Type)
			if !backpressure.ShouldAccept(priority) {
				continue
			}

			circuitBreaker.Call(func() error {
				// Simulate event processing
				time.Sleep(time.Microsecond)
				return nil
			})
		}
	})
}

// BenchmarkMemoryAllocation tests memory allocation patterns
func BenchmarkMemoryAllocation_EventCreation(b *testing.B) {
	processor := newEventProcessor()
	ctx := context.Background()

	rawEvents := make([]core.RawEvent, 1000)
	for i := range rawEvents {
		rawEvents[i] = core.RawEvent{
			Type:      "memory",
			Timestamp: time.Now(),
			CPU:       uint32(i % 4),
			PID:       uint32(1000 + i),
			TID:       uint32(1000 + i),
			UID:       1000,
			GID:       1000,
			Comm:      "process",
			Data:      make([]byte, 64),
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		event := rawEvents[i%len(rawEvents)]
		_, _ = processor.ProcessEvent(ctx, event)
	}
}

// BenchmarkConcurrentEventProcessing tests concurrent event processing
func BenchmarkConcurrentEventProcessing(b *testing.B) {
	numWorkers := 8
	eventsPerWorker := b.N / numWorkers

	config := core.Config{
		Name:               "bench-concurrent",
		Enabled:            true,
		EventBufferSize:    8192,
		MaxEventsPerSecond: 0,
	}

	collector, err := NewCollector(config)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		b.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Consumer
	var processed atomic.Uint64
	for i := 0; i < numWorkers; i++ {
		go func() {
			for range collector.Events() {
				processed.Add(1)
			}
		}()
	}

	b.ResetTimer()

	// Producers
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < eventsPerWorker; j++ {
				event := core.RawEvent{
					Type:      "concurrent",
					Timestamp: time.Now(),
					CPU:       uint32(workerID),
					PID:       uint32(1000 + j),
					Data:      []byte("concurrent event"),
				}

				// Simulate event processing
				_ = event
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalProcessed := processed.Load()
	throughput := float64(totalProcessed) / elapsed.Seconds()

	b.ReportMetric(throughput, "events/sec")
	b.ReportMetric(float64(numWorkers), "workers")
}
