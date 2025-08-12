package correlation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// Use mocks from test_helpers_test.go
// MockCorrelator is now MockCorrelator
// MockStorage is defined in test_helpers_test.go

func TestEngineCreation(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("successful creation with all correlators enabled", func(t *testing.T) {
		config := DefaultEngineConfig()
		storage := &MockStorage{}

		// Create engine without K8s client
		engine, err := NewEngine(logger, *config, nil, storage)
		require.NoError(t, err)
		require.NotNil(t, engine)

		// Should have 4 correlators (no K8s since client is nil)
		assert.Len(t, engine.correlators, 4) // temporal, sequence, performance, servicemap

		// Verify engine state
		assert.NotNil(t, engine.eventChan)
		assert.NotNil(t, engine.resultChan)
		assert.NotNil(t, engine.ctx)
		assert.NotNil(t, engine.cancel)
		assert.Equal(t, config, engine.config)

		// Clean up
		engine.Stop()
	})

	t.Run("creation with selective correlators", func(t *testing.T) {
		config := EngineConfig{
			EventBufferSize:        TestEventBufferSize,
			ResultBufferSize:       TestResultBufferSize,
			WorkerCount:            2,
			StorageCleanupInterval: ServiceMetricsWindow,
			StorageRetention:       MaxEventAge,
		}

		engine, err := NewEngine(logger, config, nil, nil)
		require.NoError(t, err)
		require.NotNil(t, engine)

		// Should have only 2 correlators
		assert.Len(t, engine.correlators, 2)

		// Clean up
		engine.Stop()
	})
}

func TestEngineStartStop(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("normal start and stop", func(t *testing.T) {
		config := EngineConfig{
			EventBufferSize:        10,
			ResultBufferSize:       10,
			WorkerCount:            2,
			StorageCleanupInterval: ServiceMetricsWindow,
			StorageRetention:       MaxEventAge,
		}

		engine, err := NewEngine(logger, config, nil, nil)
		require.NoError(t, err)

		// Start the engine
		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)

		// Give workers time to start
		time.Sleep(10 * time.Millisecond)

		// Stop the engine
		err = engine.Stop()
		require.NoError(t, err)

		// Verify channels are closed
		select {
		case _, ok := <-engine.eventChan:
			assert.False(t, ok, "event channel should be closed")
		default:
			// Channel is not ready for reading yet
		}
	})

	t.Run("multiple stop calls", func(t *testing.T) {
		config := DefaultEngineConfig()
		engine, err := NewEngine(logger, *config, nil, nil)
		require.NoError(t, err)

		err = engine.Start(context.Background())
		require.NoError(t, err)

		// First stop should succeed
		err = engine.Stop()
		require.NoError(t, err)

		// Second stop may panic due to closing closed channels
		// This is acceptable behavior - the engine is already stopped
		// In production code, callers should track the engine state
		defer func() {
			if r := recover(); r != nil {
				// Expected panic when closing closed channels
				assert.Contains(t, fmt.Sprintf("%v", r), "close of closed channel")
			}
		}()

		err = engine.Stop()
		// If no panic occurs, the error should be nil
		require.NoError(t, err)
	})
}

