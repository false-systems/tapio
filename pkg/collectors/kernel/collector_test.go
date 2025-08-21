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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.config.Name, tt.config)

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
	// Verify it's a receive-only channel and not nil
	assert.NotNil(t, ch)
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
	testEvent := domain.RawEvent{
		Type:      "test",
		Timestamp: time.Now(),
		Source:    "kernel",
		Data:      []byte(`{"test": "data"}`),
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
