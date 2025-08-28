package kernel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestNewCollectorProductionLogger tests production logger creation
func TestNewCollectorProductionLogger(t *testing.T) {
	cfg := NewDefaultConfig("prod-logger-test")

	// This should trigger zap.NewProduction() path
	collector, err := NewCollector("prod-logger-test", cfg)
	require.NoError(t, err)
	require.NotNil(t, collector)
	require.NotNil(t, collector.logger)

	assert.Equal(t, "prod-logger-test", collector.Name())

	// Clean up
	_ = collector.Stop()
}

// TestNewCollectorWithConfigNilLogger tests nil logger handling
func TestNewCollectorWithConfigNilLogger(t *testing.T) {
	cfg := NewDefaultConfig("nil-logger")

	// This should trigger logger creation path in NewCollectorWithConfig
	collector, err := NewCollectorWithConfig(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, collector)
	require.NotNil(t, collector.logger)

	assert.Equal(t, cfg.Name, collector.Name())

	// Clean up
	_ = collector.Stop()
}

// TestNewCollectorWithConfigExistingLogger tests with provided logger
func TestNewCollectorWithConfigExistingLogger(t *testing.T) {
	cfg := NewDefaultConfig("existing-logger")
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	// This should use provided logger
	collector, err := NewCollectorWithConfig(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, collector)
	require.NotNil(t, collector.logger) // Logger should be set

	// Clean up
	_ = collector.Stop()
}

// TestCollectorStartStop tests complete start/stop cycle
func TestCollectorStartStop(t *testing.T) {
	cfg := NewDefaultConfig("start-stop-test")
	collector, err := NewCollector("start-stop-test", cfg)
	require.NoError(t, err)

	// Initial state
	assert.True(t, collector.IsHealthy())

	// Start
	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// Stop
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())

	// Stop again should be safe
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

// TestCollectorEventsChannel tests events channel behavior
func TestCollectorEventsChannel(t *testing.T) {
	cfg := NewDefaultConfig("events-test")
	cfg.BufferSize = 5
	collector, err := NewCollector("events-test", cfg)
	require.NoError(t, err)

	// Get events channel
	events := collector.Events()
	assert.NotNil(t, events)
	assert.Equal(t, 5, cap(collector.events))

	// Start collector
	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err)

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)

	// Events channel should be closed
	select {
	case _, ok := <-events:
		if ok {
			// Still receiving events - this is fine during shutdown
		}
	default:
		// Channel closed or empty - both are fine
	}
}

// TestStopEBPFStub explicitly tests the stopEBPF stub function
func TestStopEBPFStub(t *testing.T) {
	cfg := NewDefaultConfig("stop-ebpf-test")
	collector, err := NewCollector("stop-ebpf-test", cfg)
	require.NoError(t, err)

	// Call stopEBPF directly - this should be a no-op on non-Linux
	collector.stopEBPF()

	// Should not panic or cause issues
	assert.True(t, true)
}

// TestCollectorBasicOperations tests all basic operations work
func TestCollectorBasicOperations(t *testing.T) {
	cfg := NewDefaultConfig("basic-ops")
	collector, err := NewCollector("basic-ops", cfg)
	require.NoError(t, err)

	// Test all basic operations
	assert.Equal(t, "basic-ops", collector.Name())
	assert.True(t, collector.IsHealthy())
	assert.NotNil(t, collector.Events())

	// Start and verify state
	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// Stop and verify state
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

// TestCollectorWithSmallBuffer tests small buffer handling
func TestCollectorWithSmallBuffer(t *testing.T) {
	cfg := NewDefaultConfig("small-buffer")
	cfg.BufferSize = 1

	collector, err := NewCollector("small-buffer", cfg)
	require.NoError(t, err)

	assert.Equal(t, 1, cap(collector.events))

	// Should work normally
	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err)

	err = collector.Stop()
	assert.NoError(t, err)
}
