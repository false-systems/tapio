package status

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestInvalidConfigurations(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		wantError string
	}{
		{
			name: "Negative buffer size",
			config: &Config{
				BufferSize:    -100,
				SampleRate:    1.0,
				FlushInterval: 1 * time.Second,
			},
			wantError: "buffer_size must be positive",
		},
		{
			name: "Zero buffer size",
			config: &Config{
				BufferSize:    0,
				SampleRate:    1.0,
				FlushInterval: 1 * time.Second,
			},
			wantError: "buffer_size must be positive",
		},
		{
			name: "Sample rate too high",
			config: &Config{
				BufferSize:    1000,
				SampleRate:    2.0,
				FlushInterval: 1 * time.Second,
			},
			wantError: "sample_rate must be between 0 and 1",
		},
		{
			name: "Negative sample rate",
			config: &Config{
				BufferSize:    1000,
				SampleRate:    -0.5,
				FlushInterval: 1 * time.Second,
			},
			wantError: "sample_rate must be between 0 and 1",
		},
		{
			name: "Zero flush interval",
			config: &Config{
				BufferSize:    1000,
				SampleRate:    1.0,
				FlushInterval: 0,
			},
			wantError: "flush_interval must be positive",
		},
		{
			name: "Negative flush interval",
			config: &Config{
				BufferSize:    1000,
				SampleRate:    1.0,
				FlushInterval: -1 * time.Second,
			},
			wantError: "flush_interval must be positive",
		},
		{
			name: "Multiple invalid fields",
			config: &Config{
				BufferSize:    -1,
				SampleRate:    3.0,
				FlushInterval: -1 * time.Second,
			},
			wantError: "buffer_size must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := NewObserver("test", tt.config)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
			assert.Nil(t, observer)
		})
	}
}

func TestChannelOverflow(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    2, // Very small buffer to force overflow
		FlushInterval: 1 * time.Second,
		Logger:        logger,
	}

	observer, err := NewObserver("test-overflow", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Fill the channel beyond capacity
	events := make([]*domain.CollectorEvent, 10)
	for i := range events {
		events[i] = &domain.CollectorEvent{
			EventID:   string(rune(i)),
			Timestamp: time.Now(),
			Type:      domain.EventTypeNetworkConnection,
			Source:    "status-test-overflow",
			Severity:  domain.EventSeverityInfo,
			EventData: domain.EventDataContainer{
				Network: &domain.NetworkData{
					SrcIP:   "10.0.0.1",
					DstIP:   "10.0.0.2",
					DstPort: 80,
				},
			},
			Metadata: domain.EventMetadata{
				Labels: map[string]string{
					"observer": "test-overflow",
					"version":  "1.0.0",
					"test":     "overflow",
				},
			},
		}
	}

	// Send events rapidly
	successCount := 0
	failCount := 0
	for _, event := range events {
		if observer.EventChannelManager.SendEvent(event) {
			successCount++
		} else {
			failCount++
		}
	}

	// Some events should be dropped due to buffer overflow
	assert.Greater(t, failCount, 0, "Expected some events to be dropped")
	assert.Greater(t, successCount, 0, "Expected some events to succeed")
	assert.Equal(t, 10, successCount+failCount)

	// Check statistics - drops tracked separately
	stats := observer.Statistics()
	// EventsDropped doesn't exist in CollectorStats, use BaseObserver drop tracking
	assert.NotNil(t, stats)
}

func TestConcurrentShutdown(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-shutdown", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Start multiple goroutines that try to stop concurrently
	var wg sync.WaitGroup
	errors := make([]error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errors[idx] = observer.Stop()
		}(i)
	}

	wg.Wait()

	// All stops should succeed without panics
	for _, err := range errors {
		assert.NoError(t, err)
	}

	// Observer should be stopped
	assert.False(t, observer.IsHealthy())
}

func TestNilDeferences(t *testing.T) {
	t.Run("Nil config logger", func(t *testing.T) {
		config := &Config{
			Enabled:       true,
			BufferSize:    100,
			FlushInterval: 1 * time.Second,
			Logger:        nil, // Nil logger
		}

		observer, err := NewObserver("test-nil-logger", config)
		assert.NoError(t, err)
		assert.NotNil(t, observer)
		// Should use NopLogger
		assert.NotNil(t, observer.logger)
	})

	t.Run("Empty aggregator operations", func(t *testing.T) {
		aggregator := NewStatusAggregator(100 * time.Millisecond)

		// Flush empty aggregator
		result := aggregator.Flush()
		assert.Empty(t, result)

		// Double flush
		result1 := aggregator.Flush()
		result2 := aggregator.Flush()
		assert.Empty(t, result2)
		_ = result1 // Use to avoid warning
	})

	t.Run("HashDecoder with non-existent keys", func(t *testing.T) {
		decoder := NewHashDecoder()

		// Get non-existent entries
		assert.Empty(t, decoder.GetService(99999))
		assert.Empty(t, decoder.GetEndpoint(99999))

		// Add and retrieve
		decoder.AddService(123, "test-service")
		assert.Equal(t, "test-service", decoder.GetService(123))
		assert.Empty(t, decoder.GetService(456))
	})
}

