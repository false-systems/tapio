package cni

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

	assert.Equal(t, "cni", collector.Name())
	assert.True(t, collector.IsHealthy())
}

func TestCollectorStartStop(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	config.BufferSize = 10

	collector, err := NewCollector(config)
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

	// Events channel should be closed
	_, ok := <-collector.Events()
	assert.False(t, ok)
}

func TestCNIStrategyImplementations(t *testing.T) {
	tests := []struct {
		name     string
		strategy CNIStrategy
		wantName string
	}{
		{
			name:     "Calico strategy",
			strategy: &CalicoStrategy{},
			wantName: "calico",
		},
		{
			name:     "Cilium strategy",
			strategy: &CiliumStrategy{},
			wantName: "cilium",
		},
		{
			name:     "Flannel strategy",
			strategy: &FlannelStrategy{},
			wantName: "flannel",
		},
		{
			name:     "Generic strategy",
			strategy: &GenericStrategy{},
			wantName: "generic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantName, tt.strategy.GetName())
			assert.NotEmpty(t, tt.strategy.GetLogPaths())
			assert.NotEmpty(t, tt.strategy.GetWatchPaths())
		})
	}
}

func TestCollectorHealthy(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	// Should be healthy initially
	assert.True(t, collector.IsHealthy())

	// Start and stop should maintain health
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	err = collector.Stop()
	require.NoError(t, err)
	assert.True(t, collector.IsHealthy())
}
