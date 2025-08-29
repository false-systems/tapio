//go:build linux

package systemd

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name          string
		collectorName string
		config        Config
		logger        *zap.Logger
		expectError   bool
	}{
		{
			name:          "valid collector with provided logger",
			collectorName: "test-systemd",
			config:        Config{BufferSize: 100, EnableEBPF: false},
			logger:        zaptest.NewLogger(t),
			expectError:   false,
		},
		{
			name:          "valid collector with nil logger",
			collectorName: "test-systemd-nil",
			config:        Config{BufferSize: 100, EnableEBPF: false},
			logger:        nil,
			expectError:   false,
		},
		{
			name:          "collector with eBPF enabled",
			collectorName: "test-systemd-ebpf",
			config:        Config{BufferSize: 100, EnableEBPF: true},
			logger:        zaptest.NewLogger(t),
			expectError:   false,
		},
		{
			name:          "collector with large buffer",
			collectorName: "test-systemd-large",
			config:        Config{BufferSize: 10000, EnableEBPF: false},
			logger:        zaptest.NewLogger(t),
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.collectorName, tt.config, tt.logger)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
				assert.Equal(t, tt.collectorName, collector.Name())
				assert.NotNil(t, collector.logger)
				assert.NotNil(t, collector.Events())
				assert.False(t, collector.IsHealthy()) // Should not be healthy until started
			}
		})
	}
}

func TestCollectorLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{BufferSize: 10, EnableEBPF: false}

	collector, err := NewCollector("test-lifecycle", config, logger)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Initial state
	assert.False(t, collector.IsHealthy())
	assert.Equal(t, "test-lifecycle", collector.Name())

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())

	// Verify events channel is closed
	select {
	case _, ok := <-collector.Events():
		assert.False(t, ok, "Events channel should be closed after Stop()")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Events channel should be closed immediately after Stop()")
	}
}

func TestCollectorStartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{BufferSize: 10, EnableEBPF: false}

	collector, err := NewCollector("test-startstop", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Test multiple starts (should not error)
	err1 := collector.Start(ctx)
	assert.NoError(t, err1)
	assert.True(t, collector.IsHealthy())

	err2 := collector.Start(ctx)
	assert.NoError(t, err2)
	assert.True(t, collector.IsHealthy())

	// Test stop
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())

	// Test multiple stops (should not error)
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

func TestCollectorStartWithCancellation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{BufferSize: 10, EnableEBPF: false}

	collector, err := NewCollector("test-cancel", config, logger)
	require.NoError(t, err)

	// Create a context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())

	err = collector.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// Cancel context
	cancel()

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

func TestCollectorEventsChannel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{BufferSize: 5, EnableEBPF: false}

	collector, err := NewCollector("test-events", config, logger)
	require.NoError(t, err)

	// Events channel should be available before starting
	events := collector.Events()
	assert.NotNil(t, events)

	// Channel should have correct buffer size
	assert.Equal(t, 5, cap(events))

	// Start collector
	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err)

	// Events channel should still be the same
	assert.Equal(t, events, collector.Events())

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)

	// Channel should be closed after stop
	select {
	case _, ok := <-events:
		assert.False(t, ok, "Events channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Events channel should be closed immediately")
	}
}

func TestCollectorName(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{BufferSize: 10, EnableEBPF: false}

	testNames := []string{
		"simple",
		"systemd-collector",
		"test-with-numbers-123",
		"",
	}

	for _, name := range testNames {
		t.Run("name_"+name, func(t *testing.T) {
			collector, err := NewCollector(name, config, logger)
			require.NoError(t, err)
			assert.Equal(t, name, collector.Name())
		})
	}
}

