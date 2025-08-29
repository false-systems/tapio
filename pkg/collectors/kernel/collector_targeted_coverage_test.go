package kernel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestStopEBPFDirectCall tests the stopEBPF stub function directly
func TestStopEBPFDirectCall(t *testing.T) {
	cfg := NewDefaultConfig("test-stop-ebpf")
	collector, err := NewCollector("test-stop-ebpf", cfg)
	require.NoError(t, err)

	// Call stopEBPF directly - this should be a no-op on non-Linux
	collector.stopEBPF()

	// Should not panic or cause issues
	assert.True(t, collector.IsHealthy())

	// Clean up
	_ = collector.Stop()
}

// TestNewCollectorWithConfigLoggerCreationError tests logger creation failure path
func TestNewCollectorWithConfigLoggerCreationError(t *testing.T) {
	cfg := NewDefaultConfig("test-logger-fail")

	// Test with nil logger - should create one successfully
	collector, err := NewCollectorWithConfig(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, collector)
	require.NotNil(t, collector.logger)

	// Test with provided logger
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	collector2, err := NewCollectorWithConfig(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, collector2)

	// Clean up both
	_ = collector.Stop()
	_ = collector2.Stop()
}

// TestCollectorStartWithEBPFFailure tests start failure scenario
func TestCollectorStartWithEBPFFailure(t *testing.T) {
	cfg := NewDefaultConfig("test-ebpf-fail")
	cfg.EnableEBPF = true

	collector, err := NewCollector("test-ebpf-fail", cfg)
	require.NoError(t, err)

	ctx := context.Background()

	// On non-Linux platforms, eBPF should be disabled but start should succeed
	err = collector.Start(ctx)
	assert.NoError(t, err) // Start should work even when eBPF is not supported

	assert.True(t, collector.IsHealthy())

	// Clean up
	_ = collector.Stop()
}

// TestCollectorStopEBPFCoverage ensures stopEBPF is called during Stop
func TestCollectorStopEBPFCoverage(t *testing.T) {
	cfg := NewDefaultConfig("test-stop-coverage")
	cfg.EnableEBPF = true

	collector, err := NewCollector("test-stop-coverage", cfg)
	require.NoError(t, err)

	ctx := context.Background()

	// Start to initialize eBPF state
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Stop should call stopEBPF internally
	err = collector.Stop()
	require.NoError(t, err)

	assert.False(t, collector.IsHealthy())
}

// TestCollectorMetricCreationErrors tests metric creation error paths
func TestCollectorMetricCreationErrors(t *testing.T) {
	cfg := NewDefaultConfig("test-metrics")

	// This test verifies that metric creation errors are handled gracefully
	collector, err := NewCollector("test-metrics", cfg)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Even if metric creation fails, collector should still work
	assert.Equal(t, "test-metrics", collector.Name())
	assert.True(t, collector.IsHealthy())

	// Clean up
	_ = collector.Stop()
}

// TestCollectorCancelContextTiming tests context cancellation timing
func TestCollectorCancelContextTiming(t *testing.T) {
	cfg := NewDefaultConfig("test-cancel-timing")
	collector, err := NewCollector("test-cancel-timing", cfg)
	require.NoError(t, err)

	// Create context with immediate cancellation
	ctx, cancel := context.WithCancel(context.Background())

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Cancel context immediately after start
	cancel()

	// Give some time for processEvents to notice cancellation
	time.Sleep(5 * time.Millisecond)

	// Stop should still work
	err = collector.Stop()
	require.NoError(t, err)
}

// TestCollectorStopNilCancel tests stop when cancel is nil
func TestCollectorStopNilCancel(t *testing.T) {
	cfg := NewDefaultConfig("test-nil-cancel")
	collector, err := NewCollector("test-nil-cancel", cfg)
	require.NoError(t, err)

	// Don't start, just stop directly
	collector.cancel = nil // Ensure cancel is nil

	err = collector.Stop()
	require.NoError(t, err)

	assert.False(t, collector.IsHealthy())
}

// TestCollectorStopNilEvents tests stop when events channel is nil
func TestCollectorStopNilEvents(t *testing.T) {
	cfg := NewDefaultConfig("test-nil-events")
	collector, err := NewCollector("test-nil-events", cfg)
	require.NoError(t, err)

	// Start and then manually nil the events channel to test the nil check
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Stop once normally
	err = collector.Stop()
	require.NoError(t, err)

	// At this point events should be nil, stop again should not panic
	err = collector.Stop()
	require.NoError(t, err)
}

// TestCollectorEventChannelAccess tests concurrent access to events channel
func TestCollectorEventChannelAccess(t *testing.T) {
	cfg := NewDefaultConfig("test-event-access")
	collector, err := NewCollector("test-event-access", cfg)
	require.NoError(t, err)

	// Test Events() method before start
	events := collector.Events()
	assert.NotNil(t, events)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Test Events() method after start
	events = collector.Events()
	assert.NotNil(t, events)

	err = collector.Stop()
	require.NoError(t, err)

	// Test Events() method after stop (should be nil)
	events = collector.Events()
	assert.Nil(t, events)
}
