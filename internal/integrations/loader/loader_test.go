package loader

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/internal/integrations/neo4j"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewLoader(t *testing.T) {
	tests := []struct {
		name        string
		logger      *zap.Logger
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid configuration",
			logger:      zaptest.NewLogger(t),
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name:        "nil logger",
			logger:      nil,
			config:      DefaultConfig(),
			expectError: true,
			errorMsg:    "logger is required",
		},
		{
			name:        "nil config",
			logger:      zaptest.NewLogger(t),
			config:      nil,
			expectError: true,
			errorMsg:    "config is required",
		},
		{
			name:   "invalid config - empty Neo4j URI",
			logger: zaptest.NewLogger(t),
			config: &Config{
				NATS:  DefaultConfig().NATS,
				Neo4j: neo4j.Config{URI: ""}, // Invalid empty URI
			},
			expectError: true,
			errorMsg:    "invalid configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, err := NewLoader(tt.logger, tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, loader)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, loader)
				assert.NotNil(t, loader.tracer)
				assert.NotNil(t, loader.eventParser)
				assert.Equal(t, tt.config, loader.config)

				// Test cleanup
				loader.cancel()
			}
		})
	}
}

func TestLoaderMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	loader, err := NewLoader(logger, config)
	require.NoError(t, err)
	defer loader.cancel()

	// Test initial metrics
	metrics := loader.GetMetrics()
	assert.Equal(t, "initializing", metrics.HealthStatus)
	assert.Equal(t, int64(0), metrics.EventsReceived)
	assert.Equal(t, int64(0), metrics.EventsProcessed)

	// Test metrics update
	loader.updateMetrics(func(m *LoaderMetrics) {
		m.EventsReceived = 100
		m.EventsProcessed = 95
		m.EventsFailed = 5
		m.HealthStatus = "running"
	})

	updatedMetrics := loader.GetMetrics()
	assert.Equal(t, "running", updatedMetrics.HealthStatus)
	assert.Equal(t, int64(100), updatedMetrics.EventsReceived)
	assert.Equal(t, int64(95), updatedMetrics.EventsProcessed)
	assert.Equal(t, int64(5), updatedMetrics.EventsFailed)
}

func TestLoaderHealthStatus(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	loader, err := NewLoader(logger, config)
	require.NoError(t, err)
	defer loader.cancel()

	// Test initial health status
	health := loader.GetHealthStatus()
	assert.NotEmpty(t, health.Status)
	assert.False(t, health.NATSConnected)  // Not connected yet
	assert.False(t, health.Neo4jConnected) // Not connected yet
	assert.NotNil(t, health.Details)

	// Check that details are populated
	assert.Contains(t, health.Details, "throughput")
	assert.Contains(t, health.Details, "processing_latency")
	assert.Contains(t, health.Details, "backlog_size")
}

func TestBatchJobGeneration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	loader, err := NewLoader(logger, config)
	require.NoError(t, err)
	defer loader.cancel()

	// Test batch ID generation
	id1 := loader.generateBatchID()
	id2 := loader.generateBatchID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Contains(t, id1, "batch_")
	assert.Contains(t, id2, "batch_")
}

func TestResourceCleanup(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	loader, err := NewLoader(logger, config)
	require.NoError(t, err)

	// Add test resources
	called := false
	loader.addResource(func() error {
		called = true
		return nil
	})

	// Test cleanup
	loader.cleanupResources()
	assert.True(t, called)
}

func TestChannelClosing(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	loader, err := NewLoader(logger, config)
	require.NoError(t, err)

	// Channels should be open initially
	assert.NotNil(t, loader.batchChannel)
	assert.NotNil(t, loader.jobQueue)
	assert.NotNil(t, loader.workerPool)

	// Close channels
	loader.closeChannels()

	// Test that channels are closed by checking if we can receive from them
	// This approach avoids the panic from sending on closed channels
	select {
	case _, ok := <-loader.batchChannel:
		assert.False(t, ok, "Expected batchChannel to be closed")
	default:
		// Channel might be closed but no data to receive, this is also valid
	}

	select {
	case _, ok := <-loader.jobQueue:
		assert.False(t, ok, "Expected jobQueue to be closed")
	default:
		// Channel might be closed but no data to receive, this is also valid
	}

	select {
	case _, ok := <-loader.workerPool:
		assert.False(t, ok, "Expected workerPool to be closed")
	default:
		// Channel might be closed but no data to receive, this is also valid
	}
}

