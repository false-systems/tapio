package correlation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestCLAUDECompliance verifies that the code complies with CLAUDE.md requirements
func TestCLAUDECompliance(t *testing.T) {
	t.Run("No map[string]interface{} in public APIs", func(t *testing.T) {
		// Test that Engine.GetMetrics returns MetricsData, not map[string]interface{}
		logger := zap.NewNop()
		config := DefaultEngineConfig()
		// Storage can be nil for this test
		var storage Storage

		engine, err := NewEngine(logger, *config, nil, storage)
		require.NoError(t, err)

		// This should compile - GetMetrics returns MetricsData
		metrics := engine.GetMetrics()

		// Verify it's the correct type
		assert.IsType(t, MetricsData{}, metrics)
		assert.NotNil(t, metrics)

		// Verify the fields are accessible with proper types
		_ = metrics.EventsProcessed   // int64
		_ = metrics.CorrelationsFound // int64
		_ = metrics.EventQueueSize    // int
		_ = metrics.ResultQueueSize   // int
		_ = metrics.CorrelatorsCount  // int
		_ = metrics.WorkersCount      // int
		_ = metrics.IsHealthy         // bool
		_ = metrics.Status            // string
	})

	t.Run("Query configuration with limits", func(t *testing.T) {
		// Test default query configuration
		config := DefaultQueryConfig()

		assert.Equal(t, 100, config.DefaultLimit)
		assert.Equal(t, 1000, config.MaxLimit)
		assert.Equal(t, 100, config.ServiceQueryLimit)
		assert.Equal(t, 200, config.PodQueryLimit)
		assert.Equal(t, 50, config.ConfigQueryLimit)
		assert.Equal(t, 150, config.DependencyQueryLimit)
		assert.Equal(t, 100, config.OwnershipQueryLimit)

		// Test GetLimit method
		assert.Equal(t, 100, config.GetLimit("service"))
		assert.Equal(t, 200, config.GetLimit("pod"))
		assert.Equal(t, 50, config.GetLimit("config"))
		assert.Equal(t, 150, config.GetLimit("dependency"))
		assert.Equal(t, 100, config.GetLimit("ownership"))
		assert.Equal(t, 100, config.GetLimit("unknown"))

		// Test ValidateLimit method
		assert.Equal(t, 100, config.ValidateLimit(0))     // Returns default for 0
		assert.Equal(t, 100, config.ValidateLimit(-1))    // Returns default for negative
		assert.Equal(t, 500, config.ValidateLimit(500))   // Returns value within bounds
		assert.Equal(t, 1000, config.ValidateLimit(2000)) // Caps at max limit
	})

	t.Run("DependencyCorrelator uses QueryConfig", func(t *testing.T) {
		mockStore := &MockGraphStore{}
		logger := zap.NewNop()

		correlator, err := NewDependencyCorrelator(mockStore, logger)
		require.NoError(t, err)
		require.NotNil(t, correlator)

		// Verify the correlator has a query config
		assert.NotNil(t, correlator.queryConfig)
		assert.Equal(t, 100, correlator.queryConfig.DefaultLimit)
	})

	t.Run("OwnershipCorrelator uses QueryConfig", func(t *testing.T) {
		mockStore := &MockGraphStore{}
		logger := zap.NewNop()

		correlator, err := NewOwnershipCorrelator(mockStore, logger)
		require.NoError(t, err)
		require.NotNil(t, correlator)

		// Verify the correlator has a query config
		assert.NotNil(t, correlator.queryConfig)
		assert.Equal(t, 100, correlator.queryConfig.DefaultLimit)
	})

	t.Run("ConfigImpactCorrelator uses QueryConfig", func(t *testing.T) {
		mockStore := &MockGraphStore{}
		logger := zap.NewNop()

		correlator, err := NewConfigImpactCorrelator(mockStore, logger)
		require.NoError(t, err)
		require.NotNil(t, correlator)

		// Verify the correlator has a query config
		assert.NotNil(t, correlator.queryConfig)
		assert.Equal(t, 100, correlator.queryConfig.DefaultLimit)
	})

	t.Run("GraphStore interface uses typed parameters", func(t *testing.T) {
		// This test verifies that GraphStore methods use QueryParams interface
		// instead of map[string]interface{}
		mockStore := &MockGraphStore{}
		ctx := context.Background()

		// Create typed query parameters
		params := &ServiceQueryParams{
			BaseQueryParams: BaseQueryParams{
				Namespace: "test-namespace",
				Cluster:   "test-cluster",
			},
			ServiceName: "test-service",
		}

		// This should compile - ExecuteQuery accepts QueryParams, not map[string]interface{}
		mockStore.On("ExecuteQuery", ctx, "test query", params).Return(nil, nil)

		// Execute the query with typed parameters
		_, err := mockStore.ExecuteQuery(ctx, "test query", params)
		assert.NoError(t, err)
	})
}
