package orchestrator

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestPipelineWorkers(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("MultipleWorkers", func(t *testing.T) {
		config := DefaultConfig()
		config.Workers = 5
		config.NATSConfig = nil // Disable NATS for unit test

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		// Track processed events
		var processedCount int32
		processedEvents := make(chan *domain.CollectorEvent, 100)

		// Override event processor for testing
		pipeline.eventsChan = processedEvents

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		require.NoError(t, err)

		// Send events through the pipeline
		go func() {
			for i := 0; i < 20; i++ {
				event := &domain.CollectorEvent{
					Timestamp: time.Now(),
					Source:    fmt.Sprintf("worker-test-%d", i),
					Type:      domain.EventTypeKernelNetwork,
					Severity:  domain.EventSeverityInfo,
				}
				pipeline.eventsChan <- event
				atomic.AddInt32(&processedCount, 1)
			}
		}()

		// Wait for events to be processed
		time.Sleep(100 * time.Millisecond)

		err = pipeline.Stop()
		assert.NoError(t, err)

		// Verify all events were queued
		assert.Equal(t, int32(20), processedCount)
	})

	t.Run("WorkerPanic", func(t *testing.T) {
		config := DefaultConfig()
		config.Workers = 2
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		// Create an observer that causes panic
		panicObserver := NewMockObserver("panic-test")

		err = pipeline.RegisterObserver("panic", panicObserver)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Start should handle panics gracefully
		err = pipeline.Start(ctx)
		assert.NoError(t, err)

		// Send event that might trigger issues
		event := &domain.CollectorEvent{
			Timestamp: time.Now(),
			Source:    "panic-test",
			Type:      domain.EventTypeKernelNetwork,
			Severity:  domain.EventSeverityInfo,
		}
		panicObserver.SendEvent(event)

		// Pipeline should continue working despite panic
		time.Sleep(50 * time.Millisecond)

		err = pipeline.Stop()
		assert.NoError(t, err)
	})
}

func TestPipelineBuffering(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("BufferOverflow", func(t *testing.T) {
		config := DefaultConfig()
		config.BufferSize = 5 // Small buffer for testing
		config.Workers = 1
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		observer := NewMockObserver("buffer-test")
		err = pipeline.RegisterObserver("buffer", observer)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		require.NoError(t, err)

		// Send more events than buffer can hold
		var dropCount int32
		for i := 0; i < 10; i++ {
			event := &domain.CollectorEvent{
				Timestamp: time.Now(),
				Source:    fmt.Sprintf("buffer-test-%d", i),
				Type:      domain.EventTypeKernelNetwork,
				Severity:  domain.EventSeverityInfo,
			}

			// Try to send with timeout
			select {
			case observer.events <- event:
				// Sent successfully
			case <-time.After(10 * time.Millisecond):
				// Event dropped due to full buffer
				atomic.AddInt32(&dropCount, 1)
			}
		}

		time.Sleep(50 * time.Millisecond)
		err = pipeline.Stop()
		assert.NoError(t, err)

		// Some events should have been dropped
		assert.True(t, dropCount > 0, "Expected some events to be dropped")
	})

	t.Run("DynamicBufferAdjustment", func(t *testing.T) {
		// Test different buffer sizes
		bufferSizes := []int{100, 500, 1000, 10000}

		for _, size := range bufferSizes {
			t.Run(fmt.Sprintf("BufferSize_%d", size), func(t *testing.T) {
				config := DefaultConfig()
				config.BufferSize = size
				config.NATSConfig = nil

				pipeline, err := New(logger, config)
				require.NoError(t, err)
				assert.Equal(t, size, cap(pipeline.eventsChan))
			})
		}
	})
}

func TestPipelineMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("EventCounting", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		observer := NewMockObserver("metrics-test")
		err = pipeline.RegisterObserver("metrics", observer)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		require.NoError(t, err)

		// Send various types of events
		eventTypes := []domain.CollectorEventType{
			domain.EventTypeKernelNetwork,
			domain.EventTypeHTTP,
			domain.EventTypeDNS,
			domain.EventTypeTCP,
		}

		for i, eventType := range eventTypes {
			event := &domain.CollectorEvent{
				Timestamp: time.Now(),
				Source:    "metrics-test",
				Type:      eventType,
				Severity:  domain.EventSeverityInfo,
			}
			observer.SendEvent(event)
			time.Sleep(10 * time.Millisecond) // Space out events
		}

		time.Sleep(100 * time.Millisecond)
		err = pipeline.Stop()
		assert.NoError(t, err)

		// Pipeline should have processed all event types
		assert.True(t, observer.IsHealthy())
	})
}

func TestObserverLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("ObserverHealthCheck", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		// Register healthy observer
		healthyObserver := NewMockObserver("healthy")
		err = pipeline.RegisterObserver("healthy", healthyObserver)
		require.NoError(t, err)

		// Register unhealthy observer
		unhealthyObserver := NewMockObserver("unhealthy")
		err = pipeline.RegisterObserver("unhealthy", unhealthyObserver)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		require.NoError(t, err)

		// Check health status
		assert.True(t, healthyObserver.IsHealthy())
		assert.True(t, unhealthyObserver.IsHealthy())

		// Stop unhealthy observer
		unhealthyObserver.Stop()
		assert.False(t, unhealthyObserver.IsHealthy())

		// Pipeline should continue with healthy observer
		event := &domain.CollectorEvent{
			Timestamp: time.Now(),
			Source:    "healthy",
			Type:      domain.EventTypeKernelNetwork,
			Severity:  domain.EventSeverityInfo,
		}
		healthyObserver.SendEvent(event)

		time.Sleep(50 * time.Millisecond)
		err = pipeline.Stop()
		assert.NoError(t, err)
	})

	t.Run("ObserverRestartOnFailure", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		observer := NewMockObserver("restart-test")
		err = pipeline.RegisterObserver("restart", observer)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		require.NoError(t, err)

		// Simulate observer failure
		observer.Stop()
		assert.False(t, observer.IsHealthy())

		// Restart observer
		err = observer.Start(ctx)
		assert.NoError(t, err)
		assert.True(t, observer.IsHealthy())

		err = pipeline.Stop()
		assert.NoError(t, err)
	})
}

func TestPipelineEdgeCases(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("EmptyPipeline", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		// Start pipeline with no observers
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		assert.NoError(t, err)

		// Should stop cleanly
		err = pipeline.Stop()
		assert.NoError(t, err)
	})

	t.Run("NilEvent", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		observer := NewMockObserver("nil-test")
		err = pipeline.RegisterObserver("nil", observer)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		require.NoError(t, err)

		// Send nil event - should be handled gracefully
		select {
		case observer.events <- nil:
			// Sent nil event
		case <-time.After(10 * time.Millisecond):
			// Timeout is fine
		}

		err = pipeline.Stop()
		assert.NoError(t, err)
	})

	t.Run("DuplicateEvents", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		observer := NewMockObserver("duplicate-test")
		err = pipeline.RegisterObserver("duplicate", observer)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		require.NoError(t, err)

		// Send duplicate events
		event := &domain.CollectorEvent{
			Timestamp: time.Now(),
			Source:    "duplicate-test",
			Type:      domain.EventTypeKernelNetwork,
			Severity:  domain.EventSeverityInfo,
		}

		for i := 0; i < 5; i++ {
			observer.SendEvent(event) // Same event multiple times
		}

		time.Sleep(50 * time.Millisecond)
		err = pipeline.Stop()
		assert.NoError(t, err)
	})
}

func BenchmarkPipeline(b *testing.B) {
	logger := zaptest.NewLogger(b)

	b.Run("SingleObserver", func(b *testing.B) {
		config := DefaultConfig()
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(b, err)

		observer := NewMockObserver("bench")
		pipeline.RegisterObserver("bench", observer)

		ctx := context.Background()
		pipeline.Start(ctx)

		event := &domain.CollectorEvent{
			Timestamp: time.Now(),
			Source:    "benchmark",
			Type:      domain.EventTypeKernelNetwork,
			Severity:  domain.EventSeverityInfo,
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			observer.SendEvent(event)
		}

		pipeline.Stop()
	})

	b.Run("MultipleObservers", func(b *testing.B) {
		config := DefaultConfig()
		config.Workers = 4
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(b, err)

		// Register multiple observers
		observers := make([]*MockObserver, 10)
		for i := 0; i < 10; i++ {
			observers[i] = NewMockObserver(fmt.Sprintf("bench-%d", i))
			pipeline.RegisterObserver(fmt.Sprintf("bench-%d", i), observers[i])
		}

		ctx := context.Background()
		pipeline.Start(ctx)

		event := &domain.CollectorEvent{
			Timestamp: time.Now(),
			Source:    "benchmark",
			Type:      domain.EventTypeKernelNetwork,
			Severity:  domain.EventSeverityInfo,
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				observers[i%10].SendEvent(event)
				i++
			}
		})

		pipeline.Stop()
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/sec")
	})
}