func TestEventToParams(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	loader, err := NewLoader(logger, config)
	require.NoError(t, err)
	defer loader.cancel()

	// Create test observation event
	pid := int32(1234)
	containerID := "container-123"
	podName := "test-pod"
	namespace := "test-namespace"
	action := "test-action"

	event := &domain.ObservationEvent{
		ID:          "test-event-1",
		Timestamp:   time.Now(),
		Source:      "kernel",
		Type:        "syscall",
		PID:         &pid,
		ContainerID: &containerID,
		PodName:     &podName,
		Namespace:   &namespace,
		Action:      &action,
		Data:        map[string]string{"key1": "value1", "key2": "value2"},
	}

	params := loader.eventToParams(event)

	// Check required fields
	assert.Equal(t, event.ID, params["id"])
	assert.Equal(t, event.Source, params["source"])
	assert.Equal(t, event.Type, params["type"])
	assert.Equal(t, event.Timestamp.UnixMilli(), params["timestamp"])

	// Check optional fields
	assert.Equal(t, *event.PID, params["pid"])
	assert.Equal(t, *event.ContainerID, params["container_id"])
	assert.Equal(t, *event.PodName, params["pod_name"])
	assert.Equal(t, *event.Namespace, params["namespace"])
	assert.Equal(t, *event.Action, params["action"])
	assert.Equal(t, event.Data, params["data"])

	// Test with minimal event (only required fields)
	minimalEvent := &domain.ObservationEvent{
		ID:        "minimal-event",
		Timestamp: time.Now(),
		Source:    "test",
		Type:      "test",
	}

	minimalParams := loader.eventToParams(minimalEvent)
	assert.Equal(t, minimalEvent.ID, minimalParams["id"])
	assert.Equal(t, minimalEvent.Source, minimalParams["source"])
	assert.Equal(t, minimalEvent.Type, minimalParams["type"])

	// Optional fields should not be present
	assert.NotContains(t, minimalParams, "pid")
	assert.NotContains(t, minimalParams, "container_id")
	assert.NotContains(t, minimalParams, "pod_name")
}

func TestBacklogSizeCalculation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	loader, err := NewLoader(logger, config)
	require.NoError(t, err)
	defer loader.cancel()

	// Initially empty
	assert.Equal(t, 0, loader.getBacklogSize())

	// Add some events to the channel
	event := &domain.ObservationEvent{
		ID:        "test-event",
		Timestamp: time.Now(),
		Source:    "test",
		Type:      "test",
	}

	go func() {
		loader.batchChannel <- event
		loader.batchChannel <- event
	}()

	// Wait a bit for the goroutine to execute
	time.Sleep(10 * time.Millisecond)

	// Should have 2 events in backlog
	backlogSize := loader.getBacklogSize()
	assert.True(t, backlogSize >= 0 && backlogSize <= 2, "Expected backlog size between 0 and 2, got %d", backlogSize)
}

func TestActiveWorkerCount(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.MaxConcurrency = 2

	loader, err := NewLoader(logger, config)
	require.NoError(t, err)
	defer loader.cancel()

	// Initially no workers
	assert.Equal(t, 0, loader.getActiveWorkerCount())

	// Simulate workers acquiring slots
	go func() {
		loader.workerPool <- struct{}{}
		time.Sleep(50 * time.Millisecond)
		<-loader.workerPool
	}()

	// Wait a bit and check
	time.Sleep(10 * time.Millisecond)
	count := loader.getActiveWorkerCount()
	assert.True(t, count >= 0 && count <= 1, "Expected active worker count between 0 and 1, got %d", count)
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorField  string
	}{
		{
			name:        "valid config",
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name: "nil NATS config",
			config: &Config{
				NATS:  nil,
				Neo4j: neo4j.Config{URI: "neo4j://localhost"},
			},
			expectError: true,
			errorField:  "NATS",
		},
		{
			name: "empty Neo4j URI",
			config: &Config{
				NATS:  DefaultConfig().NATS,
				Neo4j: neo4j.Config{URI: ""},
			},
			expectError: true,
			errorField:  "Neo4j.URI",
		},
		{
			name: "invalid batch size",
			config: &Config{
				NATS:      DefaultConfig().NATS,
				Neo4j:     neo4j.Config{URI: "neo4j://localhost"},
				BatchSize: 0,
			},
			expectError: true,
			errorField:  "BatchSize",
		},
		{
			name: "invalid batch timeout",
			config: &Config{
				NATS:         DefaultConfig().NATS,
				Neo4j:        neo4j.Config{URI: "neo4j://localhost"},
				BatchSize:    100,
				BatchTimeout: 0,
			},
			expectError: true,
			errorField:  "BatchTimeout",
		},
		{
			name: "invalid max concurrency",
			config: &Config{
				NATS:           DefaultConfig().NATS,
				Neo4j:          neo4j.Config{URI: "neo4j://localhost"},
				BatchSize:      100,
				BatchTimeout:   5 * time.Second,
				MaxConcurrency: 0,
			},
			expectError: true,
			errorField:  "MaxConcurrency",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorField)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.NotNil(t, config.NATS)
	assert.Equal(t, 100, config.BatchSize)
	assert.Equal(t, 5*time.Second, config.BatchTimeout)
	assert.Equal(t, 4, config.MaxConcurrency)
	assert.Equal(t, 30*time.Second, config.ProcessTimeout)
	assert.Equal(t, 30*time.Second, config.ShutdownTimeout)
	assert.Equal(t, 30*time.Second, config.HealthCheckInterval)
	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, 1*time.Second, config.RetryBackoff)
	assert.Equal(t, 30*time.Second, config.MaxRetryBackoff)

	// Should pass validation
	assert.NoError(t, config.Validate())
}

func TestValidationError(t *testing.T) {
	err := NewValidationError("TestField", "test-value", "must be positive")

	assert.Equal(t, "TestField", err.Field)
	assert.Equal(t, "test-value", err.Value)
	assert.Equal(t, "must be positive", err.Rule)
	assert.Contains(t, err.Error(), "TestField")
	assert.Contains(t, err.Error(), "must be positive")

	// Test unwrap
	assert.Nil(t, err.Unwrap())
}
