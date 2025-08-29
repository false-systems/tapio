package cri

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// TestCollectorLifecycle tests Start and Stop methods
func TestCollectorLifecycle(t *testing.T) {
	config := NewDefaultConfig("test")
	config.SocketPath = "/tmp/test-cri.sock"

	collector, err := NewCollector("test-lifecycle", config)
	require.NoError(t, err)
	require.NotNil(t, collector)

	collector.logger = zaptest.NewLogger(t)

	// Test initial health status
	assert.False(t, collector.IsHealthy(), "Collector should not be healthy before start")

	// Create a test context
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start will fail because socket doesn't exist, but that's expected
	err = collector.Start(ctx)
	assert.Error(t, err, "Start should fail with non-existent socket")
	assert.Contains(t, err.Error(), "failed to connect to CRI socket")

	// Health should still be false
	assert.False(t, collector.IsHealthy(), "Collector should not be healthy after failed start")

	// Test Stop
	err = collector.Stop()
	assert.NoError(t, err, "Stop should not error even after failed start")
	assert.False(t, collector.IsHealthy(), "Collector should not be healthy after stop")
}

// TestCollectorStartCancelContext tests Start with cancelled context
func TestCollectorStartCancelContext(t *testing.T) {
	config := NewDefaultConfig("test")
	config.SocketPath = "/tmp/test-cancelled.sock"

	collector, err := NewCollector("test-cancel", config)
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)

	// Create already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = collector.Start(ctx)
	assert.Error(t, err)
	assert.False(t, collector.IsHealthy())
}

// TestMonitor tests the monitor goroutine
func TestMonitor(t *testing.T) {
	config := NewDefaultConfig("test")
	config.PollInterval = 100 * time.Millisecond
	config.SocketPath = "/tmp/test.sock"

	collector, err := NewCollector("test-monitor", config)
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)

	// Create mock client
	collector.client = &mockCRIClient{
		containers: []*cri.Container{},
	}

	// Set up context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector.ctx = ctx
	collector.cancel = cancel

	// Track calls to pollContainers using a mock client that counts calls
	var callCount int32
	mockClient := &countingMockClient{
		mockCRIClient: &mockCRIClient{containers: []*cri.Container{}},
		callCount:     &callCount,
	}
	collector.client = mockClient

	// Start stream monitor in goroutine
	go collector.streamMonitor()

	// Wait for stream to establish and retry
	time.Sleep(250 * time.Millisecond)

	// Cancel context to stop monitor
	cancel()

	// Give monitor time to stop
	time.Sleep(50 * time.Millisecond)

	// Should have attempted to connect at least once (streaming may only try once before backoff)
	count := atomic.LoadInt32(&callCount)
	assert.GreaterOrEqual(t, int(count), 1, "Monitor should have attempted at least once")
}

// TestUpdateHealthStatus tests thread-safe health status updates
func TestUpdateHealthStatus(t *testing.T) {
	config := NewDefaultConfig("test")
	config.SocketPath = "/tmp/test.sock"
	collector, err := NewCollector("test-health", config)
	require.NoError(t, err)

	// Test setting to true
	collector.updateHealthStatus(true)
	assert.True(t, collector.IsHealthy())

	// Test setting to false
	collector.updateHealthStatus(false)
	assert.False(t, collector.IsHealthy())

	// Test concurrent updates (race condition test)
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(val bool) {
			collector.updateHealthStatus(val)
			done <- true
		}(i%2 == 0)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// mockCRIClient implements a mock CRI client for testing
type mockCRIClient struct {
	cri.RuntimeServiceClient
	containers []*cri.Container
	shouldFail bool
	failCount  int
}

func (m *mockCRIClient) ListContainers(ctx context.Context, req *cri.ListContainersRequest, opts ...grpc.CallOption) (*cri.ListContainersResponse, error) {
	if m.shouldFail {
		m.failCount++
		return nil, assert.AnError
	}
	return &cri.ListContainersResponse{
		Containers: m.containers,
	}, nil
}

func (m *mockCRIClient) ContainerStatus(ctx context.Context, req *cri.ContainerStatusRequest, opts ...grpc.CallOption) (*cri.ContainerStatusResponse, error) {
	if m.shouldFail {
		return nil, assert.AnError
	}

	// Return mock status based on container ID
	state := cri.ContainerState_CONTAINER_RUNNING
	if req.ContainerId == "exited-container" {
		state = cri.ContainerState_CONTAINER_EXITED
	} else if req.ContainerId == "created-container" {
		state = cri.ContainerState_CONTAINER_CREATED
	}

	return &cri.ContainerStatusResponse{
		Status: &cri.ContainerStatus{
			State: state,
		},
	}, nil
}

func (m *mockCRIClient) GetContainerEvents(ctx context.Context, req *cri.GetEventsRequest, opts ...grpc.CallOption) (cri.RuntimeService_GetContainerEventsClient, error) {
	// Always return error to simulate streaming failure for testing
	return nil, assert.AnError
}

// countingMockClient counts ListContainers calls
type countingMockClient struct {
	*mockCRIClient
	callCount *int32
}

func (c *countingMockClient) ListContainers(ctx context.Context, req *cri.ListContainersRequest, opts ...grpc.CallOption) (*cri.ListContainersResponse, error) {
	atomic.AddInt32(c.callCount, 1)
	return c.mockCRIClient.ListContainers(ctx, req, opts...)
}

func (c *countingMockClient) GetContainerEvents(ctx context.Context, req *cri.GetEventsRequest, opts ...grpc.CallOption) (cri.RuntimeService_GetContainerEventsClient, error) {
	atomic.AddInt32(c.callCount, 1)
	return c.mockCRIClient.GetContainerEvents(ctx, req, opts...)
}
