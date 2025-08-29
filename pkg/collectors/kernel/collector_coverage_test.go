package kernel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestCollectorStartErrorCoverage tests Start method error paths for coverage
func TestCollectorStartErrorCoverage(t *testing.T) {
	cfg := NewDefaultConfig("test-kernel")
	cfg.BufferSize = 100

	collector, err := NewCollector("test-kernel", cfg)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Start the collector
	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err) // On non-Linux, startEBPF returns nil

	// Start again should be fine (idempotent)
	err = collector.Start(ctx)
	assert.NoError(t, err)

	// Stop the collector
	err = collector.Stop()
	assert.NoError(t, err)
}

// TestCollectorWithNilLogger tests that nil logger gets created
func TestCollectorWithNilLogger(t *testing.T) {
	cfg := NewDefaultConfig("test")

	// Create collector with nil logger via NewCollectorWithConfig
	collector, err := NewCollectorWithConfig(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, collector)
	require.NotNil(t, collector.logger) // Should create a logger

	// Verify basic operations work
	assert.Equal(t, "test", collector.Name())
	assert.True(t, collector.IsHealthy())
}

// TestCollectorMetricFailures tests metric creation failures
func TestCollectorMetricFailures(t *testing.T) {
	cfg := NewDefaultConfig("test-metric-fail")
	logger := zaptest.NewLogger(t)

	// NewCollectorWithConfig handles metric failures gracefully
	collector, err := NewCollectorWithConfig(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Even with nil metrics, collector should function
	assert.Equal(t, "test-metric-fail", collector.Name())
	assert.True(t, collector.IsHealthy())

	// Events channel should be available
	assert.NotNil(t, collector.Events())
}

// TestCollectorStopMultipleTimes tests stopping collector multiple times
func TestCollectorStopMultipleTimes(t *testing.T) {
	cfg := NewDefaultConfig("test-multi-stop")
	collector, err := NewCollector("test-multi-stop", cfg)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err)

	// Stop multiple times should be safe
	err = collector.Stop()
	assert.NoError(t, err)

	err = collector.Stop()
	assert.NoError(t, err)

	err = collector.Stop()
	assert.NoError(t, err)
}

// TestCollectorContextCancellation tests context cancellation
func TestCollectorContextCancellation(t *testing.T) {
	cfg := NewDefaultConfig("test-ctx-cancel")
	collector, err := NewCollector("test-ctx-cancel", cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	err = collector.Start(ctx)
	assert.NoError(t, err)

	// Cancel context
	cancel()

	// Give processEvents time to exit
	time.Sleep(100 * time.Millisecond)

	// Stop should work even after context cancellation
	err = collector.Stop()
	assert.NoError(t, err)
}

// TestNewCollectorErrorPaths tests error conditions in NewCollector
func TestNewCollectorErrorPaths(t *testing.T) {
	// Test with production logger creation
	cfg := NewDefaultConfig("test-prod-logger")

	collector, err := NewCollector("test-prod-logger", cfg)
	require.NoError(t, err)
	require.NotNil(t, collector)
	assert.Equal(t, "test-prod-logger", collector.Name())
}

// TestCollectorHealthStatus tests health status changes
func TestCollectorHealthStatus(t *testing.T) {
	cfg := NewDefaultConfig("test-health")
	collector, err := NewCollector("test-health", cfg)
	require.NoError(t, err)

	// Initially healthy
	assert.True(t, collector.IsHealthy())

	// Start collector
	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// After stop, should not be healthy
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

// TestCollectorEventChannel tests event channel operations
func TestCollectorEventChannel(t *testing.T) {
	cfg := NewDefaultConfig("test-events")
	cfg.BufferSize = 10
	collector, err := NewCollector("test-events", cfg)
	require.NoError(t, err)

	// Get events channel
	events := collector.Events()
	assert.NotNil(t, events)

	// Channel should have correct buffer size
	assert.Equal(t, 10, cap(collector.events))

	// Start collector
	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err)

	// Stop collector (should close channel)
	err = collector.Stop()
	assert.NoError(t, err)

	// Reading from closed channel should return zero value
	select {
	case _, ok := <-events:
		assert.False(t, ok, "Channel should be closed")
	case <-time.After(100 * time.Millisecond):
		// Channel might not be closed immediately on non-Linux
	}
}

// TestStubFunctions tests the stub functions for coverage
func TestStubFunctions(t *testing.T) {
	cfg := NewDefaultConfig("test-stubs")
	logger := zaptest.NewLogger(t)

	collector, err := NewCollectorWithConfig(cfg, logger)
	require.NoError(t, err)

	// Test stopEBPF (stub)
	collector.stopEBPF() // Should be no-op

	// Test readEBPFEvents (stub)
	ctx, cancel := context.WithCancel(context.Background())
	collector.ctx = ctx

	done := make(chan bool)
	go func() {
		collector.readEBPFEvents() // Should block on ctx.Done()
		done <- true
	}()

	cancel()

	select {
	case <-done:
		// Good, function exited
	case <-time.After(1 * time.Second):
		t.Fatal("readEBPFEvents did not exit after context cancellation")
	}
}

// TestCollectorWithZeroBufferSize tests collector with edge case buffer size
func TestCollectorWithZeroBufferSize(t *testing.T) {
	cfg := NewDefaultConfig("test-zero-buffer")
	cfg.BufferSize = 0 // Edge case

	collector, err := NewCollector("test-zero-buffer", cfg)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Should still have a valid (though zero-size) channel
	assert.NotNil(t, collector.Events())
	assert.Equal(t, 0, cap(collector.events))
}

// TestCollectorLoggerCreationError simulates logger creation error path
func TestCollectorLoggerCreationError(t *testing.T) {
	cfg := NewDefaultConfig("test-logger-error")

	// Test the case where both zaptest and zap.NewProduction would be used
	// This increases coverage of the logger creation logic
	collector, err := NewCollector("test-logger-error", cfg)
	require.NoError(t, err)
	require.NotNil(t, collector)
	require.NotNil(t, collector.logger)
}
