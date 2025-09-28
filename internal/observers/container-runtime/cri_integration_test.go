package containerruntime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

func TestNewCRIIntegration(t *testing.T) {
	// Create observer
	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("test")
	observer, err := NewObserver("test", config)
	require.NoError(t, err)
	observer.logger = logger

	integration := NewCRIIntegration(observer)
	assert.NotNil(t, integration)
	assert.NotNil(t, integration.ebpfObserver)
	assert.NotNil(t, integration.logger)
}

func TestHandleCRIEvent(t *testing.T) {
	// Create integration
	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("test")
	observer, err := NewObserver("test", config)
	require.NoError(t, err)
	observer.logger = logger
	observer.containerCache = make(map[string]*ContainerMetadata)

	integration := NewCRIIntegration(observer)
	ctx := context.Background()

	t.Run("Container start event", func(t *testing.T) {
		event := &domain.CollectorEvent{
			EventID:   "test-1",
			Timestamp: time.Now(),
			Type:      domain.EventTypeContainerStart,
			EventData: domain.EventDataContainer{
				Container: &domain.ContainerData{
					ContainerID: "container-123",
					ImageName:   "nginx:latest",
					Runtime:     "docker",
					Labels: map[string]string{
						"app":                          "test",
						"io.kubernetes.container.name": "test-app",
						"io.kubernetes.pod.name":       "test-pod",
						"io.kubernetes.pod.uid":        "pod-uid-123",
						"io.kubernetes.pod.namespace":  "default",
					},
					Annotations: map[string]string{
						"version": "1.0",
					},
				},
			},
		}

		err := integration.HandleCRIEvent(ctx, event)
		assert.NoError(t, err)

		// Check container was cached
		metadata, exists := observer.containerCache["container-123"]
		assert.True(t, exists)
		assert.Equal(t, "test-app", metadata.ContainerName)
		assert.Equal(t, "nginx:latest", metadata.ImageName)
		assert.Equal(t, "test-pod", metadata.PodName)
		assert.Equal(t, "pod-uid-123", metadata.PodUID)
		assert.Equal(t, "default", metadata.Namespace)
	})

	t.Run("Container create event", func(t *testing.T) {
		event := &domain.CollectorEvent{
			EventID:   "test-2",
			Timestamp: time.Now(),
			Type:      domain.EventTypeContainerCreate,
			EventData: domain.EventDataContainer{
				Container: &domain.ContainerData{
					ContainerID: "container-456",
					ImageName:   "redis:latest",
					Labels: map[string]string{
						"io.kubernetes.container.name": "test-app-2",
					},
				},
			},
		}

		err := integration.HandleCRIEvent(ctx, event)
		assert.NoError(t, err)

		// Check container was cached
		metadata, exists := observer.containerCache["container-456"]
		assert.True(t, exists)
		assert.Equal(t, "test-app-2", metadata.ContainerName)
		assert.Equal(t, "redis:latest", metadata.ImageName)
	})

	t.Run("Container stop event", func(t *testing.T) {
		// First add a container
		observer.containerCache["container-789"] = &ContainerMetadata{
			ContainerID: "container-789",
			PodName:     "test-pod",
		}

		event := &domain.CollectorEvent{
			EventID:   "test-3",
			Timestamp: time.Now(),
			Type:      domain.EventTypeContainerStop,
			EventData: domain.EventDataContainer{
				Container: &domain.ContainerData{
					ContainerID: "container-789",
				},
			},
		}

		err := integration.HandleCRIEvent(ctx, event)
		assert.NoError(t, err)

		// Check container was removed from cache
		_, exists := observer.containerCache["container-789"]
		assert.False(t, exists)
	})

	t.Run("Invalid container data", func(t *testing.T) {
		event := &domain.CollectorEvent{
			EventID:   "test-4",
			Timestamp: time.Now(),
			Type:      domain.EventTypeContainerStart,
			EventData: domain.EventDataContainer{
				Container: nil, // Missing container data
			},
		}

		err := integration.HandleCRIEvent(ctx, event)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid container event data")
	})

	t.Run("Missing container ID", func(t *testing.T) {
		event := &domain.CollectorEvent{
			EventID:   "test-5",
			Timestamp: time.Now(),
			Type:      domain.EventTypeContainerStart,
			EventData: domain.EventDataContainer{
				Container: &domain.ContainerData{
					ContainerID: "", // Empty ID
				},
			},
		}

		err := integration.HandleCRIEvent(ctx, event)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing container ID")
	})

	t.Run("Unrelated event type", func(t *testing.T) {
		event := &domain.CollectorEvent{
			EventID:   "test-6",
			Timestamp: time.Now(),
			Type:      domain.EventTypeNetworkConnection, // Not a container event
			EventData: domain.EventDataContainer{
				Network: &domain.NetworkData{},
			},
		}

		// Should ignore and return nil
		err := integration.HandleCRIEvent(ctx, event)
		assert.NoError(t, err)
	})
}

func TestHandleContainerStart(t *testing.T) {
	// Create integration
	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("test")
	observer, err := NewObserver("test", config)
	require.NoError(t, err)
	observer.logger = logger
	observer.containerCache = make(map[string]*ContainerMetadata)

	integration := NewCRIIntegration(observer)
	ctx := context.Background()

	t.Run("Valid container start", func(t *testing.T) {
		event := &domain.CollectorEvent{
			EventData: domain.EventDataContainer{
				Container: &domain.ContainerData{
					ContainerID: "test-container",
					ImageName:   "app:v1",
					Runtime:     "containerd",
					Labels: map[string]string{
						"env":                          "prod",
						"io.kubernetes.container.name": "app",
						"io.kubernetes.pod.name":       "pod-1",
						"io.kubernetes.pod.uid":        "uid-1",
						"io.kubernetes.pod.namespace":  "production",
					},
				},
			},
		}

		err := integration.handleContainerStart(ctx, event)
		assert.NoError(t, err)

		// Verify metadata was stored correctly
		metadata := observer.containerCache["test-container"]
		assert.NotNil(t, metadata)
		assert.Equal(t, "test-container", metadata.ContainerID)
		assert.Equal(t, "app", metadata.ContainerName)
		assert.Equal(t, "app:v1", metadata.ImageName)
		assert.Equal(t, "pod-1", metadata.PodName)
		assert.Equal(t, "uid-1", metadata.PodUID)
		assert.Equal(t, "production", metadata.Namespace)
		assert.Equal(t, "containerd", metadata.Runtime)
		assert.Equal(t, "prod", metadata.Labels["env"])
	})
}

func TestHandleContainerStop(t *testing.T) {
	// Create integration
	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("test")
	observer, err := NewObserver("test", config)
	require.NoError(t, err)
	observer.logger = logger
	observer.containerCache = make(map[string]*ContainerMetadata)

	integration := NewCRIIntegration(observer)
	ctx := context.Background()

	t.Run("Valid container stop", func(t *testing.T) {
		// Pre-populate cache
		observer.containerCache["stop-test"] = &ContainerMetadata{
			ContainerID: "stop-test",
			PodName:     "pod",
		}

		event := &domain.CollectorEvent{
			EventData: domain.EventDataContainer{
				Container: &domain.ContainerData{
					ContainerID: "stop-test",
				},
			},
		}

		err := integration.handleContainerStop(ctx, event)
		assert.NoError(t, err)

		// Verify removed from cache
		_, exists := observer.containerCache["stop-test"]
		assert.False(t, exists)
	})
}
