package containerruntime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestOnContainerStart(t *testing.T) {
	// Create observer instance
	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("test")
	observer, err := NewObserver("test", config)
	require.NoError(t, err)
	observer.logger = logger
	observer.containerCache = make(map[string]*ContainerMetadata)

	metadata := &ContainerMetadata{
		ContainerID:   "test-container-123",
		PodName:       "test-pod",
		PodUID:        "pod-uid-123",
		Namespace:     "default",
		ContainerName: "app",
		Runtime:       "docker",
		ImageName:     "nginx:latest",
		Labels: map[string]string{
			"app": "test",
		},
		Annotations: map[string]string{
			"version": "1.0",
		},
	}

	t.Run("Add new container", func(t *testing.T) {
		err := observer.OnContainerStart("test-container-123", metadata)
		assert.NoError(t, err)

		// Check cache
		cached, exists := observer.containerCache["test-container-123"]
		assert.True(t, exists)
		assert.Equal(t, metadata, cached)
	})

	t.Run("Update existing container", func(t *testing.T) {
		// Update metadata
		updatedMetadata := *metadata
		updatedMetadata.ImageName = "nginx:1.21"

		err := observer.OnContainerStart("test-container-123", &updatedMetadata)
		assert.NoError(t, err)

		// Check cache has updated metadata
		cached, exists := observer.containerCache["test-container-123"]
		assert.True(t, exists)
		assert.Equal(t, "nginx:1.21", cached.ImageName)
	})

	t.Run("Multiple containers", func(t *testing.T) {
		// Add second container
		metadata2 := &ContainerMetadata{
			ContainerID:   "test-container-456",
			PodName:       "test-pod-2",
			ContainerName: "app2",
		}

		err := observer.OnContainerStart("test-container-456", metadata2)
		assert.NoError(t, err)

		// Check both containers exist
		assert.Len(t, observer.containerCache, 2)
		assert.NotNil(t, observer.containerCache["test-container-123"])
		assert.NotNil(t, observer.containerCache["test-container-456"])
	})
}

func TestOnContainerStop(t *testing.T) {
	// Create observer instance
	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("test")
	observer, err := NewObserver("test", config)
	require.NoError(t, err)
	observer.logger = logger
	observer.containerCache = make(map[string]*ContainerMetadata)

	// Pre-populate cache
	metadata1 := &ContainerMetadata{
		ContainerID: "container-1",
		PodName:     "pod-1",
	}
	metadata2 := &ContainerMetadata{
		ContainerID: "container-2",
		PodName:     "pod-2",
	}
	observer.containerCache["container-1"] = metadata1
	observer.containerCache["container-2"] = metadata2

	t.Run("Stop existing container", func(t *testing.T) {
		err := observer.OnContainerStop("container-1")
		assert.NoError(t, err)

		// Check removed from cache
		_, exists := observer.containerCache["container-1"]
		assert.False(t, exists)

		// Other container should still exist
		_, exists = observer.containerCache["container-2"]
		assert.True(t, exists)
	})

	t.Run("Stop non-existent container", func(t *testing.T) {
		// Should not error on non-existent container
		err := observer.OnContainerStop("non-existent")
		assert.NoError(t, err)
	})

	t.Run("Stop all containers", func(t *testing.T) {
		err := observer.OnContainerStop("container-2")
		assert.NoError(t, err)

		// Cache should be empty
		assert.Empty(t, observer.containerCache)
	})
}

func TestContainerLifecycleConcurrency(t *testing.T) {
	// Test concurrent access to container cache
	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("test")
	observer, err := NewObserver("test", config)
	require.NoError(t, err)
	observer.logger = logger
	observer.containerCache = make(map[string]*ContainerMetadata)

	// Run concurrent starts and stops
	done := make(chan bool, 10)

	// Start containers concurrently
	for i := 0; i < 5; i++ {
		go func(id int) {
			metadata := &ContainerMetadata{
				ContainerID: string(rune('0' + id)),
				PodName:     "pod",
			}
			_ = observer.OnContainerStart(string(rune('0'+id)), metadata)
			done <- true
		}(i)
	}

	// Stop containers concurrently
	for i := 0; i < 5; i++ {
		go func(id int) {
			_ = observer.OnContainerStop(string(rune('0' + id)))
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// No assertion on final state due to race conditions,
	// just testing that it doesn't panic or deadlock
}
