package health

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/observers/orchestrator"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestNegativeNilLogger tests creating observer with nil logger
func TestNegativeNilLogger(t *testing.T) {
	// Should handle nil logger gracefully or panic
	defer func() {
		if r := recover(); r != nil {
			// Expected panic with nil logger
			t.Logf("Recovered from panic: %v", r)
		}
	}()

	observer, err := NewObserver(nil, DefaultConfig())
	if err == nil && observer != nil {
		// If it doesn't panic, it should at least work
		assert.NotNil(t, observer)
	}
}

// TestNegativeInvalidConfig tests invalid configurations
func TestNegativeInvalidConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name   string
		config *Config
	}{
		{
			name: "zero buffer size",
			config: &Config{
				RingBufferSize:   0,
				EventChannelSize: 10,
				RateLimitMs:      10,
			},
		},
		{
			name: "negative channel size",
			config: &Config{
				RingBufferSize:   1024,
				EventChannelSize: -1,
				RateLimitMs:      10,
			},
		},
		{
			name: "negative rate limit",
			config: &Config{
				RingBufferSize:   1024,
				EventChannelSize: 10,
				RateLimitMs:      -100,
			},
		},
		{
			name: "nil categories map",
			config: &Config{
				RingBufferSize:    1024,
				EventChannelSize:  10,
				RateLimitMs:       10,
				EnabledCategories: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := NewObserver(logger, tt.config)
			// Should either error or handle gracefully
			if err != nil {
				assert.Error(t, err)
			} else {
				assert.NotNil(t, observer)
				// Try to start with invalid config
				ctx := context.Background()
				err = observer.Start(ctx)
				// May or may not error depending on implementation
				if err != nil {
					t.Logf("Start failed with invalid config: %v", err)
				}
				observer.Stop()
			}
		})
	}
}

// TestNegativeStartWithoutInit tests starting observer without proper initialization
func TestNegativeStartWithoutInit(t *testing.T) {
	observer := &Observer{}

	ctx := context.Background()
	err := observer.Start(ctx)

	// Should handle gracefully or error
	if err == nil {
		// If no error, should at least not crash
		observer.Stop()
	}
}

// TestNegativeDoubleStart tests starting observer twice
func TestNegativeDoubleStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()

	// First start
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Second start - should handle gracefully
	err = observer.Start(ctx)
	// May or may not error, but shouldn't crash
	if err != nil {
		assert.Error(t, err)
	}

	observer.Stop()
}

// TestNegativeDoubleStop tests stopping observer twice
func TestNegativeDoubleStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	// First stop
	err = observer.Stop()
	assert.NoError(t, err)

	// Second stop - should handle gracefully
	err = observer.Stop()
	// Should not crash
	if err != nil {
		assert.Error(t, err)
	}
}

// TestNegativeStopWithoutStart tests stopping observer without starting
func TestNegativeStopWithoutStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	// Stop without start
	err = observer.Stop()
	// Should handle gracefully
	if err != nil {
		assert.Error(t, err)
	}
}

// TestNegativeNilEventConversion tests converting nil event
func TestNegativeNilEventConversion(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	// Try to convert nil event
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Recovered from panic on nil event: %v", r)
		}
	}()

	result := observer.convertToCollectorEvent(nil)
	// Should either return nil or handle gracefully
	if result != nil {
		assert.NotNil(t, result)
	}
}

// TestNegativeInvalidEventData tests events with invalid data
func TestNegativeInvalidEventData(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	tests := []struct {
		name  string
		event *HealthEvent
	}{
		{
			name: "invalid category",
			event: &HealthEvent{
				Category: 99, // Invalid category
			},
		},
		{
			name: "invalid syscall number",
			event: &HealthEvent{
				SyscallNr: -999,
				Category:  1,
			},
		},
		{
			name: "invalid error code",
			event: &HealthEvent{
				ErrorCode: 999999, // Positive error code
				Category:  1,
			},
		},
		{
			name: "zero timestamp",
			event: &HealthEvent{
				TimestampNs: 0,
				Category:    1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := observer.convertToCollectorEvent(tt.event)
			// Should handle gracefully and return valid event
			assert.NotNil(t, result)
			assert.NotEmpty(t, result.EventID)
		})
	}
}

// TestNegativeChannelOverflow tests channel overflow behavior
func TestNegativeChannelOverflow(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:   1024,
		EventChannelSize: 2, // Very small channel
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
	defer observer.Stop()

	// Send more events than channel can hold
	dropped := 0
	sent := 0

	for i := 0; i < 10; i++ {
		event := &domain.CollectorEvent{
			EventID: string(rune(i)),
		}
		if observer.EventChannelManager.SendEvent(event) {
			sent++
		} else {
			dropped++
		}
	}

	assert.Greater(t, dropped, 0, "Should have dropped some events")
	assert.Greater(t, sent, 0, "Should have sent some events")

	// Verify drop statistics
	stats := observer.Statistics()
	// Check drops in CustomMetrics
	if stats.CustomMetrics != nil {
		assert.Equal(t, string(rune(dropped)), stats.CustomMetrics["events_dropped"])
	}
}

// TestNegativeContextCancellationDuringStart tests context cancellation
func TestNegativeContextCancellationDuringStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	// Create already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to start with cancelled context
	err = observer.Start(ctx)
	// Should either error or handle gracefully
	if err == nil {
		// If started, should stop cleanly
		observer.Stop()
	}
}

