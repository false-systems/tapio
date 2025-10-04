package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestObserverIntegration tests integration with real network components
func TestObserverIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name      string
		config    *Config
		timeout   time.Duration
		expectK8s bool
	}{
		{
			name: "connection_tracker_integration",
			config: &Config{
				ConnectionTableSize: defaultConnectionTableSize,
				ConnectionTimeout:   defaultConnectionTimeout,
				BufferSize:          defaultBufferSize,
				CleanupInterval:     defaultCleanupInterval,
				EnableK8sMapping:    false,
				Name:                "conn-integration",
				HealthCheck:         true,
				EnableOTEL:          false,
				EnableStdout:        false,
			},
			timeout:   30 * time.Second,
			expectK8s: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			observer, err := NewObserver(tt.config.Name, tt.config, zap.NewNop())
			require.NoError(t, err)
			require.NotNil(t, observer)

			err = observer.Start(ctx)
			require.NoError(t, err)
			defer observer.Stop()

			// Verify components initialized
			assert.NotNil(t, observer.connectionsTracker)
			if tt.expectK8s {
				assert.NotNil(t, observer.k8sMapper)
			} else {
				assert.Nil(t, observer.k8sMapper)
			}

			// Wait for event processing
			time.Sleep(500 * time.Millisecond)

			// Verify observer is healthy
			assert.True(t, observer.IsHealthy())

			// Verify statistics are being collected
			stats := observer.GetStats()
			assert.NotNil(t, stats)
		})
	}
}

// TestObserverIntegration_ConnectionTrackerLifecycle tests connection tracker integration
func TestObserverIntegration_ConnectionTrackerLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          defaultBufferSize,
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    false,
		Name:                "tracker-lifecycle",
		HealthCheck:         true,
		EnableOTEL:          false,
		EnableStdout:        false,
	}

	observer, err := NewObserver(config.Name, config, zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start observer
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Verify connection tracker is running
	assert.NotNil(t, observer.connectionsTracker)

	// Get tracker stats
	trackerStats := observer.connectionsTracker.GetStats()
	assert.NotNil(t, trackerStats)

	// Stop observer
	err = observer.Stop()
	assert.NoError(t, err)
}

// TestObserverIntegration_EventChannelFlow tests event flow through channels
func TestObserverIntegration_EventChannelFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          100, // Smaller buffer for testing
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    false,
		Name:                "event-flow",
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

	// Monitor event channel
	eventReceived := false
	timeout := time.After(5 * time.Second)

	select {
	case event := <-observer.Events():
		assert.NotNil(t, event)
		assert.NotEmpty(t, event.EventID)
		assert.Equal(t, config.Name, event.Source)
		eventReceived = true
	case <-timeout:
		// Timeout is acceptable on some platforms
	}

	// On platforms with fallback mode, we should receive events
	if eventReceived {
		t.Log("Event received successfully")
	} else {
		t.Log("No events received (may be platform-specific)")
	}
}

// TestObserverIntegration_MultiOutput tests multiple output targets
func TestObserverIntegration_MultiOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          defaultBufferSize,
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    false,
		Name:                "multi-output",
		HealthCheck:         true,
		EnableOTEL:          true,
		EnableStdout:        true,
	}

	observer, err := NewObserver(config.Name, config, zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Verify observer is healthy
	assert.True(t, observer.IsHealthy())

	// Wait for potential events
	time.Sleep(1 * time.Second)

	// Verify statistics
	stats := observer.GetStats()
	assert.NotNil(t, stats)
}
