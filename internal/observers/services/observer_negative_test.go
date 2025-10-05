package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestObserver_InvalidConfiguration tests error handling for invalid configs
func TestObserver_InvalidConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		expectErr bool
		errMsg    string
	}{
		{
			name: "negative_connection_table_size",
			config: &Config{
				ConnectionTableSize: -1,
				ConnectionTimeout:   defaultConnectionTimeout,
				BufferSize:          defaultBufferSize,
				CleanupInterval:     defaultCleanupInterval,
				Name:                "test",
			},
			expectErr: true,
			errMsg:    "connection_table_size must be positive",
		},
		{
			name: "zero_connection_table_size",
			config: &Config{
				ConnectionTableSize: 0,
				ConnectionTimeout:   defaultConnectionTimeout,
				BufferSize:          defaultBufferSize,
				CleanupInterval:     defaultCleanupInterval,
				Name:                "test",
			},
			expectErr: true,
			errMsg:    "connection_table_size must be positive",
		},
		{
			name: "negative_connection_timeout",
			config: &Config{
				ConnectionTableSize: defaultConnectionTableSize,
				ConnectionTimeout:   -1,
				BufferSize:          defaultBufferSize,
				CleanupInterval:     defaultCleanupInterval,
				Name:                "test",
			},
			expectErr: true,
			errMsg:    "connection_timeout must be positive",
		},
		{
			name: "negative_buffer_size",
			config: &Config{
				ConnectionTableSize: defaultConnectionTableSize,
				ConnectionTimeout:   defaultConnectionTimeout,
				BufferSize:          -1,
				CleanupInterval:     defaultCleanupInterval,
				Name:                "test",
			},
			expectErr: true,
			errMsg:    "buffer_size must be positive",
		},
		{
			name: "negative_cleanup_interval",
			config: &Config{
				ConnectionTableSize: defaultConnectionTableSize,
				ConnectionTimeout:   defaultConnectionTimeout,
				BufferSize:          defaultBufferSize,
				CleanupInterval:     -1,
				Name:                "test",
			},
			expectErr: true,
			errMsg:    "cleanup_interval must be positive",
		},
		{
			name: "k8s_enabled_negative_refresh_interval",
			config: &Config{
				ConnectionTableSize: defaultConnectionTableSize,
				ConnectionTimeout:   defaultConnectionTimeout,
				BufferSize:          defaultBufferSize,
				CleanupInterval:     defaultCleanupInterval,
				EnableK8sMapping:    true,
				K8sRefreshInterval:  -1,
				PodMappingTimeout:   defaultPodMappingTimeout,
				Name:                "test",
			},
			expectErr: true,
			errMsg:    "k8s_refresh_interval must be positive when K8s mapping enabled",
		},
		{
			name: "k8s_enabled_negative_pod_timeout",
			config: &Config{
				ConnectionTableSize: defaultConnectionTableSize,
				ConnectionTimeout:   defaultConnectionTimeout,
				BufferSize:          defaultBufferSize,
				CleanupInterval:     defaultCleanupInterval,
				EnableK8sMapping:    true,
				K8sRefreshInterval:  defaultK8sRefreshInterval,
				PodMappingTimeout:   -1,
				Name:                "test",
			},
			expectErr: true,
			errMsg:    "pod_mapping_timeout must be positive when K8s mapping enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := NewObserver(tt.config.Name, tt.config, zap.NewNop())

			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, observer)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, observer)
			}
		})
	}
}

// TestObserver_NilConfiguration tests handling of nil config
func TestObserver_NilConfiguration(t *testing.T) {
	// Note: Default config enables K8s mapping which requires K8s cluster
	// This test will succeed if K8s is available, otherwise will fail appropriately
	observer, err := NewObserver("test", nil, zap.NewNop())

	// Should either succeed or fail with K8s error
	if err != nil {
		assert.Contains(t, err.Error(), "K8s")
		return
	}

	require.NotNil(t, observer)
	assert.NotNil(t, observer.config)
}

// TestObserver_NilLogger tests handling of nil logger
func TestObserver_NilLogger(t *testing.T) {
	config := DefaultConfig()
	config.EnableK8sMapping = false // Disable K8s for testing

	observer, err := NewObserver("test", config, nil)
	require.NoError(t, err)
	require.NotNil(t, observer)

	// Should use no-op logger
	assert.NotNil(t, observer.logger)
}