func TestCollectorHealthStatus(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{BufferSize: 10, EnableEBPF: false}

	collector, err := NewCollector("test-health", config, logger)
	require.NoError(t, err)

	// Should not be healthy initially
	assert.False(t, collector.IsHealthy())

	// Should be healthy after starting
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// Should not be healthy after stopping
	err = collector.Stop()
	require.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

// Integration tests for service lifecycle monitoring
func TestSystemdEventProcessing(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{BufferSize: 100, EnableEBPF: false}

	collector, err := NewCollector("test-events", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Test that events channel is properly configured
	events := collector.Events()
	assert.NotNil(t, events)
	assert.Equal(t, 100, cap(events))
}

func TestCollectorConfigValidation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name   string
		config Config
		valid  bool
	}{
		{
			name:   "valid config with eBPF disabled",
			config: Config{BufferSize: 100, EnableEBPF: false},
			valid:  true,
		},
		{
			name:   "valid config with eBPF enabled",
			config: Config{BufferSize: 50, EnableEBPF: true},
			valid:  true,
		},
		{
			name:   "zero buffer size",
			config: Config{BufferSize: 0, EnableEBPF: false},
			valid:  true, // Should work, just no buffering
		},
		{
			name:   "large buffer size",
			config: Config{BufferSize: 10000, EnableEBPF: false},
			valid:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector("test-config", tt.config, logger)
			if tt.valid {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestCollectorConcurrentOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{BufferSize: 100, EnableEBPF: false}

	collector, err := NewCollector("test-concurrent", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Test concurrent health checks and state changes
	done := make(chan bool, 10)

	// Mix of operations to test race conditions
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				collector.IsHealthy()
				collector.Name()
				collector.Events()
			}
			done <- true
		}()
	}

	// Concurrent health status writes (via Start/Stop)
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				collector.Start(ctx) // Should be safe to call multiple times
				time.Sleep(time.Microsecond)
				collector.IsHealthy()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Stop collector
	err = collector.Stop()
	require.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

func TestCollectorRaceCondition(t *testing.T) {
	// This test specifically checks for race conditions
	// Run with: go test -race
	logger := zaptest.NewLogger(t)
	config := Config{BufferSize: 100, EnableEBPF: false}

	collector, err := NewCollector("test-race", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent Start calls
	wg.Add(3)
	for i := 0; i < 3; i++ {
		go func() {
			defer wg.Done()
			collector.Start(ctx)
		}()
	}
	wg.Wait()

	// Concurrent IsHealthy calls while stopping
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			collector.IsHealthy()
		}
	}()
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		collector.Stop()
	}()
	wg.Wait()

	assert.False(t, collector.IsHealthy())
}

func TestCollectorMetricsInitialization(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{BufferSize: 10, EnableEBPF: false}

	collector, err := NewCollector("test-metrics", config, logger)
	require.NoError(t, err)

	// Check that metrics are initialized (non-nil)
	assert.NotNil(t, collector.tracer)
	// We can't directly access private fields, but constructor should succeed
	// if metrics initialization works
}

func TestCollectorErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{BufferSize: 10, EnableEBPF: false}

	collector, err := NewCollector("test-errors", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Test starting collector multiple times
	err1 := collector.Start(ctx)
	assert.NoError(t, err1)

	err2 := collector.Start(ctx)
	assert.NoError(t, err2) // Should not error on multiple starts

	// Test context cancellation
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err3 := collector.Start(cancelCtx)
	assert.NoError(t, err3) // Start should still work with cancelled context

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid config with eBPF only",
			config:      Config{BufferSize: 100, EnableEBPF: true, EnableJournal: false},
			expectError: false,
		},
		{
			name:        "valid config with journal only",
			config:      Config{BufferSize: 100, EnableEBPF: false, EnableJournal: true},
			expectError: false,
		},
		{
			name:        "valid config with both enabled",
			config:      Config{BufferSize: 100, EnableEBPF: true, EnableJournal: true},
			expectError: false,
		},
		{
			name:        "invalid - zero buffer size",
			config:      Config{BufferSize: 0, EnableEBPF: true, EnableJournal: false},
			expectError: true,
			errorMsg:    "buffer size must be greater than 0",
		},
		{
			name:        "invalid - negative buffer size",
			config:      Config{BufferSize: -1, EnableEBPF: true, EnableJournal: false},
			expectError: true,
			errorMsg:    "buffer size must be greater than 0",
		},
		{
			name:        "invalid - too large buffer size",
			config:      Config{BufferSize: 2000000, EnableEBPF: true, EnableJournal: false},
			expectError: true,
			errorMsg:    "buffer size must not exceed 1,000,000",
		},
		{
			name:        "invalid - no monitoring enabled",
			config:      Config{BufferSize: 100, EnableEBPF: false, EnableJournal: false},
			expectError: true,
			errorMsg:    "at least one of EnableEBPF or EnableJournal must be true",
		},
		{
			name:        "valid config with service patterns",
			config:      Config{BufferSize: 100, EnableEBPF: true, ServicePatterns: []string{"nginx", "apache"}},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, 10000, config.BufferSize)
	assert.True(t, config.EnableEBPF)
	assert.True(t, config.EnableJournal)
	assert.Empty(t, config.ServicePatterns)

	// Default config should be valid
	err := config.Validate()
	assert.NoError(t, err)
}
