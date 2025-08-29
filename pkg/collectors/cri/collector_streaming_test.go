package cri

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// TestLoadInitialState tests loading initial container state
func TestLoadInitialState(t *testing.T) {
	config := NewDefaultConfig("test")
	config.SocketPath = "/tmp/test.sock"
	collector, err := NewCollector("test-load", config)
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)
	collector.ctx = context.Background()

	// Set up mock client with containers
	mockClient := &mockCRIClient{
		containers: []*cri.Container{
			{
				Id: "container-123456789012",
				Image: &cri.ImageSpec{
					Image: "nginx:latest",
				},
				ImageRef: "docker.io/library/nginx:latest",
				Labels: map[string]string{
					"io.kubernetes.pod.name":      "nginx-pod",
					"io.kubernetes.pod.namespace": "default",
					"io.kubernetes.pod.uid":       "pod-uid-123",
				},
				State: cri.ContainerState_CONTAINER_RUNNING,
			},
		},
	}
	collector.client = mockClient

	// Load initial state
	err = collector.loadInitialState(context.Background())
	require.NoError(t, err)

	// Check that container info was cached
	collector.infoMu.RLock()
	info, exists := collector.containerInfo["container-123456789012"]
	collector.infoMu.RUnlock()

	assert.True(t, exists, "Container should be in cache")
	assert.Equal(t, "nginx-pod", info.PodName)
	assert.Equal(t, "default", info.PodNamespace)
	assert.Equal(t, "pod-uid-123", info.PodUID)
}

// TestProcessStreamEvent tests processing events from the stream
func TestProcessStreamEvent(t *testing.T) {
	config := NewDefaultConfig("test")
	config.SocketPath = "/tmp/test.sock"
	collector, err := NewCollector("test-stream", config)
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)
	collector.ctx = context.Background()

	// Create test events
	tests := []struct {
		name         string
		event        *cri.ContainerEventResponse
		expectedType domain.CollectorEventType
	}{
		{
			name: "container started",
			event: &cri.ContainerEventResponse{
				ContainerId:        "test-container-123",
				ContainerEventType: cri.ContainerEventType_CONTAINER_STARTED_EVENT,
				CreatedAt:          time.Now().UnixNano(),
				PodSandboxStatus: &cri.PodSandboxStatus{
					Metadata: &cri.PodSandboxMetadata{
						Name:      "test-pod",
						Namespace: "test-ns",
						Uid:       "test-uid",
					},
					Labels: map[string]string{
						"app": "test",
					},
				},
			},
			expectedType: domain.EventTypeContainerStart,
		},
		{
			name: "container stopped",
			event: &cri.ContainerEventResponse{
				ContainerId:        "test-container-456",
				ContainerEventType: cri.ContainerEventType_CONTAINER_STOPPED_EVENT,
				CreatedAt:          time.Now().UnixNano(),
			},
			expectedType: domain.EventTypeContainerStop,
		},
		{
			name: "container created",
			event: &cri.ContainerEventResponse{
				ContainerId:        "test-container-789",
				ContainerEventType: cri.ContainerEventType_CONTAINER_CREATED_EVENT,
				CreatedAt:          time.Now().UnixNano(),
			},
			expectedType: domain.EventTypeContainerCreate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Process the event
			err := collector.processStreamEvent(context.Background(), tt.event)
			require.NoError(t, err)

			// Check that event was sent
			select {
			case domainEvent := <-collector.events:
				assert.NotNil(t, domainEvent)
				assert.Equal(t, tt.expectedType, domainEvent.Type)
				assert.Equal(t, tt.event.ContainerId, domainEvent.CorrelationHints.ContainerID)

				// Check container info was cached
				collector.infoMu.RLock()
				info, exists := collector.containerInfo[tt.event.ContainerId]
				collector.infoMu.RUnlock()
				assert.True(t, exists)
				assert.Equal(t, tt.event.ContainerEventType.String(), info.State)
			case <-time.After(100 * time.Millisecond):
				t.Fatal("No event received")
			}
		})
	}
}

// TestUpdateContainerInfo tests updating container metadata cache
func TestUpdateContainerInfo(t *testing.T) {
	config := NewDefaultConfig("test")
	config.SocketPath = "/tmp/test.sock"
	collector, err := NewCollector("test-update", config)
	require.NoError(t, err)

	// First event - creates new info
	event1 := &cri.ContainerEventResponse{
		ContainerId:        "test-container",
		ContainerEventType: cri.ContainerEventType_CONTAINER_CREATED_EVENT,
		PodSandboxStatus: &cri.PodSandboxStatus{
			Metadata: &cri.PodSandboxMetadata{
				Name:      "test-pod",
				Namespace: "default",
				Uid:       "pod-123",
			},
			Labels: map[string]string{
				"app": "test",
			},
			Annotations: map[string]string{
				"annotation": "value",
			},
		},
	}

	info := collector.updateContainerInfo(event1)
	assert.NotNil(t, info)
	assert.Equal(t, "test-container", info.ContainerID)
	assert.Equal(t, "test-pod", info.PodName)
	assert.Equal(t, "default", info.PodNamespace)
	assert.Equal(t, "pod-123", info.PodUID)
	assert.Equal(t, "test", info.Labels["app"])
	assert.Equal(t, "value", info.Annotations["annotation"])

	// Second event - updates existing info
	event2 := &cri.ContainerEventResponse{
		ContainerId:        "test-container",
		ContainerEventType: cri.ContainerEventType_CONTAINER_STARTED_EVENT,
		PodSandboxStatus: &cri.PodSandboxStatus{
			Labels: map[string]string{
				"app":    "test",
				"status": "running",
			},
		},
	}

	info = collector.updateContainerInfo(event2)
	assert.Equal(t, cri.ContainerEventType_CONTAINER_STARTED_EVENT.String(), info.State)
	assert.Equal(t, "running", info.Labels["status"])
}

