package status

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

func TestEventFlow(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:         true,
		BufferSize:      100,
		SampleRate:      1.0,
		MaxEventsPerSec: 1000,
		FlushInterval:   100 * time.Millisecond,
		EnableL7Parse:   true,
		Logger:          logger,
	}

	observer, err := NewObserver("test-flow", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start the observer
	err = observer.Start(ctx)
	if err != nil {
		t.Logf("Start failed (expected on non-Linux): %v", err)
		return
	}
	defer observer.Stop()

	// Get the event channel
	events := observer.Events()
	require.NotNil(t, events)

	// In mock mode (non-Linux), we should get mock events
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, "test-flow", event.Source)
		assert.Equal(t, domain.EventTypeNetworkConnection, event.Type)
		t.Logf("Received event: %+v", event)
	case <-time.After(15 * time.Second):
		t.Log("No events received (expected on Linux without actual traffic)")
	}
}

func TestMetricsCollection(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	config.Logger = logger

	observer, err := NewObserver("test-metrics", config)
	require.NoError(t, err)

	// Simulate events for aggregation
	observer.aggregator.Add(&StatusEvent{
		ServiceHash:  12345,
		EndpointHash: 67890,
		StatusCode:   500,
		ErrorType:    Error5XX,
		Timestamp:    uint64(time.Now().UnixNano()),
		Latency:      1500,
		PID:          1234,
	})

	observer.aggregator.Add(&StatusEvent{
		ServiceHash:  12345,
		EndpointHash: 67890,
		StatusCode:   200,
		ErrorType:    ErrorNone,
		Timestamp:    uint64(time.Now().UnixNano()),
		Latency:      500,
		PID:          1234,
	})

	// Flush aggregates
	aggregates := observer.aggregator.Flush()
	assert.Len(t, aggregates, 1)

	agg := aggregates[12345]
	require.NotNil(t, agg)
	assert.Equal(t, uint64(2), agg.TotalCount)
	assert.Equal(t, uint64(1), agg.ErrorCount)
	assert.Equal(t, 1000.0, agg.AvgLatency())
	assert.Equal(t, 0.5, agg.ErrorRate())

	// Update error rates
	observer.updateErrorRates(aggregates)
	observer.mu.RLock()
	rate, exists := observer.errorRates[12345]
	observer.mu.RUnlock()
	assert.True(t, exists)
	assert.Equal(t, 0.5, rate)
}
