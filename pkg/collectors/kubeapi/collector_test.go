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
	logger := zap.NewNop()
	config := DefaultConfig()
	collector, err := New(logger, config)
	require.NoError(t, err)

	assert.Equal(t, "kubeapi", collector.Name())
	// Don't check IsHealthy for unstarted collector
}

func TestCollectorInterface(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	collector, err := New(logger, config)
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

func TestExtractRelationships(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	collector, err := New(logger, config)
	require.NoError(t, err)

	// Test with nil object - just verify collector is created properly
	assert.NotNil(t, collector)
	assert.Equal(t, "kubeapi", collector.Name())

	// More comprehensive relationship tests would require mock K8s objects
}

func TestShouldIgnoreNamespace(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name      string
		config    Config
		namespace string
		expected  bool
	}{
		{
			name:      "no filters",
			config:    DefaultConfig(),
			namespace: "default",
			expected:  false,
		},
		{
			name: "in watch list",
			config: Config{
				WatchNamespaces: []string{"default", "kube-system"},
			},
			namespace: "default",
			expected:  false,
		},
		{
			name: "not in watch list",
			config: Config{
				WatchNamespaces: []string{"default"},
			},
			namespace: "kube-system",
			expected:  true,
		},
		{
			name: "in ignore list",
			config: Config{
				IgnoreNamespaces: []string{"kube-system"},
			},
			namespace: "kube-system",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := New(logger, tt.config)
			require.NoError(t, err)
			result := collector.shouldIgnoreNamespace(tt.namespace)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResourceEventHandler(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	collector, err := New(logger, config)
	require.NoError(t, err)

	// Get resource event handler
	handler := collector.resourceEventHandler("Pod")
	assert.NotNil(t, handler)

	// Handler should be a ResourceEventHandlerFuncs with AddFunc, UpdateFunc, DeleteFunc
	assert.NotNil(t, handler.AddFunc)
	assert.NotNil(t, handler.UpdateFunc)
	assert.NotNil(t, handler.DeleteFunc)
}

func TestContextCancellation(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	collector, err := New(logger, config)
	require.NoError(t, err)

	// Create a context that we can cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Start the collector in a goroutine since it may hang on K8s connection
	startErr := make(chan error, 1)
	go func() {
		startErr <- collector.Start(ctx)
	}()

	// Give it a moment to try to start
	select {
	case err := <-startErr:
		if err != nil {
			t.Logf("Collector failed to start (expected in CI/test env): %v", err)
			// Even if start fails, we can test context propagation
		}
	case <-time.After(100 * time.Millisecond):
		// Collector might be trying to connect, that's OK
	}

	// Cancel the context
	cancel()

	// Give it time to process cancellation
	time.Sleep(50 * time.Millisecond)

	// Verify collector recognizes cancellation
	assert.False(t, collector.IsHealthy(), "Collector should not be healthy after context cancellation")

	// Stop should complete without error
	err = collector.Stop()
	require.NoError(t, err)
}

func TestContextPropagationToWatchers(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	collector, err := New(logger, config)
	require.NoError(t, err)

	// Ensure collector has context set up properly
	ctx, cancel := context.WithCancel(context.Background())
	collector.ctx, collector.cancel = context.WithCancel(ctx)

	// Verify context is set
	assert.NotNil(t, collector.ctx, "Collector context should be set")

	// Test that setupWatchers doesn't panic when using the context
	// Note: This will fail due to no K8s connection, but should not panic
	err = collector.setupWatchers()
	if err != nil {
		t.Logf("setupWatchers failed as expected in test environment: %v", err)
		// This is expected in test environment without K8s
	}

	// Cancel context and verify
	cancel()

	select {
	case <-collector.ctx.Done():
		// Context properly cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context should have been cancelled")
	}

	collector.Stop()
}