// TestHealthChecker tests the health checking mechanism
func TestHealthChecker(t *testing.T) {
	config := NewDefaultConfig("test")
	config.SocketPath = "/tmp/test.sock"
	collector, err := NewCollector("test-health", config)
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)

	// Simulate unhealthy connection
	collector.updateHealthStatus(false)
	assert.False(t, collector.IsHealthy())

	// Check health should trigger reconnect
	collector.checkHealth()

	// Should have sent reconnect signal
	select {
	case <-collector.reconnectChan:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("No reconnect signal sent")
	}
}

// TestEnrichMetadata tests metadata enrichment
func TestEnrichMetadata(t *testing.T) {
	config := NewDefaultConfig("test")
	config.SocketPath = "/tmp/test.sock"
	collector, err := NewCollector("test-enrich", config)
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)
	collector.ctx = context.Background()

	// Add container to cache
	collector.containerInfo["test-container"] = &ContainerInfo{
		ContainerID: "test-container",
		State:       "CONTAINER_RUNNING",
	}

	// Mock client that returns updated status
	mockClient := &mockCRIClientWithStatus{
		status: &cri.ContainerStatus{
			Id:         "test-container",
			State:      cri.ContainerState_CONTAINER_EXITED,
			ExitCode:   1,
			Reason:     "Error",
			Message:    "Container failed",
			FinishedAt: time.Now().UnixNano(),
		},
	}
	collector.client = mockClient

	// Enrich metadata
	collector.enrichMetadata()

	// Check that info was updated
	collector.infoMu.RLock()
	info := collector.containerInfo["test-container"]
	collector.infoMu.RUnlock()

	assert.Equal(t, "CONTAINER_EXITED", info.State)
	assert.Equal(t, int32(1), info.ExitCode)
	assert.Equal(t, "Error", info.Reason)
	assert.Equal(t, "Container failed", info.Message)
}

// TestMapEventType tests mapping CRI event types to domain types
func TestMapEventType(t *testing.T) {
	tests := []struct {
		criType      cri.ContainerEventType
		expectedType domain.CollectorEventType
	}{
		{cri.ContainerEventType_CONTAINER_CREATED_EVENT, domain.EventTypeContainerCreate},
		{cri.ContainerEventType_CONTAINER_STARTED_EVENT, domain.EventTypeContainerStart},
		{cri.ContainerEventType_CONTAINER_STOPPED_EVENT, domain.EventTypeContainerStop},
		{cri.ContainerEventType_CONTAINER_DELETED_EVENT, domain.EventTypeContainerStop},
	}

	for _, tt := range tests {
		t.Run(tt.criType.String(), func(t *testing.T) {
			result := mapEventType(tt.criType)
			assert.Equal(t, tt.expectedType, result)
		})
	}
}

// TestMapEventAction tests mapping CRI event types to action strings
func TestMapEventAction(t *testing.T) {
	tests := []struct {
		criType        cri.ContainerEventType
		expectedAction string
	}{
		{cri.ContainerEventType_CONTAINER_CREATED_EVENT, "create"},
		{cri.ContainerEventType_CONTAINER_STARTED_EVENT, "start"},
		{cri.ContainerEventType_CONTAINER_STOPPED_EVENT, "stop"},
		{cri.ContainerEventType_CONTAINER_DELETED_EVENT, "delete"},
	}

	for _, tt := range tests {
		t.Run(tt.criType.String(), func(t *testing.T) {
			result := mapEventAction(tt.criType)
			assert.Equal(t, tt.expectedAction, result)
		})
	}
}

// TestExtractFromLabels tests safe label extraction
func TestExtractFromLabels(t *testing.T) {
	labels := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	// Existing key
	assert.Equal(t, "value1", extractFromLabels(labels, "key1"))

	// Non-existing key
	assert.Equal(t, "", extractFromLabels(labels, "key3"))

	// Nil labels
	assert.Equal(t, "", extractFromLabels(nil, "key1"))
}

// mockCRIClientWithStatus is a mock client that returns specific status
type mockCRIClientWithStatus struct {
	cri.RuntimeServiceClient
	status *cri.ContainerStatus
}

func (m *mockCRIClientWithStatus) ContainerStatus(ctx context.Context, req *cri.ContainerStatusRequest, opts ...grpc.CallOption) (*cri.ContainerStatusResponse, error) {
	return &cri.ContainerStatusResponse{
		Status: m.status,
	}, nil
}
