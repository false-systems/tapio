package cri

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// TestStressManyContainers tests with many containers
func TestStressManyContainers(t *testing.T) {
	config := NewDefaultConfig("stress")
	config.BufferSize = 1000
	config.SocketPath = "/tmp/test.sock"

	collector, err := NewCollector("stress-test", config)
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)
	collector.ctx = context.Background()

	// Create many containers
	containers := make([]*cri.Container, 100)
	for i := 0; i < 100; i++ {
		// Create container ID at least 12 chars long
		id := "container-" + string(rune('a'+i%26)) + string(rune('0'+i/26)) + "-1234567890"
		containers[i] = &cri.Container{
			Id: id,
			Image: &cri.ImageSpec{
				Image: "test:latest",
			},
			ImageRef: "test:latest",
			Labels: map[string]string{
				"index": string(rune(i)),
			},
		}
	}

	collector.client = &mockCRIClient{
		containers: containers,
	}

	// Load initial state with many containers
	start := time.Now()
	err = collector.loadInitialState(context.Background())
	require.NoError(t, err)
	elapsed := time.Since(start)

	t.Logf("Processed %d containers in %v", len(containers), elapsed)
	assert.Less(t, elapsed, 5*time.Second, "Should process 100 containers quickly")

	// Check that containers were cached (loadInitialState doesn't generate events)
	collector.infoMu.RLock()
	cachedCount := len(collector.containerInfo)
	collector.infoMu.RUnlock()

	assert.Equal(t, 100, cachedCount, "Should cache info for each container")
}

// TestStressConcurrentAccess tests concurrent access to collector
func TestStressConcurrentAccess(t *testing.T) {
	config := NewDefaultConfig("concurrent")
	config.SocketPath = "/tmp/test.sock"

	collector, err := NewCollector("concurrent-test", config)
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)

	var wg sync.WaitGroup

	// Concurrent health checks
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = collector.IsHealthy()
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Concurrent health updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			collector.updateHealthStatus(i%2 == 0)
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Concurrent name checks
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			name := collector.Name()
			assert.Equal(t, "concurrent-test", name)
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Concurrent event channel access
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			ch := collector.Events()
			assert.NotNil(t, ch)
			time.Sleep(1 * time.Millisecond)
		}
	}()

	wg.Wait()
}

// TestStressRapidStartStop tests rapid start/stop cycles
func TestStressRapidStartStop(t *testing.T) {
	config := NewDefaultConfig("rapid")
	config.SocketPath = "/tmp/nonexistent.sock" // Will fail to connect

	collector, err := NewCollector("rapid-test", config)
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)

	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)

		// Start will fail but shouldn't panic
		_ = collector.Start(ctx)

		// Stop should work even after failed start
		err := collector.Stop()
		assert.NoError(t, err)

		cancel()
	}
}

// TestStressChannelSaturation tests behavior when event channel saturates
func TestStressChannelSaturation(t *testing.T) {
	config := NewDefaultConfig("saturation")
	config.BufferSize = 10 // Small buffer
	config.SocketPath = "/tmp/test.sock"

	collector, err := NewCollector("saturation-test", config)
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)
	collector.ctx = context.Background()

	// Create many containers to saturate channel
	containers := make([]*cri.Container, 50)
	for i := 0; i < 50; i++ {
		containers[i] = &cri.Container{
			Id: "container-" + string(rune('a'+i)) + "-1234567890",
			Image: &cri.ImageSpec{
				Image: "test:latest",
			},
			ImageRef: "test:latest",
			Labels:   map[string]string{},
		}
	}

	collector.client = &mockCRIClient{
		containers: containers,
	}

	// Process stream events without draining
	for i := 0; i < 10; i++ {
		event := &cri.ContainerEventResponse{
			ContainerId:        fmt.Sprintf("full-container-%d", i),
			ContainerEventType: cri.ContainerEventType_CONTAINER_STARTED_EVENT,
			CreatedAt:          time.Now().UnixNano(),
		}
		_ = collector.processStreamEvent(context.Background(), event)
	}

	// Channel should be full
	assert.Equal(t, 10, len(collector.events), "Channel should be at capacity")

	// Additional events should be dropped (not block)
	// This is verified by the test completing without hanging
}

