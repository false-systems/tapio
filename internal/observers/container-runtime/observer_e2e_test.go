package containerruntime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// E2E test - full lifecycle test
func TestObserver_E2E_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Create observer with custom config
	config := NewDefaultConfig("e2e-test")
	config.EnableOOMKill = true
	config.EnableMemoryPressure = true
	config.EnableProcessExit = true
	config.FlushInterval = 100 * time.Millisecond

	observer, err := NewObserver("e2e", config)
	require.NoError(t, err)
	require.NotNil(t, observer)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start observer
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Verify health
	assert.True(t, observer.IsHealthy())
	health := observer.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)

	// Get event channel
	events := observer.Events()
	assert.NotNil(t, events)

	// Simulate different event types
	eventTypes := []domain.EventType{
		domain.EventTypeContainerOOM,
		domain.EventTypeContainerExit,
		domain.EventTypeMemoryPressure,
	}

	for _, eventType := range eventTypes {
		event := &domain.CollectorEvent{
			EventID:   "e2e-" + string(eventType),
			Timestamp: time.Now(),
			Type:      eventType,
			Source:    "e2e",
			Severity:  domain.EventSeverityError,
			EventData: domain.EventDataContainer{
				Container: &domain.ContainerData{
					ContainerID: "test-container-123",
					PodUID:      "pod-456",
					Namespace:   "default",
					PodName:     "test-pod",
				},
			},
			Metadata: domain.EventMetadata{
				Labels: map[string]string{
					"observer": "container-runtime",
					"version":  "1.0.0",
					"test":     "e2e",
				},
			},
		}

		// Send event
		sent := observer.SendEvent(event)
		assert.True(t, sent)

		// Receive and verify event
		select {
		case received := <-events:
			assert.Equal(t, event.EventID, received.EventID)
			assert.Equal(t, eventType, received.Type)
			assert.Equal(t, "e2e", received.Source)
		case <-time.After(1 * time.Second):
			t.Fatalf("Timeout waiting for event type %s", eventType)
		}
	}

	// Check statistics
	stats := observer.Statistics()
	assert.NotNil(t, stats)
	assert.GreaterOrEqual(t, stats.EventsProcessed, int64(len(eventTypes)))

	// Graceful shutdown
	err = observer.Stop()
	assert.NoError(t, err)
}

// Negative test - invalid configurations
func TestObserver_NegativeTests(t *testing.T) {
	tests := []struct {
		name        string
		setupConfig func() *Config
		wantErr     bool
		errContains string
	}{
		{
			name: "Negative buffer size",
			setupConfig: func() *Config {
				cfg := NewDefaultConfig("test")
				cfg.BufferSize = -1
				return cfg
			},
			wantErr:     true,
			errContains: "buffer size must be positive",
		},
		{
			name: "Zero buffer size",
			setupConfig: func() *Config {
				cfg := NewDefaultConfig("test")
				cfg.BufferSize = 0
				return cfg
			},
			wantErr:     true,
			errContains: "buffer size must be positive",
		},
		{
			name: "Invalid flush interval",
			setupConfig: func() *Config {
				cfg := NewDefaultConfig("test")
				cfg.FlushInterval = -1 * time.Second
				return cfg
			},
			wantErr:     true,
			errContains: "flush interval must be positive",
		},
		{
			name: "Negative max events per second",
			setupConfig: func() *Config {
				cfg := NewDefaultConfig("test")
				cfg.MaxEventsPerSec = -1
				return cfg
			},
			wantErr:     true,
			errContains: "max events per second must be positive",
		},
		{
			name: "Empty observer name",
			setupConfig: func() *Config {
				cfg := NewDefaultConfig("")
				return cfg
			},
			wantErr:     true,
			errContains: "name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.setupConfig()
			observer, err := NewObserver("test", cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, observer)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, observer)
				if observer != nil {
					observer.Stop()
				}
			}
		})
	}
}

// Test observer behavior with context cancellation
func TestObserver_ContextCancellation(t *testing.T) {
	observer, err := NewObserver("ctx-test", nil)
	require.NoError(t, err)
	require.NotNil(t, observer)

	ctx, cancel := context.WithCancel(context.Background())

	// Start observer
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Send an event
	event := &domain.CollectorEvent{
		EventID:   "ctx-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeContainerOOM,
		Source:    "ctx-test",
		Severity:  domain.EventSeverityError,
	}

	sent := observer.SendEvent(event)
	assert.True(t, sent)

	// Cancel context
	cancel()

	// Give it time to process cancellation
	time.Sleep(100 * time.Millisecond)

	// Observer should still be able to stop gracefully
	err = observer.Stop()
	assert.NoError(t, err)
}

// Test observer resilience to rapid start/stop cycles
func TestObserver_RapidStartStop(t *testing.T) {
	observer, err := NewObserver("rapid", nil)
	require.NoError(t, err)
	require.NotNil(t, observer)

	ctx := context.Background()

	for i := 0; i < 5; i++ {
		err := observer.Start(ctx)
		assert.NoError(t, err)

		// Send a test event
		event := &domain.CollectorEvent{
			EventID:   "rapid-" + string(i),
			Timestamp: time.Now(),
			Type:      domain.EventTypeContainerOOM,
			Source:    "rapid",
			Severity:  domain.EventSeverityError,
		}
		observer.SendEvent(event)

		err = observer.Stop()
		assert.NoError(t, err)
	}
}
