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

	// Test with nil object
	rels := collector.extractRelationships(nil)
	assert.Empty(t, rels)

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

	// Handler should have OnAdd, OnUpdate, OnDelete methods
	_, hasOnAdd := handler.(interface{ OnAdd(obj interface{}) })
	_, hasOnUpdate := handler.(interface {
		OnUpdate(oldObj, newObj interface{})
	})
	_, hasOnDelete := handler.(interface{ OnDelete(obj interface{}) })

	assert.True(t, hasOnAdd)
	assert.True(t, hasOnUpdate)
	assert.True(t, hasOnDelete)
}
