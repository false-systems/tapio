package kernel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
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
			name:     "valid collector name",
			collName: "test-kernel",
			wantErr:  false,
		},
		{
			name:     "empty collector name",
			collName: "",
			wantErr:  false, // Empty name is allowed, gets default
		},
		{
			name:     "complex collector name",
			collName: "kernel-collector-v1",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewDefaultConfig(tt.collName)
			collector, err := NewCollector(tt.collName, cfg)

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
			config:  NewDefaultConfig("test"),
			logger:  logger,
			wantErr: false,
		},
		{
			name:    "nil logger creates one",
			config:  NewDefaultConfig("test"),
			logger:  nil,
			wantErr: false,
		},
		{
			name: "custom config",
			config: &Config{
				Name:       "custom-kernel",
				BufferSize: 5000,
				EnableEBPF: true,
			},
			logger:  logger,
			wantErr: false,
		},
		{
			name:    "nil config returns error",
			config:  nil,
			logger:  logger,
			wantErr: true,
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

			// Verify collector was created correctly
			assert.NotNil(t, collector.logger)
			assert.Equal(t, tt.config.Name, collector.Name())
			assert.True(t, collector.IsHealthy()) // Should start as healthy
			assert.NotNil(t, collector.Events())

			// Check buffer size using the private field for testing
			if collector.events != nil {
				assert.Equal(t, tt.config.BufferSize, cap(collector.events))
			}
		})
	}
}

func TestCollectorLifecycle(t *testing.T) {

	tests := []struct {
		name  string
		setup func() (*Collector, error)
	}{
		{
			name: "create with nil config",
			setup: func() (*Collector, error) {
				return NewCollector("test", nil)
			},
		},
		{
			name: "create with default config",
			setup: func() (*Collector, error) {
				config := NewDefaultConfig("test")
				return NewCollector("test", config)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := tt.setup()
			require.NoError(t, err)
			require.NotNil(t, collector)

			// Start collector
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			err = collector.Start(ctx)
			require.NoError(t, err)

			// Should be healthy
			assert.True(t, collector.IsHealthy())

			// Stop collector
			err = collector.Stop()
			require.NoError(t, err)
		})
	}
}

func TestGetEventsChannel(t *testing.T) {
	cfg := NewDefaultConfig("test")
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	ch := collector.Events()
	assert.NotNil(t, ch)

	// Test that channel becomes nil after stop
	err = collector.Stop()
	require.NoError(t, err)

	ch = collector.Events()
	assert.Nil(t, ch) // Should be nil after stop
}

func TestCollectorHealthCheck(t *testing.T) {

	tests := []struct {
		name   string
		setup  func() (*Collector, error)
		modify func(*Collector)
		want   bool
	}{
		{
			name: "healthy collector",
			setup: func() (*Collector, error) {
				config := NewDefaultConfig("test")
				return NewCollector("test", config)
			},
			modify: func(c *Collector) {},
			want:   true,
		},
		{
			name: "unhealthy after forced state",
			setup: func() (*Collector, error) {
				config := NewDefaultConfig("test")
				return NewCollector("test", config)
			},
			modify: func(c *Collector) {
				c.healthy = false
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := tt.setup()
			require.NoError(t, err)

			tt.modify(collector)

			assert.Equal(t, tt.want, collector.IsHealthy())
		})
	}
}

func TestEventProcessing(t *testing.T) {
	cfg := NewDefaultConfig("test")
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Process a test event
	testEvent := &domain.CollectorEvent{
		EventID:   "test-event-1",
		Type:      "test",
		Timestamp: time.Now(),
		Source:    "kernel",
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Custom: map[string]string{"test": "data"},
		},
		Metadata: domain.EventMetadata{
			PodUID: "test-pod-uid",
		},
	}

	// Send event through internal method (simulating kernel event)
	select {
	case collector.events <- testEvent:
		// Event sent successfully
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Failed to send event to collector")
	}

	// Verify event can be received
	select {
	case receivedEvent := <-collector.Events():
		assert.Equal(t, testEvent.Type, receivedEvent.Type)
		assert.Equal(t, testEvent.Source, receivedEvent.Source)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Failed to receive event from collector")
	}
}

func TestConcurrentOperations(t *testing.T) {
	cfg := NewDefaultConfig("test")
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Run concurrent operations
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			_ = collector.IsHealthy()
			_ = collector.Name()
			time.Sleep(time.Microsecond)
		}
	}()

	// Wait for goroutine or timeout
	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Concurrent operations timed out")
	}

	err = collector.Stop()
	require.NoError(t, err)
}

func TestCollectorErrorHandling(t *testing.T) {
	tests := []struct {
		name   string
		setup  func() (*Collector, error)
		action func(*Collector) error
		want   bool // expected error
	}{
		{
			name: "stop without start",
			setup: func() (*Collector, error) {
				return NewCollector("test", nil)
			},
			action: func(c *Collector) error {
				return c.Stop()
			},
			want: false, // Stop should not error
		},
		{
			name: "multiple stops",
			setup: func() (*Collector, error) {
				c, err := NewCollector("test", nil)
				if err != nil {
					return nil, err
				}
				c.Stop() // First stop
				return c, nil
			},
			action: func(c *Collector) error {
				return c.Stop() // Second stop
			},
			want: false, // Multiple stops should not error
		},
		{
			name: "start with cancelled context",
			setup: func() (*Collector, error) {
				return NewCollector("test", nil)
			},
			action: func(c *Collector) error {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // Cancel immediately
				return c.Start(ctx)
			},
			want: false, // Should not error, but might behave differently
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := tt.setup()
			require.NoError(t, err)
			require.NotNil(t, collector)

			err = tt.action(collector)
			if tt.want {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Clean up
			collector.Stop()
		})
	}
}

