package cri

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewDefaultConfig tests the default configuration
func TestNewDefaultConfig(t *testing.T) {
	config := NewDefaultConfig("test-cri")

	require.NotNil(t, config)
	assert.Equal(t, "test-cri", config.Name)
	assert.Equal(t, "", config.SocketPath) // Auto-detect
	assert.Equal(t, 10000, config.BufferSize)
	assert.Equal(t, 5*time.Second, config.PollInterval)
}

// TestNewCollector tests basic collector creation
func TestNewCollector(t *testing.T) {
	config := NewDefaultConfig("test")
	config.SocketPath = "/tmp/test.sock" // Use explicit path for testing

	collector, err := NewCollector("test-cri", config)

	require.NoError(t, err)
	require.NotNil(t, collector)
	assert.Equal(t, "test-cri", collector.Name())
}

// TestCollectorHealthy tests health check
func TestCollectorHealthy(t *testing.T) {
	config := NewDefaultConfig("test")
	config.SocketPath = "/tmp/test.sock" // Use explicit path for testing

	collector, err := NewCollector("test-cri", config)
	require.NoError(t, err)

	// Collector should not be healthy until started
	assert.False(t, collector.IsHealthy())
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name: "valid config",
			config: func() *Config {
				c := NewDefaultConfig("test")
				c.SocketPath = "/tmp/test.sock"
				return c
			}(),
			expectError: false,
		},
		{
			name: "zero buffer size",
			config: &Config{
				Name:         "test",
				SocketPath:   "/test.sock",
				BufferSize:   0,
				PollInterval: time.Second,
			},
			expectError: false, // Should use default
		},
		{
			name: "zero poll interval",
			config: &Config{
				Name:         "test",
				SocketPath:   "/test.sock",
				BufferSize:   1000,
				PollInterval: 0,
			},
			expectError: false, // Should use default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCollector("test", tt.config)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
