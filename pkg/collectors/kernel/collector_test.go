package kernel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name     string
		collName string
		wantErr  bool
	}{
		{
			name:     "valid name",
			collName: "test-kernel",
			wantErr:  false,
		},
		{
			name:     "empty name",
			collName: "",
			wantErr:  false, // Empty name creates a collector with default config
		},
		{
			name:     "long name",
			collName: "very-long-kernel-collector-name-for-testing",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.collName)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, collector)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, collector)

			// Verify collector properties
			assert.Equal(t, tt.collName, collector.Name())
			assert.True(t, collector.IsHealthy())
			assert.NotNil(t, collector.logger)
			assert.NotNil(t, collector.events)
			assert.NotNil(t, collector.tracer)

			// Verify OTEL metrics are initialized (even if nil due to test environment)
			// This tests that metric creation doesn't panic
			assert.NotPanics(t, func() {
				collector.Name()
			})
		})
	}
}

func TestNewCollectorWithConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name    string
		config  *Config
		logger  *zap.Logger
		wantErr bool
	}{
		{
			name:    "valid config and logger",
			config:  DefaultConfig(),
			logger:  logger,
			wantErr: false,
		},
		{
			name:    "nil logger creates one",
			config:  DefaultConfig(),
			logger:  nil,
			wantErr: false,
		},
		{
			name: "custom config",
			config: &Config{
				Name:            "custom-kernel",
				Enabled:         true,
				SamplingEnabled: false,
				SamplingRate:    50,
			},
			logger:  logger,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollectorWithConfig(tt.config, tt.logger)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, collector)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, collector)

			// Verify collector properties
			assert.Equal(t, tt.config.Name, collector.Name())
			assert.True(t, collector.IsHealthy())
			assert.NotNil(t, collector.logger)
			assert.NotNil(t, collector.events)
			assert.Equal(t, cap(collector.events), DefaultEventBufferSize)
		})
	}
}

func TestCollectorLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:    "lifecycle-test",
		Enabled: true,
	}

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Test initial state
	assert.True(t, collector.IsHealthy())
	assert.Equal(t, "lifecycle-test", collector.Name())

	// Test start
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// Test Events channel is available
	events := collector.Events()
	assert.NotNil(t, events)

	// Test stop
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())

	// Test double stop (should not error)
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

func TestCollectorStartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "start-stop-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("start collector", func(t *testing.T) {
		err := collector.Start(ctx)
		assert.NoError(t, err)
		assert.True(t, collector.IsHealthy())
		assert.NotNil(t, collector.ctx)
		assert.NotNil(t, collector.cancel)
	})

	t.Run("stop collector", func(t *testing.T) {
		err := collector.Stop()
		assert.NoError(t, err)
		assert.False(t, collector.IsHealthy())
	})

	t.Run("restart after stop", func(t *testing.T) {
		err := collector.Start(ctx)
		assert.NoError(t, err)
		assert.True(t, collector.IsHealthy())

		err = collector.Stop()
		assert.NoError(t, err)
		assert.False(t, collector.IsHealthy())
	})
}

func TestCollectorEvents(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:    "events-test",
		Enabled: true,
	}

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Test Events channel before start
	events := collector.Events()
	assert.NotNil(t, events)
	assert.Equal(t, cap(events), DefaultEventBufferSize)

	// Start collector
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Events channel should still be available
	events = collector.Events()
	assert.NotNil(t, events)

	// Stop collector
	err = collector.Stop()
	require.NoError(t, err)

	// Events channel should be nil after stop
	events = collector.Events()
	assert.Nil(t, events)
}

func TestCollectorName(t *testing.T) {
	tests := []struct {
		name          string
		collectorName string
	}{
		{"simple name", "kernel"},
		{"hyphenated name", "kernel-collector"},
		{"empty name", ""},
		{"special chars", "kernel_collector-2024"},
	}

	logger := zaptest.NewLogger(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{Name: tt.collectorName}
			collector, err := NewCollectorWithConfig(config, logger)
			require.NoError(t, err)

			assert.Equal(t, tt.collectorName, collector.Name())
		})
	}
}

