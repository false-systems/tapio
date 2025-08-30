package syscallerrors

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewCollector(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "default config",
			config:  nil,
			wantErr: false,
		},
		{
			name: "custom config",
			config: &Config{
				RingBufferSize:   16 * 1024 * 1024,
				EventChannelSize: 5000,
				RateLimitMs:      200,
				EnabledCategories: map[string]bool{
					"file":    true,
					"network": true,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(logger, tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, collector)
			assert.Equal(t, "syscall-errors", collector.Name())
			assert.NotNil(t, collector.Events())
		})
	}
}

func TestCollectorName(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)

	assert.Equal(t, "syscall-errors", collector.Name())
}

func TestCollectorEvents(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)

	ch := collector.Events()
	assert.NotNil(t, ch)

	// Channel should be readable
	select {
	case <-ch:
		// Channel might be closed or have data
	default:
		// Channel is open and empty, which is expected
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, 8*1024*1024, config.RingBufferSize)
	assert.Equal(t, 10000, config.EventChannelSize)
	assert.Equal(t, 100, config.RateLimitMs)
	assert.Equal(t, map[string]bool{
		"file":    true,
		"network": true,
		"memory":  true,
	}, config.EnabledCategories)
	assert.False(t, config.RequireAllMetrics)
}

// Linux-specific tests are in collector_linux_test.go
// These tests verify the basic structure and configuration
