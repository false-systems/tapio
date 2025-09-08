package resourcestarvation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCollectorWithBase(t *testing.T) {
	t.Run("base functionality", func(t *testing.T) {
		logger := zap.NewNop()
		config := NewDefaultConfig()

		collector, err := NewCollector(config, logger)
		require.NoError(t, err)

		// Test that base methods are available
		assert.True(t, collector.IsHealthy())
		assert.Equal(t, "resource-starvation", collector.Name())

		// Test statistics
		stats := collector.Statistics()
		assert.Equal(t, int64(0), stats.EventsProcessed)
		assert.Equal(t, int64(0), stats.ErrorCount)

		// Test health
		health := collector.Health()
		assert.Equal(t, "healthy", string(health.Status))

		// Test event channel
		eventCh := collector.Events()
		assert.NotNil(t, eventCh)
	})

	t.Run("event processing", func(t *testing.T) {
		logger := zap.NewNop()
		config := NewDefaultConfig()
		config.EventChannelSize = 10

		collector, err := NewCollector(config, logger)
		require.NoError(t, err)

		ctx := context.Background()

		// Create a test event
		event := &StarvationEvent{
			EventType:  uint32(EventSchedWait),
			Timestamp:  uint64(time.Now().UnixNano()),
			VictimPID:  1234,
			VictimTGID: 1234,
			VictimPrio: 120,
			WaitTimeNS: 50_000_000,  // 50ms
			RunTimeNS:  100_000_000, // 100ms
			CPUCore:    0,
		}

		// Process the event
		err = collector.ProcessEvent(ctx, event)
		require.NoError(t, err)

		// Check statistics were updated
		stats := collector.Statistics()
		assert.Equal(t, int64(1), stats.EventsProcessed)

		// Check event was sent
		select {
		case receivedEvent := <-collector.Events():
			assert.NotNil(t, receivedEvent)
			assert.Contains(t, receivedEvent.EventID, "starvation-1234")
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Did not receive event")
		}
	})

	t.Run("lifecycle management", func(t *testing.T) {
		logger := zap.NewNop()
		config := NewDefaultConfig()

		collector, err := NewCollector(config, logger)
		require.NoError(t, err)

		ctx := context.Background()

		// Start collector
		err = collector.Start(ctx)
		require.NoError(t, err)

		// Verify it's healthy
		assert.True(t, collector.IsHealthy())

		// Stop collector
		err = collector.Stop()
		require.NoError(t, err)

		// Verify it's no longer healthy
		assert.False(t, collector.IsHealthy())
	})
}