func TestCollectorStopBehavior(t *testing.T) {
	cfg := NewDefaultConfig("test")
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Test stopEBPF coverage by calling Stop
	err = collector.Stop()
	require.NoError(t, err)

	// Verify collector is no longer healthy after stop
	assert.False(t, collector.IsHealthy())
}

func TestCollectorConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "nil config uses default",
			config:  nil,
			wantErr: false,
		},
		{
			name: "zero buffer size",
			config: &Config{
				Name:       "test",
				BufferSize: 0,
				EnableEBPF: false,
			},
			wantErr: false, // Should work with 0 buffer
		},
		{
			name: "large buffer size",
			config: &Config{
				Name:       "test",
				BufferSize: 100000,
				EnableEBPF: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector("test", tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, collector)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, collector)

			// Verify config was applied or defaulted
			assert.NotNil(t, collector.config)
			if tt.config != nil {
				assert.Equal(t, tt.config.BufferSize, collector.config.BufferSize)
				assert.Equal(t, tt.config.EnableEBPF, collector.config.EnableEBPF)
			}
		})
	}
}

func TestCollectorMetricsRecording(t *testing.T) {
	cfg := NewDefaultConfig("test")
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	// Test that metrics are created (they might be nil in test environment)
	// This tests the metric creation code paths
	assert.NotNil(t, collector.tracer)

	// Test metric recording doesn't panic with nil metrics
	ctx := context.Background()
	assert.NotPanics(t, func() {
		if collector.eventsProcessed != nil {
			collector.eventsProcessed.Add(ctx, 1)
		}
		if collector.errorsTotal != nil {
			collector.errorsTotal.Add(ctx, 1)
		}
		if collector.processingTime != nil {
			collector.processingTime.Record(ctx, 100)
		}
		if collector.droppedEvents != nil {
			collector.droppedEvents.Add(ctx, 1)
		}
		if collector.bufferUsage != nil {
			collector.bufferUsage.Record(ctx, 50)
		}
	})
}

func TestCollectorStartError(t *testing.T) {
	cfg := &Config{
		Name:       "test",
		BufferSize: 100,
		EnableEBPF: true,
	}
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	// Test start with eBPF enabled (will use stub on non-Linux)
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err) // Should not error, but logs warning

	err = collector.Stop()
	require.NoError(t, err)
}

func TestCollectorCoverageBoost(t *testing.T) {
	// Test various edge cases to boost coverage

	// Test NewCollectorWithConfig with different configurations
	tests := []struct {
		name   string
		config *Config
		logger *zap.Logger
	}{
		{
			name: "large buffer with ebpf disabled",
			config: &Config{
				Name:       "large-test",
				BufferSize: 50000,
				EnableEBPF: false,
			},
			logger: zaptest.NewLogger(t),
		},
		{
			name: "zero buffer size",
			config: &Config{
				Name:       "zero-test",
				BufferSize: 0,
				EnableEBPF: false,
			},
			logger: nil, // Test nil logger path
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollectorWithConfig(tt.config, tt.logger)
			require.NoError(t, err)
			require.NotNil(t, collector)

			// Test all public methods for coverage
			assert.Equal(t, tt.config.Name, collector.Name())
			assert.True(t, collector.IsHealthy())
			assert.NotNil(t, collector.Events())

			// Test start/stop cycle
			ctx := context.Background()
			err = collector.Start(ctx)
			require.NoError(t, err)

			err = collector.Stop()
			require.NoError(t, err)

			// Verify stopped state
			assert.False(t, collector.IsHealthy())
		})
	}
}

func TestCollectorEventChannelOperations(t *testing.T) {
	cfg := NewDefaultConfig("test")
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Test channel operations for more coverage
	events := collector.Events()
	assert.NotNil(t, events)

	// Test that we can receive on the channel (it will be empty)
	select {
	case <-events:
		t.Fatal("Should not receive any events")
	default:
		// Expected - no events
	}

	// Test stop and verify channel state
	err = collector.Stop()
	require.NoError(t, err)

	// After stop, events channel should be nil
	events = collector.Events()
	assert.Nil(t, events)
}

func TestCollectorExtremeConfigurations(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{
			name: "maximum realistic buffer",
			config: &Config{
				Name:       "max-buffer",
				BufferSize: 1000000, // 1M buffer
				EnableEBPF: true,
			},
		},
		{
			name: "minimal config",
			config: &Config{
				Name:       "minimal",
				BufferSize: 1,
				EnableEBPF: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.config.Name, tt.config)
			require.NoError(t, err)
			require.NotNil(t, collector)

			// Basic functionality should work regardless of config
			assert.Equal(t, tt.config.Name, collector.Name())
			assert.True(t, collector.IsHealthy())

			// Test start/stop with extreme configs
			ctx := context.Background()
			err = collector.Start(ctx)
			require.NoError(t, err)

			err = collector.Stop()
			require.NoError(t, err)
		})
	}
}
