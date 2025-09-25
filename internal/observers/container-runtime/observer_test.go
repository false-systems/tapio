package containerruntime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewObserver(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "Default config",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "Custom config",
			config:  NewDefaultConfig("test-observer"),
			wantErr: false,
		},
		{
			name: "Invalid config - empty name",
			config: &Config{
				BufferSize:      1000,
				MetricsEnabled:  true,
				EnableOOMKill:   true,
				FlushInterval:   30 * time.Second,
				MaxEventsPerSec: 1000,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := NewObserver("container-runtime", tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, observer)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, observer)
				if observer != nil {
					assert.Equal(t, "container-runtime", observer.Name())
					// Cleanup
					observer.Stop()
				}
			}
		})
	}
}

func TestObserver_StartStop(t *testing.T) {
	observer, err := NewObserver("test", nil)
	require.NoError(t, err)
	require.NotNil(t, observer)

	ctx := context.Background()

	// Start the observer
	err = observer.Start(ctx)
	assert.NoError(t, err)

	// Check it's healthy
	assert.True(t, observer.IsHealthy())

	// Stop the observer
	err = observer.Stop()
	assert.NoError(t, err)
}

func TestObserver_Events(t *testing.T) {
	observer, err := NewObserver("test", nil)
	require.NoError(t, err)
	require.NotNil(t, observer)

	// Get event channel
	events := observer.Events()
	assert.NotNil(t, events)

	// Should be able to send events through the channel manager
	testEvent := &domain.CollectorEvent{
		EventID:   "test-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeContainerOOM,
		Source:    "test",
		Severity:  domain.EventSeverityError,
	}

	sent := observer.SendEvent(testEvent)
	assert.True(t, sent)

	// Should receive the event
	select {
	case received := <-events:
		assert.Equal(t, testEvent.EventID, received.EventID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for event")
	}
}

func TestObserver_Statistics(t *testing.T) {
	observer, err := NewObserver("test", nil)
	require.NoError(t, err)
	require.NotNil(t, observer)

	stats := observer.Statistics()
	assert.NotNil(t, stats)
	assert.Equal(t, int64(0), stats.EventsProcessed)
	assert.Equal(t, int64(0), stats.ErrorCount)
}

func TestObserver_Health(t *testing.T) {
	observer, err := NewObserver("test", nil)
	require.NoError(t, err)
	require.NotNil(t, observer)

	health := observer.Health()
	assert.NotNil(t, health)
	assert.Equal(t, domain.HealthHealthy, health.Status)
}
