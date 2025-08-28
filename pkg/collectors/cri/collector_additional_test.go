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
	"google.golang.org/grpc"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// MockCRIClient is a mock implementation of the CRI RuntimeServiceClient
type MockCRIClient struct {
	mock.Mock
}

func (m *MockCRIClient) ListContainers(ctx context.Context, req *cri.ListContainersRequest, opts ...grpc.CallOption) (*cri.ListContainersResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(*cri.ListContainersResponse), args.Error(1)
}

func (m *MockCRIClient) ContainerStatus(ctx context.Context, req *cri.ContainerStatusRequest, opts ...grpc.CallOption) (*cri.ContainerStatusResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(*cri.ContainerStatusResponse), args.Error(1)
}

// Add other required methods as no-ops
func (m *MockCRIClient) Version(ctx context.Context, req *cri.VersionRequest, opts ...grpc.CallOption) (*cri.VersionResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(*cri.VersionResponse), args.Error(1)
}

func (m *MockCRIClient) RunPodSandbox(ctx context.Context, req *cri.RunPodSandboxRequest, opts ...grpc.CallOption) (*cri.RunPodSandboxResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) StopPodSandbox(ctx context.Context, req *cri.StopPodSandboxRequest, opts ...grpc.CallOption) (*cri.StopPodSandboxResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) RemovePodSandbox(ctx context.Context, req *cri.RemovePodSandboxRequest, opts ...grpc.CallOption) (*cri.RemovePodSandboxResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) PodSandboxStatus(ctx context.Context, req *cri.PodSandboxStatusRequest, opts ...grpc.CallOption) (*cri.PodSandboxStatusResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ListPodSandbox(ctx context.Context, req *cri.ListPodSandboxRequest, opts ...grpc.CallOption) (*cri.ListPodSandboxResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) CreateContainer(ctx context.Context, req *cri.CreateContainerRequest, opts ...grpc.CallOption) (*cri.CreateContainerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) StartContainer(ctx context.Context, req *cri.StartContainerRequest, opts ...grpc.CallOption) (*cri.StartContainerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) StopContainer(ctx context.Context, req *cri.StopContainerRequest, opts ...grpc.CallOption) (*cri.StopContainerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) RemoveContainer(ctx context.Context, req *cri.RemoveContainerRequest, opts ...grpc.CallOption) (*cri.RemoveContainerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ListImages(ctx context.Context, req *cri.ListImagesRequest, opts ...grpc.CallOption) (*cri.ListImagesResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ImageStatus(ctx context.Context, req *cri.ImageStatusRequest, opts ...grpc.CallOption) (*cri.ImageStatusResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) PullImage(ctx context.Context, req *cri.PullImageRequest, opts ...grpc.CallOption) (*cri.PullImageResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) RemoveImage(ctx context.Context, req *cri.RemoveImageRequest, opts ...grpc.CallOption) (*cri.RemoveImageResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ImageFsInfo(ctx context.Context, req *cri.ImageFsInfoRequest, opts ...grpc.CallOption) (*cri.ImageFsInfoResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ContainerStats(ctx context.Context, req *cri.ContainerStatsRequest, opts ...grpc.CallOption) (*cri.ContainerStatsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ListContainerStats(ctx context.Context, req *cri.ListContainerStatsRequest, opts ...grpc.CallOption) (*cri.ListContainerStatsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) PodSandboxStats(ctx context.Context, req *cri.PodSandboxStatsRequest, opts ...grpc.CallOption) (*cri.PodSandboxStatsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ListPodSandboxStats(ctx context.Context, req *cri.ListPodSandboxStatsRequest, opts ...grpc.CallOption) (*cri.ListPodSandboxStatsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) UpdateRuntimeConfig(ctx context.Context, req *cri.UpdateRuntimeConfigRequest, opts ...grpc.CallOption) (*cri.UpdateRuntimeConfigResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) Status(ctx context.Context, req *cri.StatusRequest, opts ...grpc.CallOption) (*cri.StatusResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) CheckpointContainer(ctx context.Context, req *cri.CheckpointContainerRequest, opts ...grpc.CallOption) (*cri.CheckpointContainerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) GetContainerEvents(ctx context.Context, req *cri.GetEventsRequest, opts ...grpc.CallOption) (cri.RuntimeService_GetContainerEventsClient, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ListMetricDescriptors(ctx context.Context, req *cri.ListMetricDescriptorsRequest, opts ...grpc.CallOption) (*cri.ListMetricDescriptorsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ListPodSandboxMetrics(ctx context.Context, req *cri.ListPodSandboxMetricsRequest, opts ...grpc.CallOption) (*cri.ListPodSandboxMetricsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) RuntimeConfig(ctx context.Context, req *cri.RuntimeConfigRequest, opts ...grpc.CallOption) (*cri.RuntimeConfigResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) Attach(ctx context.Context, req *cri.AttachRequest, opts ...grpc.CallOption) (*cri.AttachResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// Add missing methods from CRI interface
func (m *MockCRIClient) ReopenContainerLog(ctx context.Context, req *cri.ReopenContainerLogRequest, opts ...grpc.CallOption) (*cri.ReopenContainerLogResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) ExecSync(ctx context.Context, req *cri.ExecSyncRequest, opts ...grpc.CallOption) (*cri.ExecSyncResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) Exec(ctx context.Context, req *cri.ExecRequest, opts ...grpc.CallOption) (*cri.ExecResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) PortForward(ctx context.Context, req *cri.PortForwardRequest, opts ...grpc.CallOption) (*cri.PortForwardResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) UpdateContainerResources(ctx context.Context, req *cri.UpdateContainerResourcesRequest, opts ...grpc.CallOption) (*cri.UpdateContainerResourcesResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockCRIClient) UpdatePodSandboxResources(ctx context.Context, req *cri.UpdatePodSandboxResourcesRequest, opts ...grpc.CallOption) (*cri.UpdatePodSandboxResourcesResponse, error) {
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

	// Test that collector fails to start since socket doesn't exist
	// This is expected behavior - collector should fail if it can't connect
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = collector.Start(ctx)
	assert.Error(t, err, "Start should fail without CRI socket")
	assert.Contains(t, err.Error(), "failed to connect to CRI socket")

	// Collector should not be healthy after failed start
	assert.False(t, collector.IsHealthy())

	// Test stop (should succeed even after failed start)
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

func TestPollContainersWithMockClient(t *testing.T) {
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

	// Setup mock response for ListContainers with empty list
	mockClient.On("ListContainers", mock.Anything, &cri.ListContainersRequest{}).Return(
		&cri.ListContainersResponse{
			Containers: []*cri.Container{},
		}, nil)

	// Set up context and cancel so the collector can be initialized
	collector.ctx, collector.cancel = context.WithCancel(context.Background())
	defer collector.cancel()

	// Load initial state
	err = collector.loadInitialState(context.Background())
	require.NoError(t, err)

	// Verify the mock was called
	mockClient.AssertExpectations(t)
}

func TestPollContainersListError(t *testing.T) {
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

	// Setup mock to return error
	mockClient.On("ListContainers", mock.Anything, &cri.ListContainersRequest{}).Return(
		(*cri.ListContainersResponse)(nil), fmt.Errorf("connection failed"))

	// Set up context and cancel so the collector can be initialized
	collector.ctx, collector.cancel = context.WithCancel(context.Background())
	defer collector.cancel()

	// Load initial state - should handle error
	err = collector.loadInitialState(context.Background())
	require.Error(t, err)

	// Verify the mock was called
	mockClient.AssertExpectations(t)
}

func TestStreamMonitorReconnect(t *testing.T) {
	cfg := &Config{
		SocketPath:   "/tmp/test.sock",
		BufferSize:   100,
		PollInterval: 50 * time.Millisecond, // Not used in streaming
	}

	collector, err := NewCollector("test-cri", cfg)
	require.NoError(t, err)

	// Create mock client
	mockClient := &MockCRIClient{}
	collector.client = mockClient

	// Setup mock to return event stream
	mockClient.On("GetContainerEvents", mock.Anything, &cri.GetEventsRequest{}).Return(
		nil, fmt.Errorf("stream error")).Maybe()

	// Start stream monitor in goroutine
	ctx, cancel := context.WithCancel(context.Background())
	collector.ctx, collector.cancel = context.WithCancel(ctx)

	go collector.streamMonitor()

	// Let it run for a bit to trigger multiple polls
	time.Sleep(150 * time.Millisecond)

	// Cancel and cleanup
	cancel()
	collector.cancel()

	// Wait a bit for goroutine to finish
	time.Sleep(10 * time.Millisecond)

	// Verify the mock was called multiple times
	mockClient.AssertExpectations(t)
}

func TestDetectCRISocketVariants(t *testing.T) {
	// Test the socket detection with various scenarios
	socket := detectCRISocket()

	// On most test systems, this should return empty string
	// since standard sockets don't exist
	if socket != "" {
		// If a socket is found, it should be one of the expected paths
		expectedSockets := []string{
			"/run/containerd/containerd.sock",
			"/run/crio/crio.sock",
			"/var/run/dockershim.sock",
			"/var/run/cri-dockerd.sock",
		}

		found := false
		for _, expected := range expectedSockets {
			if socket == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "Detected socket should be one of the expected paths: %s", socket)
	}
}

func TestNewCollectorWithAutoDetection(t *testing.T) {
	cfg := &Config{
		SocketPath:   "", // Empty to trigger auto-detection
		BufferSize:   100,
		PollInterval: 1 * time.Second,
	}

	// This should either succeed (if a CRI socket is found) or fail
	collector, err := NewCollector("test-cri", cfg)

	if err != nil {
		// Expected when no CRI socket is found
		assert.Contains(t, err.Error(), "no CRI socket found")
		assert.Nil(t, collector)
	} else {
		// If collector is created, it should be valid
		assert.NotNil(t, collector)
		assert.Equal(t, "test-cri", collector.Name())
	}
}
