package processsignals

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

func TestNewObserver(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name:        "default config",
			config:      nil,
			expectError: false,
		},
		{
			name:        "valid config",
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name: "custom config",
			config: &Config{
				BufferSize:       5000,
				EnableEBPF:       false,
				EnableRingBuffer: false,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := NewObserver("test-runtime", tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, observer)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, observer)
				assert.Equal(t, "test-runtime", observer.Name())
				assert.True(t, observer.IsHealthy())

				// Test OTEL instrumentation is initialized
				assert.NotNil(t, observer.tracer)

				// Test base components are initialized
				assert.NotNil(t, observer.BaseObserver)
				assert.NotNil(t, observer.EventChannelManager)
				assert.NotNil(t, observer.LifecycleManager)

				// Test signal tracker is initialized
				assert.NotNil(t, observer.signalTracker)
			}
		})
	}
}

func TestObserverLifecycle(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := &Config{
		BufferSize: 1000,
		EnableEBPF: false, // Disable eBPF for testing
		Logger:     logger,
	}

	observer, err := NewObserver("test-runtime", config)
	require.NoError(t, err)
	require.NotNil(t, observer)

	// Test Start
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, observer.IsHealthy())

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)

	// Check events channel is available
	events := observer.Events()
	assert.NotNil(t, events)

	// Test Stop
	err = observer.Stop()
	assert.NoError(t, err)

	// After stop, should not be healthy
	assert.False(t, observer.IsHealthy())
}

func TestObserverStatistics(t *testing.T) {
	observer, err := NewObserver("test-runtime", nil)
	require.NoError(t, err)

	stats := observer.Statistics()
	assert.NotNil(t, stats)

	// Check statistics values
	assert.Equal(t, int64(0), stats.EventsProcessed)
	assert.Equal(t, int64(0), stats.ErrorCount)
}

func TestObserverHealth(t *testing.T) {
	observer, err := NewObserver("test-runtime", nil)
	require.NoError(t, err)

	health := observer.Health()
	assert.NotNil(t, health)
	assert.Equal(t, domain.HealthHealthy, health.Status)
	assert.Equal(t, "test-runtime", health.Component)

	// Record an error and check health
	observer.BaseObserver.RecordError(assert.AnError)
	health = observer.Health()
	assert.Equal(t, int64(1), health.ErrorCount)
}

func TestSignalDecoding(t *testing.T) {
	tests := []struct {
		signal      int
		name        string
		description string
		isFatal     bool
	}{
		{
			signal:      SIGTERM,
			name:        "SIGTERM",
			description: "Termination request",
			isFatal:     true,
		},
		{
			signal:      SIGKILL,
			name:        "SIGKILL",
			description: "Kill (cannot be caught or ignored)",
			isFatal:     true,
		},
		{
			signal:      SIGSEGV,
			name:        "SIGSEGV",
			description: "Segmentation fault (invalid memory reference)",
			isFatal:     true,
		},
		{
			signal:      999,
			name:        "UNKNOWN",
			description: "Unknown signal",
			isFatal:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.name, GetSignalName(tt.signal))
			assert.Equal(t, tt.description, GetSignalDescription(tt.signal))
			assert.Equal(t, tt.isFatal, IsSignalFatal(tt.signal))
		})
	}
}

func TestExitCodeDecoding(t *testing.T) {
	tests := []struct {
		exitCode    uint32
		description string
		signal      int
		coreDumped  bool
	}{
		{
			exitCode:    0,
			description: "Successful exit",
			signal:      0,
			coreDumped:  false,
		},
		{
			exitCode:    1,
			description: "Exited with code 1",
			signal:      0,
			coreDumped:  false,
		},
		{
			exitCode:    137, // 128 + 9 (SIGKILL)
			description: "Terminated by SIGKILL",
			signal:      9,
			coreDumped:  false,
		},
		{
			exitCode:    143, // 128 + 15 (SIGTERM)
			description: "Terminated by SIGTERM",
			signal:      15,
			coreDumped:  false,
		},
		{
			exitCode:    139, // 128 + 11 (SIGSEGV) with core dump
			description: "Terminated by SIGSEGV (core dumped)",
			signal:      11,
			coreDumped:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			info := DecodeExitCode(tt.exitCode)
			assert.NotNil(t, info)

			if tt.signal > 0 {
				assert.Equal(t, tt.signal, info.Signal)
				assert.Contains(t, info.Description, GetSignalName(tt.signal))
			} else {
				assert.Equal(t, int(tt.exitCode), info.Code)
			}

			if tt.coreDumped {
				assert.True(t, info.CoreDumped)
				assert.Contains(t, info.Description, "core dumped")
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	assert.NotNil(t, config)
	assert.Equal(t, 10000, config.BufferSize)
	assert.True(t, config.EnableEBPF)
	assert.True(t, config.EnableRingBuffer)
	assert.Equal(t, 8192, config.RingBufferSize)
	assert.Equal(t, 32, config.BatchSize)
	assert.Equal(t, 10*time.Millisecond, config.BatchTimeout)
	assert.True(t, config.EnableFilters)
}