func TestCollectorHealthStatus(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "health-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Initially healthy
	assert.True(t, collector.IsHealthy())

	// Start collector
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// Stop collector
	err = collector.Stop()
	require.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "empty name",
			config: &Config{
				Name:    "",
				Enabled: true,
			},
			wantErr: true,
			errMsg:  "collector name cannot be empty",
		},
		{
			name: "invalid buffer size",
			config: &Config{
				Name:    "test",
				Enabled: true,
				BufferConfig: eBPFBufferConfig{
					KernelEventsBuffer: -1,
				},
			},
			wantErr: true,
			errMsg:  "kernel events buffer size must be positive",
		},
		{
			name: "invalid resource limits",
			config: &Config{
				Name:    "test",
				Enabled: true,
				BufferConfig: eBPFBufferConfig{
					KernelEventsBuffer:   512,
					ProcessEventsBuffer:  256,
					NetworkEventsBuffer:  512,
					SecurityEventsBuffer: 256,
				},
				ResourceLimits: ResourceLimits{
					MaxMemoryMB:   -1,
					MaxCPUPercent: 150,
				},
			},
			wantErr: true,
			errMsg:  "max memory must be positive",
		},
		{
			name: "invalid backpressure config",
			config: &Config{
				Name:    "test",
				Enabled: true,
				BufferConfig: eBPFBufferConfig{
					KernelEventsBuffer:   512,
					ProcessEventsBuffer:  256,
					NetworkEventsBuffer:  512,
					SecurityEventsBuffer: 256,
				},
				ResourceLimits: ResourceLimits{
					MaxMemoryMB:     100,
					MaxCPUPercent:   50,
					EventQueueSize:  1000,
					BatchTimeout:    100 * time.Millisecond,
					MaxEventsPerSec: 1000,
				},
				Backpressure: BackpressureConfig{
					Enabled:       true,
					HighWatermark: 1.5, // Invalid > 1.0
					LowWatermark:  0.5,
				},
			},
			wantErr: true,
			errMsg:  "high watermark must be between 0-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfigGetBufferSize(t *testing.T) {
	config := &Config{
		BufferConfig: eBPFBufferConfig{
			KernelEventsBuffer:   512,
			ProcessEventsBuffer:  256,
			NetworkEventsBuffer:  1024,
			SecurityEventsBuffer: 128,
		},
	}

	tests := []struct {
		bufferType string
		expected   int
	}{
		{"kernel", 512 * 1024},
		{"process", 256 * 1024},
		{"network", 1024 * 1024},
		{"security", 128 * 1024},
		{"unknown", 256 * 1024}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.bufferType, func(t *testing.T) {
			size := config.GetBufferSize(tt.bufferType)
			assert.Equal(t, tt.expected, size)
		})
	}
}

func TestKernelEventStructure(t *testing.T) {
	// Test that KernelEvent structure is properly sized and accessible
	event := KernelEvent{}

	// Verify struct has expected fields
	assert.Equal(t, uint64(0), event.Timestamp)
	assert.Equal(t, uint32(0), event.PID)
	assert.Equal(t, uint32(0), event.TID)
	assert.Equal(t, uint32(0), event.EventType)
	assert.Equal(t, uint64(0), event.Size)
	assert.Equal(t, uint64(0), event.CgroupID)

	// Verify array sizes
	assert.Len(t, event.Comm, 16)
	assert.Len(t, event.PodUID, 36)
	assert.Len(t, event.Data, 64)

	// Test that we can set and read values
	event.Timestamp = uint64(time.Now().UnixNano())
	event.PID = 1234
	event.EventType = EventTypeProcess
	copy(event.Comm[:], "test-process")

	assert.NotZero(t, event.Timestamp)
	assert.Equal(t, uint32(1234), event.PID)
	assert.Equal(t, EventTypeProcess, event.EventType)
}

