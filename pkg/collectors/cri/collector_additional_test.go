package cri

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// MockCRIClient is a mock implementation of the CRI RuntimeServiceClient
type MockCRIClient struct {
	mock.Mock
}

func (m *MockCRIClient) ListContainers(ctx context.Context, req *cri.ListContainersRequest) (*cri.ListContainersResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(*cri.ListContainersResponse), args.Error(1)
}

func (m *MockCRIClient) ContainerStatus(ctx context.Context, req *cri.ContainerStatusRequest) (*cri.ContainerStatusResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(*cri.ContainerStatusResponse), args.Error(1)
}

// Add other required methods as no-ops
func (m *MockCRIClient) Version(ctx context.Context, req *cri.VersionRequest) (*cri.VersionResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(*cri.VersionResponse), args.Error(1)
}

func (m *MockCRIClient) RunPodSandbox(ctx context.Context, req *cri.RunPodSandboxRequest) (*cri.RunPodSandboxResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) StopPodSandbox(ctx context.Context, req *cri.StopPodSandboxRequest) (*cri.StopPodSandboxResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) RemovePodSandbox(ctx context.Context, req *cri.RemovePodSandboxRequest) (*cri.RemovePodSandboxResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) PodSandboxStatus(ctx context.Context, req *cri.PodSandboxStatusRequest) (*cri.PodSandboxStatusResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ListPodSandbox(ctx context.Context, req *cri.ListPodSandboxRequest) (*cri.ListPodSandboxResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) CreateContainer(ctx context.Context, req *cri.CreateContainerRequest) (*cri.CreateContainerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) StartContainer(ctx context.Context, req *cri.StartContainerRequest) (*cri.StartContainerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) StopContainer(ctx context.Context, req *cri.StopContainerRequest) (*cri.StopContainerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) RemoveContainer(ctx context.Context, req *cri.RemoveContainerRequest) (*cri.RemoveContainerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ListImages(ctx context.Context, req *cri.ListImagesRequest) (*cri.ListImagesResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ImageStatus(ctx context.Context, req *cri.ImageStatusRequest) (*cri.ImageStatusResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) PullImage(ctx context.Context, req *cri.PullImageRequest) (*cri.PullImageResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) RemoveImage(ctx context.Context, req *cri.RemoveImageRequest) (*cri.RemoveImageResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ImageFsInfo(ctx context.Context, req *cri.ImageFsInfoRequest) (*cri.ImageFsInfoResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ContainerStats(ctx context.Context, req *cri.ContainerStatsRequest) (*cri.ContainerStatsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ListContainerStats(ctx context.Context, req *cri.ListContainerStatsRequest) (*cri.ListContainerStatsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) PodSandboxStats(ctx context.Context, req *cri.PodSandboxStatsRequest) (*cri.PodSandboxStatsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ListPodSandboxStats(ctx context.Context, req *cri.ListPodSandboxStatsRequest) (*cri.ListPodSandboxStatsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) UpdateRuntimeConfig(ctx context.Context, req *cri.UpdateRuntimeConfigRequest) (*cri.UpdateRuntimeConfigResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) Status(ctx context.Context, req *cri.StatusRequest) (*cri.StatusResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) CheckpointContainer(ctx context.Context, req *cri.CheckpointContainerRequest) (*cri.CheckpointContainerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) GetContainerEvents(req *cri.GetEventsRequest, server cri.RuntimeService_GetContainerEventsServer) error {
	return fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ListMetricDescriptors(ctx context.Context, req *cri.ListMetricDescriptorsRequest) (*cri.ListMetricDescriptorsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ListPodSandboxMetrics(ctx context.Context, req *cri.ListPodSandboxMetricsRequest) (*cri.ListPodSandboxMetricsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) RuntimeConfig(ctx context.Context, req *cri.RuntimeConfigRequest) (*cri.RuntimeConfigResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) Attach(ctx context.Context, req *cri.AttachRequest) (*cri.AttachResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// Test functions

func TestCollectorStartStop(t *testing.T) {
	cfg := &Config{
		SocketPath:   "/tmp/test.sock",
		BufferSize:   100,
		PollInterval: 1 * time.Second,
	}

	collector, err := NewCollector("test-cri", cfg)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Test that collector starts in disconnected state since socket doesn't exist
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	assert.NoError(t, err, "Start should succeed even without CRI socket")

	// Verify collector is running
	assert.True(t, collector.IsHealthy())

	// Test stop
	err = collector.Stop()
	assert.NoError(t, err)
}

func TestCollectorEventsChannel(t *testing.T) {
	cfg := &Config{
		SocketPath:   "/tmp/test.sock",
		BufferSize:   10,
		PollInterval: 100 * time.Millisecond,
	}

	collector, err := NewCollector("test-cri", cfg)
	require.NoError(t, err)

	events := collector.Events()
	assert.NotNil(t, events)

	// Channel should be buffered with correct size
	assert.Equal(t, 10, cap(events))
}

func TestGetContainerEventType(t *testing.T) {
	tests := []struct {
		name     string
		state    cri.ContainerState
		expected domain.CollectorEventType
	}{
		{
			name:     "created container",
			state:    cri.ContainerState_CONTAINER_CREATED,
			expected: domain.EventTypeContainerCreate,
		},
		{
			name:     "running container",
			state:    cri.ContainerState_CONTAINER_RUNNING,
			expected: domain.EventTypeContainerStart,
		},
		{
			name:     "exited container",
			state:    cri.ContainerState_CONTAINER_EXITED,
			expected: domain.EventTypeContainerStop,
		},
		{
			name:     "unknown container state",
			state:    cri.ContainerState_CONTAINER_UNKNOWN,
			expected: domain.EventTypeContainerStop, // Default to stop
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getContainerEventType(tt.state)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetContainerAction(t *testing.T) {
	tests := []struct {
		name     string
		state    cri.ContainerState
		expected string
	}{
		{
			name:     "created container",
			state:    cri.ContainerState_CONTAINER_CREATED,
			expected: "create",
		},
		{
			name:     "running container",
			state:    cri.ContainerState_CONTAINER_RUNNING,
			expected: "start",
		},
		{
			name:     "exited container",
			state:    cri.ContainerState_CONTAINER_EXITED,
			expected: "stop",
		},
		{
			name:     "unknown container state",
			state:    cri.ContainerState_CONTAINER_UNKNOWN,
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getContainerAction(tt.state)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectCRISocket(t *testing.T) {
	// This tests the socket detection logic
	socket := detectCRISocket()

	// On test systems, this will likely return empty string
	// since none of the standard sockets exist
	assert.IsType(t, "", socket)
}

func TestProcessContainerWithMockClient(t *testing.T) {
	cfg := &Config{
		SocketPath:   "/tmp/test.sock",
		BufferSize:   100,
		PollInterval: 1 * time.Second,
	}

	collector, err := NewCollector("test-cri", cfg)
	require.NoError(t, err)

	// Create mock client
	mockClient := &MockCRIClient{}
	collector.client = mockClient

	// Setup test container
	containerID := "test-container-123"
	container := &cri.Container{
		Id:       containerID,
		ImageRef: "sha256:abc123",
		Image: &cri.ImageSpec{
			Image: "nginx:latest",
		},
		Labels: map[string]string{
			"io.kubernetes.pod.name":      "nginx-pod",
			"io.kubernetes.pod.namespace": "default",
			"io.kubernetes.pod.uid":       "pod-uid-123",
		},
	}

	status := &cri.ContainerStatus{
		Id: containerID,
		Metadata: &cri.ContainerMetadata{
			Name: "nginx",
		},
		State:     cri.ContainerState_CONTAINER_RUNNING,
		CreatedAt: time.Now().UnixNano(),
	}

	// Mock the ContainerStatus call
	mockClient.On("ContainerStatus", mock.Anything, &cri.ContainerStatusRequest{
		ContainerId: containerID,
		Verbose:     false,
	}).Return(&cri.ContainerStatusResponse{
		Status: status,
	}, nil)

	ctx := context.Background()

	// Process the container - this should generate an event
	collector.processContainer(ctx, container)

	// Verify the mock was called
	mockClient.AssertExpectations(t)

	// Try to read event from channel (non-blocking)
	select {
	case event := <-collector.Events():
		assert.Equal(t, domain.EventTypeContainerStart, event.Type)
		assert.Equal(t, collector.name, event.Source)
		assert.Equal(t, containerID, event.EventData.Container.ContainerID)
		assert.Equal(t, "nginx:latest", event.EventData.Container.ImageName)
		assert.Equal(t, "start", event.EventData.Container.Action)

		// Check Kubernetes context
		assert.Equal(t, "nginx-pod", event.K8sContext.Name)
		assert.Equal(t, "default", event.K8sContext.Namespace)
		assert.Equal(t, "pod-uid-123", event.K8sContext.UID)

	case <-time.After(100 * time.Millisecond):
		t.Fatal("No event was generated")
	}
}

func TestProcessContainerWithStatusError(t *testing.T) {
	cfg := &Config{
		SocketPath:   "/tmp/test.sock",
		BufferSize:   100,
		PollInterval: 1 * time.Second,
	}

	collector, err := NewCollector("test-cri", cfg)
	require.NoError(t, err)

	// Create mock client
	mockClient := &MockCRIClient{}
	collector.client = mockClient

	containerID := "test-container-123"
	container := &cri.Container{
		Id: containerID,
	}

	// Mock the ContainerStatus call to return error
	mockClient.On("ContainerStatus", mock.Anything, &cri.ContainerStatusRequest{
		ContainerId: containerID,
		Verbose:     false,
	}).Return((*cri.ContainerStatusResponse)(nil), fmt.Errorf("container not found"))

	ctx := context.Background()

	// Process the container - should handle error gracefully
	collector.processContainer(ctx, container)

	// Verify the mock was called
	mockClient.AssertExpectations(t)

	// Should not generate any events
	select {
	case <-collector.Events():
		t.Fatal("No event should be generated on error")
	case <-time.After(50 * time.Millisecond):
		// Expected - no event generated
	}
}

func TestCollectorName(t *testing.T) {
	cfg := &Config{
		SocketPath:   "/tmp/test.sock",
		BufferSize:   100,
		PollInterval: 1 * time.Second,
	}

	collector, err := NewCollector("test-collector", cfg)
	require.NoError(t, err)

	assert.Equal(t, "test-collector", collector.Name())
}
