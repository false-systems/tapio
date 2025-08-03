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
\tlogger := zap.NewNop()
\tconfig := DefaultConfig()
\tcollector, err := New(logger, config)
\trequire.NoError(t, err)

\t// Test with nil object
\trels := collector.extractRelationships(nil)
\tassert.Empty(t, rels)

\t// More comprehensive relationship tests would require mock K8s objects
}

func TestShouldIgnoreNamespace(t *testing.T) {
\tlogger := zap.NewNop()
\t
\ttests := []struct {
\t\tname      string
\t\tconfig    Config
\t\tnamespace string
\t\texpected  bool
\t}{
\t\t{
\t\t\tname:      "no filters",
\t\t\tconfig:    DefaultConfig(),
\t\t\tnamespace: "default",
\t\t\texpected:  false,
\t\t},
\t\t{
\t\t\tname: "in watch list",
\t\t\tconfig: Config{
\t\t\t\tWatchNamespaces: []string{"default", "kube-system"},
\t\t\t},
\t\t\tnamespace: "default",
\t\t\texpected:  false,
\t\t},
\t\t{
\t\t\tname: "not in watch list",
\t\t\tconfig: Config{
\t\t\t\tWatchNamespaces: []string{"default"},
\t\t\t},
\t\t\tnamespace: "kube-system",
\t\t\texpected:  true,
\t\t},
\t\t{
\t\t\tname: "in ignore list",
\t\t\tconfig: Config{
\t\t\t\tIgnoreNamespaces: []string{"kube-system"},
\t\t\t},
\t\t\tnamespace: "kube-system",
\t\t\texpected:  true,
\t\t},
\t}

\tfor _, tt := range tests {
\t\tt.Run(tt.name, func(t *testing.T) {
\t\t\tcollector, err := New(logger, tt.config)
\t\t\trequire.NoError(t, err)
\t\t\tresult := collector.shouldIgnoreNamespace(tt.namespace)
\t\t\tassert.Equal(t, tt.expected, result)
\t\t})
\t}
}

func TestResourceEventHandler(t *testing.T) {
\tlogger := zap.NewNop()
\tconfig := DefaultConfig()
\tcollector, err := New(logger, config)
\trequire.NoError(t, err)

\t// Get resource event handler
\thandler := collector.resourceEventHandler("Pod")
\tassert.NotNil(t, handler)
\t
\t// Handler should have OnAdd, OnUpdate, OnDelete methods
\t_, hasOnAdd := handler.(interface{ OnAdd(obj interface{}) })
\t_, hasOnUpdate := handler.(interface{ OnUpdate(oldObj, newObj interface{}) })
\t_, hasOnDelete := handler.(interface{ OnDelete(obj interface{}) })
\t
\tassert.True(t, hasOnAdd)
\tassert.True(t, hasOnUpdate)
\tassert.True(t, hasOnDelete)
}
