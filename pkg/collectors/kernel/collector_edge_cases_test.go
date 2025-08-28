package kernel

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// TestNewCollectorEdgeCases tests edge cases in NewCollector
func TestNewCollectorEdgeCases(t *testing.T) {
	// Test with various names
	testCases := []string{
		"",         // Empty name
		"test",     // Simple name
		"test-123", // Name with dash
		"test_123", // Name with underscore
		"TEST",     // Uppercase
		"123",      // Numbers
		"very-long-collector-name-that-exceeds-normal-length", // Long name
	}

	for _, name := range testCases {
		t.Run(name, func(t *testing.T) {
			cfg := NewDefaultConfig(name)
			collector, err := NewCollector(name, cfg)
			require.NoError(t, err)
			require.NotNil(t, collector)

			// Verify name is set correctly
			assert.Equal(t, name, collector.Name())

			// Clean up
			_ = collector.Stop()
		})
	}
}

// TestNewCollectorWithConfigEdgeCases tests edge cases in NewCollectorWithConfig
func TestNewCollectorWithConfigEdgeCases(t *testing.T) {
	t.Run("with observer logger", func(t *testing.T) {
		// Create observer logger to capture logs
		core, obs := observer.New(zapcore.InfoLevel)
		logger := zap.New(core)

		cfg := NewDefaultConfig("test-observer")
		collector, err := NewCollectorWithConfig(cfg, logger)
		require.NoError(t, err)
		require.NotNil(t, collector)

		// Verify logs were captured
		assert.Greater(t, obs.Len(), 0)

		// Clean up
		_ = collector.Stop()
	})

	t.Run("with disabled features", func(t *testing.T) {
		cfg := NewDefaultConfig("test-disabled")
		cfg.EnableEBPF = false
		cfg.BufferSize = 1

		collector, err := NewCollectorWithConfig(cfg, nil)
		require.NoError(t, err)
		require.NotNil(t, collector)

		// Start with eBPF disabled
		ctx := context.Background()
		err = collector.Start(ctx)
		assert.NoError(t, err)

		// Should still be healthy
		assert.True(t, collector.IsHealthy())

		// Clean up
		_ = collector.Stop()
	})
}

// TestCollectorStartEdgeCases tests edge cases in Start method
func TestCollectorStartEdgeCases(t *testing.T) {
	t.Run("start with cancelled context", func(t *testing.T) {
		cfg := NewDefaultConfig("test-cancelled")
		collector, err := NewCollector("test-cancelled", cfg)
		require.NoError(t, err)

		// Create already cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Start should handle cancelled context gracefully
		err = collector.Start(ctx)
		assert.NoError(t, err) // Should still start

		// Give processEvents time to notice context is cancelled
		time.Sleep(10 * time.Millisecond)

		// Clean up
		_ = collector.Stop()
	})

	t.Run("start with deadline context", func(t *testing.T) {
		cfg := NewDefaultConfig("test-deadline")
		collector, err := NewCollector("test-deadline", cfg)
		require.NoError(t, err)

		// Create context with very short deadline
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		// Start should handle deadline
		err = collector.Start(ctx)
		assert.NoError(t, err)

		// Wait for deadline
		time.Sleep(5 * time.Millisecond)

		// Clean up
		_ = collector.Stop()
	})

	t.Run("multiple concurrent starts", func(t *testing.T) {
		cfg := NewDefaultConfig("test-concurrent")
		collector, err := NewCollector("test-concurrent", cfg)
		require.NoError(t, err)

		ctx := context.Background()

		// Start multiple times concurrently
		done := make(chan bool, 3)
		for i := 0; i < 3; i++ {
			go func() {
				_ = collector.Start(ctx)
				done <- true
			}()
		}

		// Wait for all to complete
		for i := 0; i < 3; i++ {
			<-done
		}

		// Should still be healthy
		assert.True(t, collector.IsHealthy())

		// Clean up
		_ = collector.Stop()
	})
}

