package cni

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestNewCollector(t *testing.T) {
	collector, err := NewCollector("test-cni")
	require.NoError(t, err)

	assert.Equal(t, "test-cni", collector.Name())
	assert.True(t, collector.IsHealthy())
}

func TestCollectorInterface(t *testing.T) {
	collector, err := NewCollector("test-cni")
	require.NoError(t, err)

	// Verify it implements collectors.Collector
	var _ collectors.Collector = collector
}

func TestCollectorStartStop(t *testing.T) {
	collector, err := NewCollector("test-cni")
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
	collector, err := NewCollector("test-cni")
	require.NoError(t, err)

	// Test event creation
	event := collector.createEvent("test_event", map[string]interface{}{
		"key": "value",
		"num": 123,
	})

	assert.Equal(t, "cni", event.Type)
	assert.Equal(t, "test-cni", event.Metadata["collector"])
	assert.Equal(t, "test_event", event.Metadata["event"])
	assert.NotNil(t, event.Data)
	assert.False(t, event.Timestamp.IsZero())
}
