package kubeapi

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
)

func TestNewCollector(t *testing.T) {
	collector, err := NewCollector("test-kubeapi")
	require.NoError(t, err)

	assert.Equal(t, "kubeapi", collector.Name())
	// Don't check IsHealthy for unstarted collector
}

func TestCollectorInterface(t *testing.T) {
	collector, err := NewCollector("test-kubeapi")
	require.NoError(t, err)

	// Verify it implements collectors.Collector
	var _ collectors.Collector = collector
}

func TestCollectorStartStop(t *testing.T) {
	// Use the full New function to get proper K8s connection
	logger := zap.NewNop()
	config := DefaultConfig()
	collector, err := New(logger, config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Should not be able to start twice
	err = collector.Start(ctx)
	assert.Error(t, err)

	// Check health while running
	assert.True(t, collector.IsHealthy())

	// Stop collector
	err = collector.Stop()
	require.NoError(t, err)

	// Should not be healthy after stop
	assert.False(t, collector.IsHealthy())
}

func TestEventCreation(t *testing.T) {
	collector, err := NewCollector("test-kubeapi")
	require.NoError(t, err)

	// Test event creation
	event := collector.createEvent("test_event", map[string]interface{}{
		"resource": "pods",
		"action":   "ADDED",
		"name":     "test-pod",
	}, "", "")

	assert.Equal(t, "kubeapi", event.Type)
	assert.Equal(t, "kubeapi", event.Metadata["collector"])
	assert.Equal(t, "test_event", event.Metadata["event"])
	assert.NotNil(t, event.Data)
	assert.False(t, event.Timestamp.IsZero())
}

func TestNewCollectorFromCollectorConfig(t *testing.T) {
	config := collectors.CollectorConfig{
		BufferSize: 100,
		Labels: map[string]string{
			"test": "true",
			"name": "custom-kubeapi",
		},
	}

	collector, err := NewCollectorFromCollectorConfig(config)
	require.NoError(t, err)

	assert.Equal(t, "kubeapi", collector.Name())
	// Don't check IsHealthy for unstarted collector
}
