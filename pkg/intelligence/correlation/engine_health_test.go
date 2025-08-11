package correlation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestEngine_HealthCheck(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("healthy engine", func(t *testing.T) {
		config := *DefaultEngineConfig()
		config.StorageWorkerCount = 2

		// Create engine with memory storage
		storage := NewMemoryStorage(logger)
		engine, err := NewEngine(logger, config, nil, storage)
		require.NoError(t, err)
		require.NotNil(t, engine)

		// Start engine
		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)

		// Health check should pass
		err = engine.HealthCheck(ctx)
		assert.NoError(t, err)

		// Stop engine
		err = engine.Stop()
		assert.NoError(t, err)
	})

	t.Run("unhealthy storage", func(t *testing.T) {
		config := *DefaultEngineConfig()
		config.StorageWorkerCount = 2

		// Create mock storage that fails health check
		mockStorage := &MockStorage{}
		mockStorage.On("HealthCheck", context.Background()).Return(fmt.Errorf("storage connection failed"))

		engine, err := NewEngine(logger, config, nil, mockStorage)
		require.NoError(t, err)
		require.NotNil(t, engine)

		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)

		// Health check should fail due to storage
		err = engine.HealthCheck(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "storage health check failed")

		err = engine.Stop()
		assert.NoError(t, err)
	})

	t.Run("stopped engine", func(t *testing.T) {
		config := *DefaultEngineConfig()
		config.StorageWorkerCount = 2

		storage := NewMemoryStorage(logger)
		engine, err := NewEngine(logger, config, nil, storage)
		require.NoError(t, err)

		// Start then stop engine
		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)
		err = engine.Stop()
		require.NoError(t, err)

		// Health check should fail on stopped engine
		err = engine.HealthCheck(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "engine is not running")
	})

	t.Run("queue near capacity", func(t *testing.T) {
		// Create config with small queues
		config := *DefaultEngineConfig()
		config.EventBufferSize = 10
		config.ResultBufferSize = 5
		config.StorageWorkerCount = 1

		storage := NewMemoryStorage(logger)
		engine, err := NewEngine(logger, config, nil, storage)
		require.NoError(t, err)

		ctx := context.Background()
		err = engine.Start(ctx)
		require.NoError(t, err)

		// Fill result queue to near capacity (90%+)
		for i := 0; i < 5; i++ {
			select {
			case engine.resultChan <- &CorrelationResult{
				ID:   fmt.Sprintf("test-%d", i),
				Type: "test",
			}:
			default:
				break
			}
		}

		// Health check should fail due to queue overflow
		err = engine.HealthCheck(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "queue near capacity")

		err = engine.Stop()
		assert.NoError(t, err)
	})
}

func TestEngine_IsHealthy(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := *DefaultEngineConfig()

	storage := NewMemoryStorage(logger)
	engine, err := NewEngine(logger, config, nil, storage)
	require.NoError(t, err)

	// Engine should be healthy after creation
	assert.True(t, engine.IsHealthy())

	// Start engine
	ctx := context.Background()
	err = engine.Start(ctx)
	require.NoError(t, err)
	assert.True(t, engine.IsHealthy())

	// Stop engine
	err = engine.Stop()
	assert.NoError(t, err)

	// Engine should be unhealthy after stop
	assert.False(t, engine.IsHealthy())
}

func TestEngine_GetHealthStatus(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := *DefaultEngineConfig()
	config.EnabledCorrelators = []string{"temporal"}
	config.StorageWorkerCount = 5

	storage := NewMemoryStorage(logger)
	engine, err := NewEngine(logger, config, nil, storage)
	require.NoError(t, err)

	ctx := context.Background()
	err = engine.Start(ctx)
	require.NoError(t, err)

	// Get health status
	status := engine.GetHealthStatus(ctx)

	// Verify basic health status
	assert.True(t, status.IsHealthy)
	assert.Equal(t, "correlation-engine", status.Component)
	assert.Equal(t, "1.0.0", status.Version)
	assert.WithinDuration(t, time.Now(), status.Timestamp, 1*time.Second)

	// Verify dependencies
	assert.Contains(t, status.Dependencies, "storage")
	storageHealth := status.Dependencies["storage"]
	assert.Equal(t, "correlation-storage", storageHealth.Name)
	assert.True(t, storageHealth.IsHealthy)

	assert.Contains(t, status.Dependencies, "correlator-temporal")
	correlatorHealth := status.Dependencies["correlator-temporal"]
	assert.Equal(t, "temporal", correlatorHealth.Name)
	assert.True(t, correlatorHealth.IsHealthy)

	// Verify queue health
	assert.Equal(t, 0, status.QueueHealth.EventQueue.Size)
	assert.Equal(t, config.EventBufferSize, status.QueueHealth.EventQueue.Capacity)
	assert.Equal(t, 0.0, status.QueueHealth.EventQueue.Usage)

	assert.Equal(t, 0, status.QueueHealth.ResultQueue.Size)
	assert.Equal(t, config.ResultBufferSize, status.QueueHealth.ResultQueue.Capacity)
	assert.Equal(t, 0.0, status.QueueHealth.ResultQueue.Usage)

	// Verify storage queue is present
	require.NotNil(t, status.QueueHealth.StorageQueue)
	assert.Equal(t, 0, status.QueueHealth.StorageQueue.Size)

	err = engine.Stop()
	assert.NoError(t, err)
}

func TestEngine_GetHealthStatus_WithFailures(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := *DefaultEngineConfig()

	// Create mock storage that fails health check
	mockStorage := &MockStorage{}
	mockStorage.On("HealthCheck", context.Background()).Return(fmt.Errorf("connection lost"))

	engine, err := NewEngine(logger, config, nil, mockStorage)
	require.NoError(t, err)

	ctx := context.Background()
	err = engine.Start(ctx)
	require.NoError(t, err)

	// Get health status
	status := engine.GetHealthStatus(ctx)

	// Overall health should be false due to storage failure
	assert.False(t, status.IsHealthy)

	// Storage dependency should be unhealthy
	assert.Contains(t, status.Dependencies, "storage")
	storageHealth := status.Dependencies["storage"]
	assert.False(t, storageHealth.IsHealthy)
	assert.Equal(t, "connection lost", storageHealth.Message)

	err = engine.Stop()
	assert.NoError(t, err)
}