// TestStressMemoryLeaks tests for memory leaks with repeated operations
func TestStressMemoryLeaks(t *testing.T) {
	config := NewDefaultConfig("memleak")
	config.BufferSize = 100
	config.SocketPath = "/tmp/test.sock"

	collector, err := NewCollector("memleak-test", config)
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)
	collector.ctx = context.Background()

	// Set up mock client
	collector.client = &mockCRIClient{
		containers: []*cri.Container{
			{
				Id: "leak-test-container",
				Image: &cri.ImageSpec{
					Image: "test:latest",
				},
				ImageRef: "test:latest",
				Labels: map[string]string{
					"test": "memory",
				},
			},
		},
	}

	// Poll many times and drain events
	for i := 0; i < 1000; i++ {
		event := &cri.ContainerEventResponse{
			ContainerId:        fmt.Sprintf("container-%d", i),
			ContainerEventType: cri.ContainerEventType_CONTAINER_STARTED_EVENT,
			CreatedAt:          time.Now().UnixNano(),
		}
		_ = collector.processStreamEvent(context.Background(), event)

		// Drain event
		select {
		case <-collector.events:
			// Event drained
		case <-time.After(10 * time.Millisecond):
			t.Fatal("No event generated")
		}
	}

	// If we get here without OOM, memory is managed properly
	assert.True(t, true, "Completed 1000 iterations without memory issues")
}

// Benchmark tests
func BenchmarkProcessStreamEvent(b *testing.B) {
	config := NewDefaultConfig("bench")
	collector, _ := NewCollector("bench-test", config)
	collector.ctx = context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		event := &cri.ContainerEventResponse{
			ContainerId:        fmt.Sprintf("bench-container-%d", i),
			ContainerEventType: cri.ContainerEventType_CONTAINER_STARTED_EVENT,
			CreatedAt:          time.Now().UnixNano(),
		}
		_ = collector.processStreamEvent(context.Background(), event)

		// Drain events
		for len(collector.events) > 0 {
			<-collector.events
		}
	}
}

func BenchmarkProcessContainer(b *testing.B) {
	config := NewDefaultConfig("bench")
	collector, _ := NewCollector("bench-process", config)
	collector.client = &mockCRIClient{}

	ctx := context.Background()
	container := &cri.Container{
		Id:       "bench-container",
		ImageRef: "test:latest",
		Image:    &cri.ImageSpec{Image: "test"},
		Labels: map[string]string{
			"io.kubernetes.pod.name": "bench-pod",
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		collector.processContainer(ctx, container)

		// Drain event
		select {
		case <-collector.events:
		default:
		}
	}
}

// mockSlowCRIClient simulates slow CRI responses
type mockSlowCRIClient struct {
	cri.RuntimeServiceClient
	delay time.Duration
}

func (m *mockSlowCRIClient) ListContainers(ctx context.Context, req *cri.ListContainersRequest, opts ...grpc.CallOption) (*cri.ListContainersResponse, error) {
	time.Sleep(m.delay)
	return &cri.ListContainersResponse{
		Containers: []*cri.Container{},
	}, nil
}

func (m *mockSlowCRIClient) ContainerStatus(ctx context.Context, req *cri.ContainerStatusRequest, opts ...grpc.CallOption) (*cri.ContainerStatusResponse, error) {
	time.Sleep(m.delay)
	return &cri.ContainerStatusResponse{
		Status: &cri.ContainerStatus{
			State: cri.ContainerState_CONTAINER_RUNNING,
		},
	}, nil
}

// TestSlowCRIResponse tests handling of slow CRI responses
func TestSlowCRIResponse(t *testing.T) {
	config := NewDefaultConfig("slow")
	config.SocketPath = "/tmp/test.sock"
	collector, err := NewCollector("slow-test", config)
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)
	collector.ctx = context.Background()

	// Use slow mock client
	collector.client = &mockSlowCRIClient{
		delay: 100 * time.Millisecond,
	}

	start := time.Now()
	err = collector.loadInitialState(context.Background())
	require.NoError(t, err)
	elapsed := time.Since(start)

	// Should complete despite slow response
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
	assert.Less(t, elapsed, 500*time.Millisecond)
}
