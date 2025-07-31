package etcd

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestCollector(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Verify basic properties
	assert.Equal(t, "etcd", collector.Name())
	assert.True(t, collector.IsHealthy())

	// Check that we can receive from the events channel
	eventsChan := collector.Events()
	assert.NotNil(t, eventsChan)
}

func TestCollectorStartStop(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	// Test double start
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	err = collector.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	// Test stop
	err = collector.Stop()
	assert.NoError(t, err)
}

func TestCollectorHealthCheck(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	// Should be healthy on creation
	assert.True(t, collector.IsHealthy())

	// Start and verify still healthy
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	assert.True(t, collector.IsHealthy())
}

func TestCollectorContextCancellation(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Cancel context
	cancel()

	// Give collector time to stop
	time.Sleep(100 * time.Millisecond)

	// Clean shutdown
	err = collector.Stop()
	assert.NoError(t, err)
}