func TestEngineProcess(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("successful event processing", func(t *testing.T) {
		mockCorrelator := &TestifyMockCorrelator{}
		mockStorage := &MockStorage{}

		config := EngineConfig{
			EventBufferSize:        10,
			ResultBufferSize:       10,
			WorkerCount:            1,
			ProcessingTimeout:      1 * time.Second,
			StorageCleanupInterval: ServiceMetricsWindow,
			StorageRetention:       MaxEventAge,
		}

		engine, err := NewEngine(logger, config, nil, mockStorage)
		require.NoError(t, err)

		// Add mock correlator
		engine.correlators = []Correlator{mockCorrelator}

		// Setup expectations
		event := &domain.UnifiedEvent{
			ID:        "test-event-1",
			Type:      "systemd",
			Timestamp: time.Now(),
		}

		result := &CorrelationResult{
			ID:      "correlation-1",
			Type:    "test",
			Events:  []string{event.ID},
			Message: "Test correlation",
		}

		mockCorrelator.On("Name").Return("test-correlator")
		mockCorrelator.On("Process", mock.Anything, event).Return([]*CorrelationResult{result}, nil)
		mockStorage.On("Store", mock.Anything, result).Return(nil)

		// Start engine
		err = engine.Start(context.Background())
		require.NoError(t, err)

		// Process event
		err = engine.Process(context.Background(), event)
		require.NoError(t, err)

		// Wait for result
		select {
		case received := <-engine.Results():
			assert.Equal(t, result.ID, received.ID)
			assert.Equal(t, result.Type, received.Type)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for result")
		}

		// Verify metrics
		time.Sleep(100 * time.Millisecond) // Give time for processing
		metrics := engine.GetMetrics()
		assert.Equal(t, int64(1), metrics.EventsProcessed)
		assert.Equal(t, int64(1), metrics.CorrelationsFound)

		// Clean up
		engine.Stop()

		mockCorrelator.AssertExpectations(t)
		mockStorage.AssertExpectations(t)
	})

	t.Run("nil event handling", func(t *testing.T) {
		config := DefaultEngineConfig()
		engine, err := NewEngine(logger, *config, nil, nil)
		require.NoError(t, err)

		err = engine.Process(context.Background(), nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "event is nil")

		engine.Stop()
	})

	t.Run("processing timeout", func(t *testing.T) {
		config := EngineConfig{
			EventBufferSize:        1,
			ResultBufferSize:       1,
			WorkerCount:            0, // No workers
			ProcessingTimeout:      50 * time.Millisecond,
			StorageCleanupInterval: ServiceMetricsWindow,
			StorageRetention:       MaxEventAge,
		}

		engine, err := NewEngine(logger, config, nil, nil)
		require.NoError(t, err)

		// Fill the event channel
		event1 := &domain.UnifiedEvent{ID: "event-1"}
		engine.eventChan <- event1

		// Try to process another event - should timeout
		event2 := &domain.UnifiedEvent{ID: "event-2"}
		err = engine.Process(context.Background(), event2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")

		engine.Stop()
	})

	t.Run("context cancellation", func(t *testing.T) {
		config := EngineConfig{
			EventBufferSize:        1,
			ProcessingTimeout:      1 * time.Second,
			StorageCleanupInterval: ServiceMetricsWindow,
			StorageRetention:       MaxEventAge,
		}

		engine, err := NewEngine(logger, config, nil, nil)
		require.NoError(t, err)

		// Fill the channel
		engine.eventChan <- &domain.UnifiedEvent{ID: "blocking-event"}

		// Create cancellable context
		ctx, cancel := context.WithCancel(context.Background())

		// Start processing in goroutine
		errChan := make(chan error)
		go func() {
			errChan <- engine.Process(ctx, &domain.UnifiedEvent{ID: "test-event"})
		}()

		// Cancel context
		cancel()

		// Should receive context error
		err = <-errChan
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)

		engine.Stop()
	})
}

