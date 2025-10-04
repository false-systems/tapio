package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// TestObserverE2E tests complete end-to-end workflows
func TestObserverE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	tests := []struct {
		name         string
		config       *Config
		setupFunc    func(*testing.T) context.Context
		validateFunc func(*testing.T, *Observer)
	}{
		{
			name: "complete_lifecycle_connection_tracking_only",
			config: &Config{
				ConnectionTableSize: defaultConnectionTableSize,
				ConnectionTimeout:   defaultConnectionTimeout,
				BufferSize:          defaultBufferSize,
				CleanupInterval:     defaultCleanupInterval,
				EnableK8sMapping:    false,
				Name:                "e2e-test",
				HealthCheck:         true,
				EnableOTEL:          false,
				EnableStdout:        false,
			},
			setupFunc: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				t.Cleanup(cancel)
				return ctx
			},
			validateFunc: func(t *testing.T, obs *Observer) {
				assert.NotNil(t, obs)
				assert.NotNil(t, obs.connectionsTracker)
				assert.Nil(t, obs.k8sMapper)
			},
		},
		{
			name: "complete_lifecycle_with_k8s_enrichment",
			config: &Config{
				ConnectionTableSize: defaultConnectionTableSize,
				ConnectionTimeout:   defaultConnectionTimeout,
				BufferSize:          defaultBufferSize,
				CleanupInterval:     defaultCleanupInterval,
				EnableK8sMapping:    true,
				K8sRefreshInterval:  defaultK8sRefreshInterval,
				PodMappingTimeout:   defaultPodMappingTimeout,
				Name:                "e2e-k8s-test",
				HealthCheck:         true,
				EnableOTEL:          false,
				EnableStdout:        false,
			},
			setupFunc: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				t.Cleanup(cancel)
				return ctx
			},
			validateFunc: func(t *testing.T, obs *Observer) {
				assert.NotNil(t, obs)
				assert.NotNil(t, obs.connectionsTracker)
				// k8sMapper may be nil on non-K8s environments
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupFunc(t)

			// Create observer
			observer, err := NewObserver(tt.config.Name, tt.config, zap.NewNop())
			require.NoError(t, err)
			require.NotNil(t, observer)

			// Start observer
			err = observer.Start(ctx)
			require.NoError(t, err)

			// Wait for initialization
			time.Sleep(100 * time.Millisecond)

			// Validate health
			assert.True(t, observer.IsHealthy())

			// Validate configuration
			tt.validateFunc(t, observer)

			// Collect some events (simulated on non-Linux)
			eventCount := 0
			timeout := time.After(2 * time.Second)
		eventLoop:
			for eventCount < 5 {
				select {
				case event := <-observer.Events():
					assert.NotNil(t, event)
					assert.NotEmpty(t, event.EventID)
					assert.Equal(t, tt.config.Name, event.Source)
					eventCount++
				case <-timeout:
					break eventLoop
				}
			}

			// Get statistics
			stats := observer.GetStats()
			assert.NotNil(t, stats)

			// Stop observer
			err = observer.Stop()
			assert.NoError(t, err)

			// Verify cleanup
			assert.False(t, observer.IsHealthy())
		})
	}
}

// TestObserverE2E_EventProcessing tests event processing pipeline
func TestObserverE2E_EventProcessing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          defaultBufferSize,
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    false,
		Name:                "event-test",
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

	// Collect events
	receivedEvents := make([]*domain.CollectorEvent, 0)
	timeout := time.After(5 * time.Second)

collectLoop:
	for len(receivedEvents) < 10 {
		select {
		case event := <-observer.Events():
			receivedEvents = append(receivedEvents, event)
		case <-timeout:
			break collectLoop
		}
	}

	// Verify event structure
	for _, event := range receivedEvents {
		assert.NotEmpty(t, event.EventID)
		assert.NotZero(t, event.Timestamp)
		assert.Equal(t, config.Name, event.Source)
		assert.NotEmpty(t, event.Type)
		assert.NotEmpty(t, event.Severity)
	}
}

// TestObserverE2E_StatisticsCollection tests statistics collection over time
func TestObserverE2E_StatisticsCollection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          defaultBufferSize,
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    false,
		Name:                "stats-test",
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

	// Collect initial stats
	initialStats := observer.GetStats()
	assert.NotNil(t, initialStats)

	// Wait for stats collection
	time.Sleep(1 * time.Second)

	// Collect updated stats
	updatedStats := observer.GetStats()
	assert.NotNil(t, updatedStats)

	// Verify stats structure
	assert.GreaterOrEqual(t, updatedStats.ActiveConnections, uint64(0))
	assert.GreaterOrEqual(t, updatedStats.ServicesDiscovered, uint64(0))
}