// TestCollectorStopEdgeCases tests edge cases in Stop method
func TestCollectorStopEdgeCases(t *testing.T) {
	t.Run("stop unstarted collector", func(t *testing.T) {
		cfg := NewDefaultConfig("test-unstarted")
		collector, err := NewCollector("test-unstarted", cfg)
		require.NoError(t, err)

		// Stop without start
		err = collector.Stop()
		assert.NoError(t, err)

		// Should not be healthy after stop
		assert.False(t, collector.IsHealthy())

		// Multiple stops should be safe
		err = collector.Stop()
		assert.NoError(t, err)
		err = collector.Stop()
		assert.NoError(t, err)
	})

	t.Run("concurrent stops", func(t *testing.T) {
		cfg := NewDefaultConfig("test-concurrent-stop")
		collector, err := NewCollector("test-concurrent-stop", cfg)
		require.NoError(t, err)

		// Start first
		ctx := context.Background()
		err = collector.Start(ctx)
		assert.NoError(t, err)

		// Stop multiple times concurrently
		done := make(chan bool, 3)
		for i := 0; i < 3; i++ {
			go func() {
				_ = collector.Stop()
				done <- true
			}()
		}

		// Wait for all to complete
		for i := 0; i < 3; i++ {
			<-done
		}

		// Should not be healthy after stop
		assert.False(t, collector.IsHealthy())
	})
}

// TestCollectorProcessEventsEdgeCases tests edge cases in processEvents
func TestCollectorProcessEventsEdgeCases(t *testing.T) {
	cfg := NewDefaultConfig("test-process-edge")
	cfg.BufferSize = 1 // Very small buffer

	collector, err := NewCollector("test-process-edge", cfg)
	require.NoError(t, err)

	// Create context that will be cancelled quickly
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	collector.ctx = ctx
	collector.cancel = cancel

	// Run processEvents
	done := make(chan bool)
	go func() {
		collector.processEvents()
		done <- true
	}()

	// Should exit after context timeout
	select {
	case <-done:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("processEvents did not exit after context timeout")
	}
}

// TestReadEBPFEvents tests the readEBPFEvents stub
func TestReadEBPFEvents(t *testing.T) {
	cfg := NewDefaultConfig("test-read-ebpf")
	collector, err := NewCollector("test-read-ebpf", cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	collector.ctx = ctx

	done := make(chan bool)
	go func() {
		collector.readEBPFEvents()
		done <- true
	}()

	cancel()

	select {
	case <-done:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("readEBPFEvents did not exit")
	}
}

// TestCollectorBufferEdgeCases tests various buffer sizes
func TestCollectorBufferEdgeCases(t *testing.T) {
	bufferSizes := []int{0, 1, 10, 100, 1000, 10000, 100000}

	for _, size := range bufferSizes {
		t.Run(fmt.Sprintf("buffer_%d", size), func(t *testing.T) {
			cfg := NewDefaultConfig("test-buffer")
			cfg.BufferSize = size

			collector, err := NewCollector("test-buffer", cfg)
			require.NoError(t, err)
			require.NotNil(t, collector)

			// Verify buffer was created with correct size
			assert.Equal(t, size, cap(collector.events))

			// Should be able to start and stop
			ctx := context.Background()
			err = collector.Start(ctx)
			assert.NoError(t, err)

			err = collector.Stop()
			assert.NoError(t, err)
		})
	}
}

// TestCollectorConfigValidationEdgeCases tests configuration validation edge cases
func TestCollectorConfigValidationEdgeCases(t *testing.T) {
	t.Run("zero buffer size", func(t *testing.T) {
		cfg := NewDefaultConfig("test-zero")
		cfg.BufferSize = 0

		// Should handle zero buffer size gracefully
		collector, err := NewCollector("test-zero", cfg)
		require.NoError(t, err)
		require.NotNil(t, collector)

		// Buffer should be created with zero size
		assert.Equal(t, 0, cap(collector.events))
	})
}