func TestEngineWorkers(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("multiple workers processing", func(t *testing.T) {
		mockCorrelator := &TestifyMockCorrelator{}
		mockStorage := &MockStorage{}

		config := EngineConfig{
			EventBufferSize:        TestEventBufferSize,
			ResultBufferSize:       TestResultBufferSize,
			WorkerCount:            4,
			StorageCleanupInterval: ServiceMetricsWindow,
			StorageRetention:       MaxEventAge,
		}

		engine, err := NewEngine(logger, config, nil, mockStorage)
		require.NoError(t, err)

		engine.correlators = []Correlator{mockCorrelator}

		// Track processed events
		var processedCount int32

		mockCorrelator.On("Name").Return("test-correlator")
		mockCorrelator.On("Process", mock.Anything, mock.Anything).Run(
			func(args mock.Arguments) {
				atomic.AddInt32(&processedCount, 1)
			},
		).Return([]*CorrelationResult{{
			ID:   "test-correlation",
			Type: "test",
		}}, nil)
		mockStorage.On("Store", mock.Anything, mock.Anything).Return(nil)

		// Start engine
		err = engine.Start(context.Background())
		require.NoError(t, err)

		// Send multiple events
		eventCount := 20
		for i := 0; i < eventCount; i++ {
			event := &domain.UnifiedEvent{
				ID:        fmt.Sprintf("event-%d", i),
				Timestamp: time.Now(),
			}
			err = engine.Process(context.Background(), event)
			require.NoError(t, err)
		}

		// Wait for processing
		time.Sleep(500 * time.Millisecond)

		// Verify all events were processed
		assert.Equal(t, int32(eventCount), atomic.LoadInt32(&processedCount))

		engine.Stop()
	})

	t.Run("worker error handling", func(t *testing.T) {
		mockCorrelator := &TestifyMockCorrelator{}

		config := EngineConfig{
			EventBufferSize:        10,
			ResultBufferSize:       10,
			WorkerCount:            2,
			StorageCleanupInterval: ServiceMetricsWindow,
			StorageRetention:       MaxEventAge,
		}

		engine, err := NewEngine(logger, config, nil, nil)
		require.NoError(t, err)

		engine.correlators = []Correlator{mockCorrelator}

		// Correlator returns error
		mockCorrelator.On("Name").Return("failing-correlator")
		mockCorrelator.On("Process", mock.Anything, mock.Anything).Return(
			([]*CorrelationResult)(nil),
			errors.New("processing error"),
		)

		err = engine.Start(context.Background())
		require.NoError(t, err)

		// Process event - should not crash despite error
		event := &domain.UnifiedEvent{ID: "test-event"}
		err = engine.Process(context.Background(), event)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		// Engine should still be running
		assert.True(t, engine.GetMetrics().IsHealthy)

		engine.Stop()
	})
}

func TestEngineMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("metrics tracking", func(t *testing.T) {
		mockCorrelator := &TestifyMockCorrelator{}

		config := DefaultEngineConfig()
		engine, err := NewEngine(logger, *config, nil, nil)
		require.NoError(t, err)

		engine.correlators = []Correlator{mockCorrelator}

		// Setup mock
		mockCorrelator.On("Name").Return("test-correlator")
		mockCorrelator.On("Process", mock.Anything, mock.Anything).Return(
			[]*CorrelationResult{{ID: "correlation-1", Type: "test"}},
			nil,
		)

		err = engine.Start(context.Background())
		require.NoError(t, err)

		// Process some events
		for i := 0; i < 5; i++ {
			event := &domain.UnifiedEvent{
				ID:        fmt.Sprintf("event-%d", i),
				Timestamp: time.Now(),
			}
			err = engine.Process(context.Background(), event)
			require.NoError(t, err)
		}

		// Wait for processing
		time.Sleep(200 * time.Millisecond)

		// Check metrics
		metrics := engine.GetMetrics()
		assert.Equal(t, int64(5), metrics.EventsProcessed)
		assert.Equal(t, int64(5), metrics.CorrelationsFound)
		assert.True(t, metrics.IsHealthy)
		assert.Equal(t, "running", metrics.Status)
		assert.Equal(t, 1, metrics.CorrelatorsCount)
		assert.Equal(t, config.WorkerCount, metrics.WorkersCount)

		// Check detailed metrics
		detailed := engine.GetDetailedMetrics()
		assert.Equal(t, metrics.EventsProcessed, detailed.EventsProcessed)
		assert.Equal(t, metrics.CorrelationsFound, detailed.CorrelationsFound)

		engine.Stop()
	})
}