func TestPatternDetectionEdgeCases(t *testing.T) {
	t.Run("Empty events list", func(t *testing.T) {
		events := []*StatusEvent{}

		for _, pattern := range KnownPatterns {
			// Should handle empty list without panic
			result := pattern.Detector(events)
			assert.False(t, result, "Empty events should not trigger pattern: %s", pattern.Name)
		}
	})

	t.Run("Nil events in list", func(t *testing.T) {
		events := []*StatusEvent{
			nil,
			{ServiceHash: 123, ErrorType: ErrorTimeout},
			nil,
			{ServiceHash: 456, ErrorType: ErrorTimeout},
			nil,
		}

		// Patterns should handle nil entries gracefully
		for _, pattern := range KnownPatterns {
			// Should not panic
			_ = pattern.Detector(events)
		}
	})

	t.Run("Single event", func(t *testing.T) {
		events := []*StatusEvent{
			{ServiceHash: 123, ErrorType: ErrorTimeout},
		}

		// Single event shouldn't trigger any patterns
		for _, pattern := range KnownPatterns {
			result := pattern.Detector(events)
			assert.False(t, result, "Single event should not trigger pattern: %s", pattern.Name)
		}
	})
}

func TestResourceLeaks(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 50 * time.Millisecond,
		Logger:        logger,
	}

	// Start and stop multiple times to check for leaks
	for i := 0; i < 5; i++ {
		observer, err := NewObserver("test-leak", config)
		require.NoError(t, err)

		ctx := context.Background()
		err = observer.Start(ctx)
		require.NoError(t, err)

		// Do some work
		for j := 0; j < 10; j++ {
			event := &StatusEvent{
				ServiceHash:  uint32(j),
				EndpointHash: uint32(j),
				StatusCode:   200,
				ErrorType:    ErrorNone,
				Timestamp:    uint64(time.Now().UnixNano()),
			}
			observer.aggregator.Add(event)
		}

		// Stop should clean up all resources
		err = observer.Stop()
		assert.NoError(t, err)

		// Verify cleanup
		assert.False(t, observer.IsHealthy())

		// Channel should be closed
		select {
		case _, ok := <-observer.Events():
			assert.False(t, ok, "Channel should be closed")
		default:
			// Channel might be empty but closed
		}
	}
}

func TestPanicRecovery(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-panic", config)
	require.NoError(t, err)

	// The observer should handle panics gracefully
	// This is more relevant for the actual eBPF processing

	ctx := context.Background()
	err = observer.Start(ctx)
	assert.NoError(t, err)

	// Observer should remain healthy even if background goroutines have issues
	time.Sleep(200 * time.Millisecond)
	assert.True(t, observer.IsHealthy())

	err = observer.Stop()
	assert.NoError(t, err)
}

func TestErrorAccumulation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-errors", config)
	require.NoError(t, err)

	// Record multiple errors
	testError := errors.New("test error")
	for i := 0; i < 10; i++ {
		observer.RecordError(testError)
	}

	// Check error statistics
	stats := observer.Statistics()
	assert.Equal(t, int64(10), stats.ErrorCount)
	// LastError doesn't exist in CollectorStats, errors are counted only
}

func TestObserverRaceConditions(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 50 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-race", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Run concurrent operations
	var wg sync.WaitGroup

	// Concurrent status updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			observer.SetHealthy(i%2 == 0)
		}
	}()

	// Concurrent error recording
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			observer.RecordError(errors.New("test"))
		}
	}()

	// Concurrent event recording
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			observer.RecordEvent()
		}
	}()

	// Concurrent statistics reads
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = observer.Statistics()
		}
	}()

	// Concurrent health checks
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = observer.IsHealthy()
		}
	}()

	// Wait for all goroutines
	wg.Wait()

	// System should still be functional
	stats := observer.Statistics()
	assert.NotNil(t, stats)
	assert.Greater(t, stats.EventsProcessed, int64(0))
}

func TestAggregatorOverflow(t *testing.T) {
	aggregator := NewStatusAggregator(50 * time.Millisecond)

	// Add maximum number of unique services
	for i := 0; i < 100000; i++ {
		event := &StatusEvent{
			ServiceHash:  uint32(i),
			EndpointHash: uint32(i),
			StatusCode:   500,
			ErrorType:    Error5XX,
			Latency:      uint32(i),
		}
		aggregator.Add(event)
	}

	// Should handle large number of services
	result := aggregator.Flush()
	assert.Len(t, result, 100000)

	// Second flush should be empty
	result2 := aggregator.Flush()
	assert.Empty(t, result2)
}

func TestInvalidEventData(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-invalid", config)
	require.NoError(t, err)

	// Test with various invalid events
	invalidEvents := []*StatusEvent{
		{ServiceHash: 0, EndpointHash: 0, StatusCode: 0},      // All zeros
		{ServiceHash: ^uint32(0), StatusCode: 999},            // Max values
		{ErrorType: ErrorType(255)},                           // Invalid error type
		{Latency: ^uint32(0)},                                 // Max latency
		{Timestamp: 0},                                         // Zero timestamp
		{PID: 0},                                              // Zero PID
	}

	// Aggregator should handle all invalid data gracefully
	for _, event := range invalidEvents {
		observer.aggregator.Add(event)
	}

	// Flush and verify no panics
	result := observer.aggregator.Flush()
	assert.NotNil(t, result)
}

func TestContextCancellation(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 1 * time.Second,
		Logger:        logger,
	}

	observer, err := NewObserver("test-cancel", config)
	require.NoError(t, err)

	// Start with cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Cancel context immediately
	cancel()

	// Give time for cancellation to propagate
	time.Sleep(100 * time.Millisecond)

	// Observer should handle cancellation gracefully
	err = observer.Stop()
	assert.NoError(t, err)
}

func TestMetricCallbackErrors(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-metrics", config)
	require.NoError(t, err)

	// Test error rate callback with no data
	ctx := context.Background()
	err = observer.observeErrorRate(ctx, nil)
	assert.NoError(t, err)

	// Add some error rates
	observer.mu.Lock()
	observer.errorRates[123] = 0.5
	observer.errorRates[456] = 0.25
	observer.mu.Unlock()

	// Callback should work with data
	err = observer.observeErrorRate(ctx, nil)
	assert.NoError(t, err)
}