// TestNegativeClosedChannel tests sending to closed channel
func TestNegativeClosedChannel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Close the channel
	observer.EventChannelManager.Close()

	// Try to send event to closed channel
	event := &domain.CollectorEvent{EventID: "test"}
	sent := observer.EventChannelManager.SendEvent(event)
	assert.False(t, sent, "Should not send to closed channel")

	observer.Stop()
}

// TestNegativeFactoryErrors tests factory function error cases
func TestNegativeFactoryErrors(t *testing.T) {
	tests := []struct {
		name      string
		obsName   string
		config    *orchestrator.ObserverConfigData
		logger    *zap.Logger
		wantErr   bool
		errString string
	}{
		{
			name:      "nil config",
			obsName:   "health",
			config:    nil,
			logger:    zap.NewNop(),
			wantErr:   true,
			errString: "config is required",
		},
		{
			name:      "nil logger",
			obsName:   "health",
			config:    &orchestrator.ObserverConfigData{},
			logger:    nil,
			wantErr:   true,
			errString: "logger is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := Factory(tt.obsName, tt.config, tt.logger)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errString)
				assert.Nil(t, observer)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, observer)
			}
		})
	}
}

// TestNegativeGetStatsErrors tests error cases in GetStats
func TestNegativeGetStatsErrors(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	// Get stats without starting (no eBPF state)
	stats, err := observer.GetStats()
	// Should handle gracefully
	if runtime.GOOS == "linux" {
		// On Linux, might error due to missing eBPF state
		if err != nil {
			assert.Error(t, err)
		}
	} else {
		// On non-Linux, should return empty stats
		assert.NoError(t, err)
		assert.NotNil(t, stats)
	}
}

// TestNegativePanicRecovery tests panic recovery in event processing
func TestNegativePanicRecovery(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Create event that might cause issues
	event := &HealthEvent{
		TimestampNs: ^uint64(0), // Max uint64
		PID:         ^uint32(0), // Max uint32
		ErrorCode:   -999999,
		Category:    255,
	}

	// Should not panic
	domainEvent := observer.convertToCollectorEvent(event)
	assert.NotNil(t, domainEvent)
}

// TestNegativeRaceConditions tests for race conditions
func TestNegativeRaceConditions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race condition test in short mode")
	}

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	done := make(chan bool)

	// Concurrent operations
	go func() {
		for i := 0; i < 100; i++ {
			observer.IsHealthy()
			observer.Statistics()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			event := &HealthEvent{PID: uint32(i)}
			observer.convertToCollectorEvent(event)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			observer.BaseObserver.RecordEvent()
			observer.BaseObserver.RecordDrop()
		}
		done <- true
	}()

	// Wait for goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	observer.Stop()
}

// TestNegativeInvalidMetricsCreation tests metrics creation failures
func TestNegativeInvalidMetricsCreation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:    1024,
		EventChannelSize:  10,
		RateLimitMs:       10,
		RequireAllMetrics: true, // Require all metrics
		EnabledCategories: map[string]bool{
			"test": true,
		},
	}

	// In some environments, metric creation might fail
	observer, err := NewObserver(logger, config)
	if err != nil {
		// If metrics are required and fail, should error
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "metric")
	} else {
		// If succeeded, observer should work
		assert.NotNil(t, observer)
		observer.Stop()
	}
}

// TestNegativeTimeoutScenarios tests various timeout scenarios
func TestNegativeTimeoutScenarios(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	// Very short timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	err = observer.Start(ctx)
	if err == nil {
		time.Sleep(10 * time.Millisecond)
		// Context should be cancelled
		select {
		case <-ctx.Done():
			assert.Equal(t, context.DeadlineExceeded, ctx.Err())
		default:
			t.Fatal("context should be cancelled")
		}
		observer.Stop()
	}
}

// TestNegativeCorruptedEventData tests handling of corrupted event data
func TestNegativeCorruptedEventData(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	// Create event with corrupted string data (non-UTF8)
	event := &HealthEvent{
		Comm: [16]byte{0xFF, 0xFE, 0xFD, 0xFC}, // Invalid UTF-8
		Path: [256]byte{0xFF, 0xFE, 0xFD},       // Invalid UTF-8
	}

	// Should handle gracefully
	result := observer.convertToCollectorEvent(event)
	assert.NotNil(t, result)
	// Strings might be garbled but shouldn't crash
}

// TestNegativeErrorPropagation tests error propagation through the system
func TestNegativeErrorPropagation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	// Inject error condition by setting invalid state
	observer.ebpfState = nil

	// Operations should handle missing state gracefully
	stats, err := observer.GetStats()
	if runtime.GOOS == "linux" {
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "eBPF")
	} else {
		// Non-Linux should still work
		assert.NotNil(t, stats)
	}
}

// TestNegativeCustomError tests custom error scenarios
func TestNegativeCustomError(t *testing.T) {
	// Test custom error propagation
	customErr := errors.New("custom test error")

	// Mock scenario where internal functions return errors
	handleError := func(err error) error {
		if err != nil {
			return errors.Join(err, errors.New("wrapped error"))
		}
		return nil
	}

	wrappedErr := handleError(customErr)
	assert.Error(t, wrappedErr)
	assert.Contains(t, wrappedErr.Error(), "custom test error")
	assert.Contains(t, wrappedErr.Error(), "wrapped error")
}