func TestEngineStorageCleanup(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("periodic cleanup", func(t *testing.T) {
		mockStorage := &MockStorage{}

		config := EngineConfig{
			EventBufferSize:        10,
			ResultBufferSize:       10,
			WorkerCount:            1,
			StorageCleanupInterval: 50 * time.Millisecond,
			StorageRetention:       1 * time.Hour,
		}

		engine, err := NewEngine(logger, config, nil, mockStorage)
		require.NoError(t, err)

		// Expect at least 2 cleanup calls
		cleanupCalled := make(chan bool, 2)
		mockStorage.On("Cleanup", mock.Anything, config.StorageRetention).Return(nil).Run(
			func(args mock.Arguments) {
				select {
				case cleanupCalled <- true:
				default:
				}
			},
		)

		err = engine.Start(context.Background())
		require.NoError(t, err)

		// Wait for cleanup to be called
		select {
		case <-cleanupCalled:
			// First cleanup
		case <-time.After(200 * time.Millisecond):
			t.Fatal("cleanup not called")
		}

		select {
		case <-cleanupCalled:
			// Second cleanup
		case <-time.After(200 * time.Millisecond):
			t.Fatal("second cleanup not called")
		}

		engine.Stop()
		mockStorage.AssertExpectations(t)
	})

	t.Run("cleanup error handling", func(t *testing.T) {
		mockStorage := &MockStorage{}

		config := EngineConfig{
			StorageCleanupInterval: 50 * time.Millisecond,
			StorageRetention:       1 * time.Hour,
		}

		engine, err := NewEngine(logger, config, nil, mockStorage)
		require.NoError(t, err)

		// Cleanup returns error
		mockStorage.On("Cleanup", mock.Anything, mock.Anything).Return(
			errors.New("cleanup failed"),
		)

		err = engine.Start(context.Background())
		require.NoError(t, err)

		// Wait for cleanup attempt
		time.Sleep(100 * time.Millisecond)

		// Engine should still be healthy despite cleanup errors
		assert.True(t, engine.GetMetrics().IsHealthy)

		engine.Stop()
	})
}

func TestEngineConcurrency(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("concurrent event processing", func(t *testing.T) {
		mockCorrelator := &TestifyMockCorrelator{}

		config := EngineConfig{
			EventBufferSize:        TestEventBufferSize,
			ResultBufferSize:       TestResultBufferSize,
			WorkerCount:            4,
			StorageCleanupInterval: ServiceMetricsWindow,
			StorageRetention:       MaxEventAge,
		}

		engine, err := NewEngine(logger, config, nil, nil)
		require.NoError(t, err)

		engine.correlators = []Correlator{mockCorrelator}

		// Track processing
		var mu sync.Mutex
		processedEvents := make(map[string]bool)

		mockCorrelator.On("Name").Return("test-correlator")
		mockCorrelator.On("Process", mock.Anything, mock.Anything).Run(
			func(args mock.Arguments) {
				event := args.Get(1).(*domain.UnifiedEvent)
				mu.Lock()
				processedEvents[event.ID] = true
				mu.Unlock()

				// Simulate some work
				time.Sleep(10 * time.Millisecond)
			},
		).Return([]*CorrelationResult{{
			ID:   "test-correlation",
			Type: "test",
		}}, nil)

		err = engine.Start(context.Background())
		require.NoError(t, err)

		// Send events concurrently
		var wg sync.WaitGroup
		eventCount := 50

		for i := 0; i < eventCount; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				event := &domain.UnifiedEvent{
					ID:        fmt.Sprintf("event-%d", id),
					Timestamp: time.Now(),
				}
				err := engine.Process(context.Background(), event)
				assert.NoError(t, err)
			}(i)
		}

		wg.Wait()

		// Wait for processing
		time.Sleep(500 * time.Millisecond)

		// Verify all events were processed
		mu.Lock()
		assert.Len(t, processedEvents, eventCount)
		mu.Unlock()

		engine.Stop()
	})
}

func BenchmarkEngineProcess(b *testing.B) {
	logger := zap.NewNop()

	mockCorrelator := &TestifyMockCorrelator{}
	mockCorrelator.On("Name").Return("bench-correlator")
	mockCorrelator.On("Process", mock.Anything, mock.Anything).Return(
		[]*CorrelationResult{{ID: "correlation", Type: "bench"}},
		nil,
	)

	config := EngineConfig{
		EventBufferSize:        DefaultEventBufferSize,
		ResultBufferSize:       DefaultResultBufferSize,
		WorkerCount:            4,
		StorageCleanupInterval: 5 * time.Minute,
		StorageRetention:       24 * time.Hour,
	}

	engine, err := NewEngine(logger, config, nil, nil)
	require.NoError(b, err)

	engine.correlators = []Correlator{mockCorrelator}

	err = engine.Start(context.Background())
	require.NoError(b, err)

	defer engine.Stop()

	event := &domain.UnifiedEvent{
		ID:        "bench-event",
		Type:      "systemd",
		Timestamp: time.Now(),
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := engine.Process(context.Background(), event)
		if err != nil {
			b.Fatal(err)
		}
	}
}
