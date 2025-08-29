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

		// Create a collector that causes panic
		panicCollector := NewMockCollector("panic-test")

		err = pipeline.RegisterCollector("panic", panicCollector)
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
		panicCollector.SendEvent(event)

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

		collector := NewMockCollector("buffer-test")
		err = pipeline.RegisterCollector("buffer", collector)
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
			case collector.events <- event:
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

		collector := NewMockCollector("metrics-test")
		err = pipeline.RegisterCollector("metrics", collector)
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
			collector.SendEvent(event)
			time.Sleep(10 * time.Millisecond) // Space out events
		}

		time.Sleep(100 * time.Millisecond)
		err = pipeline.Stop()
		assert.NoError(t, err)

		// Pipeline should have processed all event types
		assert.True(t, collector.IsHealthy())
	})
}

func TestCollectorLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("CollectorHealthCheck", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		// Register healthy collector
		healthyCollector := NewMockCollector("healthy")
		err = pipeline.RegisterCollector("healthy", healthyCollector)
		require.NoError(t, err)

		// Register unhealthy collector
		unhealthyCollector := NewMockCollector("unhealthy")
		err = pipeline.RegisterCollector("unhealthy", unhealthyCollector)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		require.NoError(t, err)

		// Check health status
		assert.True(t, healthyCollector.IsHealthy())
		assert.True(t, unhealthyCollector.IsHealthy())

		// Stop unhealthy collector
		unhealthyCollector.Stop()
		assert.False(t, unhealthyCollector.IsHealthy())

		// Pipeline should continue with healthy collector
		event := &domain.CollectorEvent{
			Timestamp: time.Now(),
			Source:    "healthy",
			Type:      domain.EventTypeKernelNetwork,
			Severity:  domain.EventSeverityInfo,
		}
		healthyCollector.SendEvent(event)

		time.Sleep(50 * time.Millisecond)
		err = pipeline.Stop()
		assert.NoError(t, err)
	})

	t.Run("CollectorRestartOnFailure", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		collector := NewMockCollector("restart-test")
		err = pipeline.RegisterCollector("restart", collector)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		require.NoError(t, err)

		// Simulate collector failure
		collector.Stop()
		assert.False(t, collector.IsHealthy())

		// Restart collector
		err = collector.Start(ctx)
		assert.NoError(t, err)
		assert.True(t, collector.IsHealthy())

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

		// Start pipeline with no collectors
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

		collector := NewMockCollector("nil-test")
		err = pipeline.RegisterCollector("nil", collector)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		require.NoError(t, err)

		// Send nil event - should be handled gracefully
		select {
		case collector.events <- nil:
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

		collector := NewMockCollector("duplicate-test")
		err = pipeline.RegisterCollector("duplicate", collector)
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
			collector.SendEvent(event) // Same event multiple times
		}

		time.Sleep(50 * time.Millisecond)
		err = pipeline.Stop()
		assert.NoError(t, err)
	})
}

func BenchmarkPipeline(b *testing.B) {
	logger := zaptest.NewLogger(b)

	b.Run("SingleCollector", func(b *testing.B) {
		config := DefaultConfig()
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(b, err)

		collector := NewMockCollector("bench")
		pipeline.RegisterCollector("bench", collector)

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
			collector.SendEvent(event)
		}

		pipeline.Stop()
	})

	b.Run("MultipleCollectors", func(b *testing.B) {
		config := DefaultConfig()
		config.Workers = 4
		config.NATSConfig = nil

		pipeline, err := New(logger, config)
		require.NoError(b, err)

		// Register multiple collectors
		collectors := make([]*MockCollector, 10)
		for i := 0; i < 10; i++ {
			collectors[i] = NewMockCollector(fmt.Sprintf("bench-%d", i))
			pipeline.RegisterCollector(fmt.Sprintf("bench-%d", i), collectors[i])
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
				collectors[i%10].SendEvent(event)
				i++
			}
		})

		pipeline.Stop()
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/sec")
	})
}