// TestObserver_StartWithoutContext tests start with canceled context
func TestObserver_StartWithoutContext(t *testing.T) {
	config := DefaultConfig()
	config.EnableK8sMapping = false

	observer, err := NewObserver("test", config, zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = observer.Start(ctx)
	// Error may or may not occur depending on timing
	// The important thing is it doesn't panic
	defer observer.Stop()
}

// TestObserver_StopBeforeStart tests stopping before starting
func TestObserver_StopBeforeStart(t *testing.T) {
	config := DefaultConfig()
	config.EnableK8sMapping = false

	observer, err := NewObserver("test", config, zap.NewNop())
	require.NoError(t, err)

	err = observer.Stop()
	assert.NoError(t, err)
}

// TestObserver_DoubleStart tests starting twice
func TestObserver_DoubleStart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping negative test in short mode")
	}

	config := DefaultConfig()
	config.EnableK8sMapping = false

	observer, err := NewObserver("test", config, zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Second start should either return error or be no-op
	err2 := observer.Start(ctx)
	// We don't require error, just shouldn't panic
	_ = err2
}

// TestObserver_DoubleStop tests stopping twice
func TestObserver_DoubleStop(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping negative test in short mode")
	}

	config := DefaultConfig()
	config.EnableK8sMapping = false

	observer, err := NewObserver("test", config, zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)

	err = observer.Stop()
	assert.NoError(t, err)

	// Second stop should be no-op
	err = observer.Stop()
	assert.NoError(t, err)
}

// TestObserver_EventChannelFull tests behavior when event channel is full
func TestObserver_EventChannelFull(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping negative test in short mode")
	}

	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          10, // Very small buffer
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    false,
		Name:                "full-buffer",
		HealthCheck:         true,
		EnableOTEL:          false,
		EnableStdout:        false,
	}

	observer, err := NewObserver(config.Name, config, zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Don't consume events, let buffer fill
	time.Sleep(2 * time.Second)

	// Observer should still be healthy and not crash
	// Drops should be counted
	stats := observer.GetStats()
	assert.NotNil(t, stats)
}

// TestObserver_GetStatsWhileStopped tests stats retrieval when stopped
func TestObserver_GetStatsWhileStopped(t *testing.T) {
	config := DefaultConfig()
	config.EnableK8sMapping = false

	observer, err := NewObserver("test", config, zap.NewNop())
	require.NoError(t, err)

	// Get stats before starting
	stats := observer.GetStats()
	assert.NotNil(t, stats)
	assert.Equal(t, uint64(0), stats.ActiveConnections)
}

// TestObserver_GetServiceMapWithoutK8s tests service map when K8s is disabled
func TestObserver_GetServiceMapWithoutK8s(t *testing.T) {
	config := DefaultConfig()
	config.EnableK8sMapping = false

	observer, err := NewObserver("test", config, zap.NewNop())
	require.NoError(t, err)

	serviceMap := observer.GetServiceMap()
	assert.NotNil(t, serviceMap)
	assert.Empty(t, serviceMap)
}

// TestConnectionTracker_StopBeforeStart tests tracker stop before start
func TestConnectionTracker_StopBeforeStart(t *testing.T) {
	config := DefaultConfig()
	tracker := NewConnectionTracker(config, zap.NewNop())
	require.NotNil(t, tracker)

	err := tracker.Stop()
	assert.NoError(t, err)
}

// TestConfig_Validate tests all validation rules
func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name       string
		modifyFunc func(*Config)
		expectErr  bool
		errMsg     string
	}{
		{
			name: "valid_config",
			modifyFunc: func(c *Config) {
				// No modification
			},
			expectErr: false,
		},
		{
			name: "invalid_connection_table_size",
			modifyFunc: func(c *Config) {
				c.ConnectionTableSize = 0
			},
			expectErr: true,
			errMsg:    "connection_table_size must be positive",
		},
		{
			name: "invalid_connection_timeout",
			modifyFunc: func(c *Config) {
				c.ConnectionTimeout = 0
			},
			expectErr: true,
			errMsg:    "connection_timeout must be positive",
		},
		{
			name: "invalid_buffer_size",
			modifyFunc: func(c *Config) {
				c.BufferSize = 0
			},
			expectErr: true,
			errMsg:    "buffer_size must be positive",
		},
		{
			name: "invalid_cleanup_interval",
			modifyFunc: func(c *Config) {
				c.CleanupInterval = 0
			},
			expectErr: true,
			errMsg:    "cleanup_interval must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			tt.modifyFunc(config)

			err := config.Validate()
			if tt.expectErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