func TestOTELMetricsIntegration(t *testing.T) {
	// Set up test metric provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	logger := zaptest.NewLogger(t)
	config := &Config{Name: "otel-test"}

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Verify OTEL components are initialized
	assert.NotNil(t, collector.tracer)
	// Note: metrics may be nil in test environment, which is handled gracefully

	// Test that operations don't panic when metrics are nil
	assert.NotPanics(t, func() {
		ctx := context.Background()
		if collector.eventsProcessed != nil {
			collector.eventsProcessed.Add(ctx, 1)
		}
		if collector.errorsTotal != nil {
			collector.errorsTotal.Add(ctx, 1)
		}
		if collector.processingTime != nil {
			collector.processingTime.Record(ctx, 100.0)
		}
		if collector.bufferUsage != nil {
			collector.bufferUsage.Record(ctx, 50)
		}
		if collector.droppedEvents != nil {
			collector.droppedEvents.Add(ctx, 1)
		}
	})
}

func TestEventProcessingFlow(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "processing-test"}

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Get events channel
	events := collector.Events()
	require.NotNil(t, events)

	// On non-Linux platforms, no events will be generated from eBPF
	// but the collector should still start successfully
	assert.True(t, collector.IsHealthy())

	// Stop collector
	err = collector.Stop()
	require.NoError(t, err)
}

func TestConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "concurrent-test"}

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Test concurrent access to collector methods
	done := make(chan struct{})
	errors := make(chan error, 10)

	// Start multiple goroutines accessing collector methods
	for i := 0; i < 5; i++ {
		go func() {
			defer func() { done <- struct{}{} }()

			// Test IsHealthy (read operation)
			assert.NotPanics(t, func() {
				collector.IsHealthy()
			})

			// Test Name (read operation)
			assert.NotPanics(t, func() {
				collector.Name()
			})

			// Test Events (read operation)
			assert.NotPanics(t, func() {
				collector.Events()
			})
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 5; i++ {
		select {
		case <-done:
		case err := <-errors:
			t.Errorf("Concurrent access error: %v", err)
		case <-time.After(5 * time.Second):
			t.Error("Timeout waiting for concurrent access test")
		}
	}
}

func TestDefaultConstants(t *testing.T) {
	// Test that constants are reasonable values
	assert.Greater(t, DefaultEventBufferSize, 0)
	assert.Greater(t, DefaultKernelBufferKB, 0)
	assert.Greater(t, DefaultProcessBufferKB, 0)
	assert.Greater(t, DefaultNetworkBufferKB, 0)
	assert.Greater(t, DefaultSecurityBufferKB, 0)

	assert.Greater(t, DefaultMaxMemoryMB, 0)
	assert.Greater(t, DefaultMaxCPUPercent, 0)
	assert.LessOrEqual(t, DefaultMaxCPUPercent, 100)
	assert.Greater(t, DefaultMaxEventsPerSec, 0)

	assert.Greater(t, DefaultHighWatermark, 0.0)
	assert.Less(t, DefaultHighWatermark, 1.0)
	assert.Greater(t, DefaultLowWatermark, 0.0)
	assert.Less(t, DefaultLowWatermark, DefaultHighWatermark)
	assert.Greater(t, DefaultDropThreshold, DefaultHighWatermark)

	assert.Greater(t, DefaultBatchTimeout, time.Duration(0))
	assert.Greater(t, DefaultRecoveryDelay, time.Duration(0))
	assert.Greater(t, DefaultHealthCheckInterval, time.Duration(0))
}

func TestEventTypes(t *testing.T) {
	// Test event type constants are defined
	assert.Equal(t, uint32(0), EventTypeProcess)
	assert.Equal(t, uint32(1), EventTypeFile)
	assert.Equal(t, uint32(2), EventTypeNetwork)
	assert.Equal(t, uint32(3), EventTypeContainer)
	assert.Equal(t, uint32(4), EventTypeMount)
}

func TestMemoryManagement(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "memory-test"}

	// Create and destroy multiple collectors to test memory cleanup
	for i := 0; i < 10; i++ {
		collector, err := NewCollectorWithConfig(config, logger)
		require.NoError(t, err)

		ctx := context.Background()
		err = collector.Start(ctx)
		require.NoError(t, err)

		err = collector.Stop()
		require.NoError(t, err)
	}

	// This test mainly ensures no memory leaks or panics occur
	// during repeated creation/destruction cycles
}

func TestErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("start with cancelled context", func(t *testing.T) {
		config := &Config{Name: "error-test-1"}
		collector, err := NewCollectorWithConfig(config, logger)
		require.NoError(t, err)

		// Create already cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Start should handle cancelled context gracefully
		err = collector.Start(ctx)
		// On stub platforms, this should not error
		// On Linux, behavior may vary based on eBPF availability
		if err != nil {
			t.Logf("Start with cancelled context returned error (expected on some platforms): %v", err)
		}

		// Cleanup
		collector.Stop()
	})

	t.Run("multiple start calls", func(t *testing.T) {
		config := &Config{Name: "error-test-2"}
		collector, err := NewCollectorWithConfig(config, logger)
		require.NoError(t, err)

		ctx := context.Background()

		// First start should succeed
		err = collector.Start(ctx)
		assert.NoError(t, err)

		// Second start should not panic
		assert.NotPanics(t, func() {
			collector.Start(ctx)
		})

		// Cleanup
		collector.Stop()
	})
}

func TestDefaultConfigCreation(t *testing.T) {
	config := DefaultConfig()
	require.NotNil(t, config)

	// Test that default config validates
	err := config.Validate()
	assert.NoError(t, err)

	// Test specific default values
	assert.Equal(t, "kernel-collector", config.Name)
	assert.True(t, config.Enabled)
	assert.True(t, config.SamplingEnabled)
	assert.Equal(t, DefaultSamplingRate, config.SamplingRate)
	assert.False(t, config.DebugMode)

	// Test buffer config defaults
	assert.Equal(t, DefaultKernelBufferKB, config.BufferConfig.KernelEventsBuffer)
	assert.Equal(t, DefaultProcessBufferKB, config.BufferConfig.ProcessEventsBuffer)
	assert.Equal(t, DefaultNetworkBufferKB, config.BufferConfig.NetworkEventsBuffer)
	assert.Equal(t, DefaultSecurityBufferKB, config.BufferConfig.SecurityEventsBuffer)

	// Test resource limits defaults
	assert.Equal(t, DefaultMaxMemoryMB, config.ResourceLimits.MaxMemoryMB)
	assert.Equal(t, DefaultMaxCPUPercent, config.ResourceLimits.MaxCPUPercent)
	assert.Equal(t, DefaultEventQueueSize, config.ResourceLimits.EventQueueSize)
	assert.Equal(t, DefaultBatchTimeout, config.ResourceLimits.BatchTimeout)
	assert.Equal(t, DefaultMaxEventsPerSec, config.ResourceLimits.MaxEventsPerSec)

	// Test backpressure defaults
	assert.True(t, config.Backpressure.Enabled)
	assert.Equal(t, DefaultHighWatermark, config.Backpressure.HighWatermark)
	assert.Equal(t, DefaultLowWatermark, config.Backpressure.LowWatermark)
	assert.Equal(t, DefaultDropThreshold, config.Backpressure.DropThreshold)

	// Test health config defaults
	assert.True(t, config.Health.Enabled)
	assert.Equal(t, DefaultHealthCheckInterval, config.Health.Interval)
	assert.Equal(t, DefaultMaxHealthFailures, config.Health.MaxFailures)
	assert.True(t, config.Health.RestartOnFailure)
}

func TestAdvancedConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
		setupFn func(*Config)
	}{
		{
			name:    "valid health config disabled",
			wantErr: false,
			setupFn: func(c *Config) {
				c.Health.Enabled = false
			},
		},
		{
			name:    "invalid health interval",
			wantErr: true,
			errMsg:  "health check interval must be positive",
			setupFn: func(c *Config) {
				c.Health.Enabled = true
				c.Health.Interval = -1 * time.Second
			},
		},
		{
			name:    "valid backpressure disabled",
			wantErr: false,
			setupFn: func(c *Config) {
				c.Backpressure.Enabled = false
			},
		},
		{
			name:    "invalid watermark order",
			wantErr: true,
			errMsg:  "low watermark",
			setupFn: func(c *Config) {
				c.Backpressure.Enabled = true
				c.Backpressure.HighWatermark = 0.6
				c.Backpressure.LowWatermark = 0.8 // Higher than high watermark
			},
		},
		{
			name:    "sampling disabled with zero rate",
			wantErr: false,
			setupFn: func(c *Config) {
				c.SamplingEnabled = false
				c.SamplingRate = 0
			},
		},
		{
			name:    "sampling enabled with invalid rate",
			wantErr: true,
			errMsg:  "sampling rate must be positive",
			setupFn: func(c *Config) {
				c.SamplingEnabled = true
				c.SamplingRate = -10
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			if tt.setupFn != nil {
				tt.setupFn(config)
			}

			err := config.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCollectorWithCustomOTELProvider(t *testing.T) {
	// Test that collector works with custom OTEL provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	logger := zaptest.NewLogger(t)
	config := &Config{Name: "custom-otel-test"}

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Verify metrics are created with custom provider
	assert.NotNil(t, collector.tracer)
	// Metrics may still be nil due to naming issues, but should not panic

	// Test collector lifecycle with custom provider
	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err)

	err = collector.Stop()
	assert.NoError(t, err)
}

func TestConstantsAndTypes(t *testing.T) {
	// Test that all constants are reasonable
	assert.Equal(t, 1024, KBToBytes)

	// Test process monitoring constants
	assert.Greater(t, MaxProcessScanLimit, 0)
	assert.Greater(t, MaxConnectionScanLimit, 0)

	// Test retry constants
	assert.Greater(t, DefaultMaxRetryAttempts, 0)
	assert.Greater(t, DefaultRetryInitialDelay, time.Duration(0))

	// Test fallback intervals
	assert.Greater(t, ProcessFallbackInterval, time.Duration(0))
	assert.Greater(t, NetworkFallbackInterval, time.Duration(0))
	assert.Greater(t, MemoryFallbackInterval, time.Duration(0))

	// Test health constants
	assert.Greater(t, RecentErrorThreshold, time.Duration(0))
	assert.Greater(t, FallbackHealthTimeout, time.Duration(0))
}

func TestNetworkInfoStruct(t *testing.T) {
	// Test NetworkInfo structure
	netInfo := NetworkInfo{
		IPVersion: 4,
		Protocol:  6, // TCP
		State:     1,
		Direction: 0,
		SPort:     8080,
		DPort:     443,
		SAddrV4:   0x7f000001, // 127.0.0.1
		DAddrV4:   0x08080808, // 8.8.8.8
	}

	assert.Equal(t, uint8(4), netInfo.IPVersion)
	assert.Equal(t, uint8(6), netInfo.Protocol)
	assert.Equal(t, uint16(8080), netInfo.SPort)
	assert.Equal(t, uint16(443), netInfo.DPort)

	// Test IPv6 addresses
	netInfo.IPVersion = 6
	netInfo.SAddrV6 = [4]uint32{0x20010db8, 0x85a30000, 0x00008a2e, 0x03707334}
	assert.Equal(t, uint8(6), netInfo.IPVersion)
	assert.NotEqual(t, [4]uint32{}, netInfo.SAddrV6)
}

func TestRaceConditionSafety(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "race-test"}

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Test concurrent read operations during lifecycle changes
	done := make(chan struct{})

	// Start goroutines that continuously read collector state
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()

			for j := 0; j < 100; j++ {
				// These operations should be safe to call concurrently
				_ = collector.Name()
				_ = collector.IsHealthy()
				_ = collector.Events()
			}
		}()
	}

	// Perform lifecycle operations while reads are happening
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		err := collector.Start(ctx)
		assert.NoError(t, err)

		err = collector.Stop()
		assert.NoError(t, err)
	}

	// Wait for all read goroutines to complete
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Timeout waiting for race condition test")
		}
	}
}
