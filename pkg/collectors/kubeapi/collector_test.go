package kubeapi

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestNewCollector(t *testing.T) {
	collector, err := NewCollector("test-kubeapi")
	require.NoError(t, err)

	assert.Equal(t, "test-kubeapi", collector.Name())
	assert.True(t, collector.IsHealthy())
}

func TestCollectorInterface(t *testing.T) {
	collector, err := NewCollector("test-kubeapi")
	require.NoError(t, err)

	// Verify it implements collectors.Collector
	var _ collectors.Collector = collector
}

func TestCollectorStartStop(t *testing.T) {
	collector, err := NewCollector("test-kubeapi")
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Should not be able to start twice
	err = collector.Start(ctx)
	assert.Error(t, err)

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

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
	assert.Equal(t, "test-kubeapi", event.Metadata["collector"])
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

	assert.Equal(t, "custom-kubeapi", collector.Name())
	assert.True(t, collector.IsHealthy())
}
