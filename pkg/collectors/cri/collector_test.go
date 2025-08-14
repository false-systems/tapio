package cri

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// CRIServiceInterface defines the minimal interface needed for testing
type CRIServiceInterface interface {
	Version(context.Context, *cri.VersionRequest) (*cri.VersionResponse, error)
	ListContainers(context.Context, *cri.ListContainersRequest) (*cri.ListContainersResponse, error)
	ContainerStatus(context.Context, *cri.ContainerStatusRequest) (*cri.ContainerStatusResponse, error)
}

// MockCRIClient for testing - implements only the methods we need
type MockCRIClient struct {
	containers map[string]*cri.ContainerStatus
	mu         sync.RWMutex
	callCount  int
	healthy    bool
}

func NewMockCRIClient() *MockCRIClient {
	return &MockCRIClient{
		containers: make(map[string]*cri.ContainerStatus),
		healthy:    true,
	}
}

func (m *MockCRIClient) Connect() error {
	return nil
}

func (m *MockCRIClient) Close() error {
	return nil
}

func (m *MockCRIClient) ListContainers() ([]*cri.Container, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.callCount++

	var containers []*cri.Container
	for id := range m.containers {
		containers = append(containers, &cri.Container{Id: id})
	}

	return containers, nil
}

func (m *MockCRIClient) ContainerStatus(containerID string) (*cri.ContainerStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status, exists := m.containers[containerID]
	if !exists {
		// Return default status
		status = &cri.ContainerStatus{
			Id:    containerID,
			State: cri.ContainerState_CONTAINER_RUNNING,
		}
	}

	return status, nil
}

func (m *MockCRIClient) IsHealthy() bool {
	return m.healthy
}

// Helper methods for tests
func (m *MockCRIClient) AddContainer(id string, status *cri.ContainerStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.containers[id] = status
}

func (m *MockCRIClient) UpdateContainer(id string, status *cri.ContainerStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.containers[id] = status
}

func (m *MockCRIClient) RemoveContainer(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.containers, id)
}

func (m *MockCRIClient) GetCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.callCount
}

func (m *MockCRIClient) SetHealthy(healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthy = healthy
}

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
	}{
		{
			name: "valid default config",
			config: func() Config {
				config := DefaultConfig()
				config.SocketPath = "/tmp/test-cri.sock" // Use test socket
				return config
			}(),
			expectError: false,
		},
		{
			name: "invalid config - zero event buffer",
			config: Config{
				Name:            "test",
				SocketPath:      "/tmp/test-cri.sock",
				EventBufferSize: 0,
			},
			expectError: true,
		},
		{
			name: "invalid config - invalid ring buffer size",
			config: Config{
				Name:            "test",
				SocketPath:      "/tmp/test-cri.sock",
				EventBufferSize: 1000,
				RingBufferSize:  1000, // Not power of 2
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector("test-collector", tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
				assert.Equal(t, "test-collector", collector.name)
				assert.NotNil(t, collector.ringBuffer)
				assert.NotNil(t, collector.eventPool)
				assert.NotNil(t, collector.metrics)
			}
		})
	}
}

func TestCollectorStartStop(t *testing.T) {
	config := DefaultConfig()
	config.SocketPath = "/tmp/test.sock" // Use test socket

	collector, err := NewCollector("test", config)
	require.NoError(t, err)

	// Mock the CRI client by creating a compatible interface
	// Note: In a production test, we'd use dependency injection instead
	// For now, skip the direct client replacement since it requires full interface implementation
	collector.socket = "/tmp/mock.sock"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test start
	err = collector.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, collector.isRunning.Load())

	// Test double start (should fail)
	err = collector.Start(ctx)
	assert.Error(t, err)

	// Test stop
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.isRunning.Load())

	// Test double stop (should succeed)
	err = collector.Stop()
	assert.NoError(t, err)
}

func TestEventCreation(t *testing.T) {
	config := DefaultConfig()
	collector, err := NewCollector("test", config)
	require.NoError(t, err)

	// Create test container status
	status := &cri.ContainerStatus{
		Id:       "test-container-123",
		ExitCode: 137,
		Reason:   "OOMKilled",
		Labels: map[string]string{
			"io.kubernetes.pod.uid":       "pod-uid-123",
			"io.kubernetes.pod.name":      "test-pod",
			"io.kubernetes.pod.namespace": "default",
		},
	}

	// Test OOM event creation
	collector.createEvent(status, EventOOM)

	// Verify event was added to ring buffer
	event := collector.ringBuffer.Read()
	require.NotNil(t, event)

	assert.Equal(t, EventOOM, event.Type)
	assert.Equal(t, int32(137), event.ExitCode)
	assert.Equal(t, uint8(1), event.OOMKilled)
	assert.Equal(t, int32(9), event.Signal)
	assert.Equal(t, "test-pod", event.PodName)
	assert.Equal(t, "default", event.Namespace)
	assert.Equal(t, "test-container-123", event.GetContainerID())
	assert.Equal(t, "pod-uid-123", event.GetPodUID())

	// Return event to pool
	collector.eventPool.Put(event)
}

func TestEventPooling(t *testing.T) {
	pool := NewEventPool()

	// Get event from pool
	event1 := pool.Get()
	assert.NotNil(t, event1)

	// Modify event
	event1.SetContainerID("test-id")
	event1.Type = EventOOM
	event1.ExitCode = 137

	// Return to pool
	pool.Put(event1)

	// Get another event (should be reset)
	event2 := pool.Get()
	assert.NotNil(t, event2)

	// Should be reset
	assert.Equal(t, "", event2.GetContainerID())
	assert.Equal(t, EventType(0), event2.Type)
	assert.Equal(t, int32(0), event2.ExitCode)
}

func TestRingBuffer(t *testing.T) {
	buffer := NewRingBuffer()

	// Test empty buffer
	event := buffer.Read()
	assert.Nil(t, event)
	assert.Equal(t, 0.0, buffer.Usage())

	// Create test event
	testEvent := &Event{
		Type:     EventOOM,
		ExitCode: 137,
	}

	// Test write
	ok := buffer.Write(testEvent)
	assert.True(t, ok)
	assert.Greater(t, buffer.Usage(), 0.0)

	// Test read
	readEvent := buffer.Read()
	assert.Equal(t, testEvent, readEvent)
	assert.Equal(t, 0.0, buffer.Usage())

	// Test buffer overflow
	for i := 0; i < RingBufferSize+10; i++ {
		buffer.Write(&Event{})
	}

	// Should drop events when full
	ok = buffer.Write(&Event{})
	assert.False(t, ok) // Buffer should be full
}

func TestContainerFiltering(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		container *ContainerInfo
		expected  bool
	}{
		{
			name: "kubernetes container included",
			config: Config{
				KubernetesOnly: true,
			},
			container: &ContainerInfo{
				Labels: map[string]string{
					"io.kubernetes.pod.uid": "test-uid",
				},
			},
			expected: true,
		},
		{
			name: "non-kubernetes container excluded",
			config: Config{
				KubernetesOnly: true,
			},
			container: &ContainerInfo{
				Labels: map[string]string{},
			},
			expected: false,
		},
		{
			name: "system container excluded",
			config: Config{
				ExcludeSystemContainers: true,
			},
			container: &ContainerInfo{
				Name:  "k8s_POD_test",
				Image: "pause",
			},
			expected: false,
		},
		{
			name: "namespace filtering - included",
			config: Config{
				IncludeNamespaces: []string{"default", "app"},
			},
			container: &ContainerInfo{
				Labels: map[string]string{
					"io.kubernetes.pod.namespace": "default",
				},
			},
			expected: true,
		},
		{
			name: "namespace filtering - excluded",
			config: Config{
				ExcludeNamespaces: []string{"kube-system"},
			},
			container: &ContainerInfo{
				Labels: map[string]string{
					"io.kubernetes.pod.namespace": "kube-system",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.ShouldIncludeContainer(tt.container)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHealthCheck(t *testing.T) {
	config := DefaultConfig()
	config.SocketPath = "/tmp/test-health.sock" // Use test socket
	collector, err := NewCollector("test", config)
	require.NoError(t, err)

	// Mock client - skip full interface mocking for now
	// In production, we'd use dependency injection or interface abstraction

	// Test healthy state
	healthy, metrics := collector.Health()
	assert.False(t, healthy) // Not running yet
	assert.Contains(t, metrics, "status")

	// Start collector
	collector.isRunning.Store(true)
	healthy, metrics = collector.Health()
	assert.True(t, healthy)
	assert.Equal(t, "healthy", metrics["status"])

	// Test unhealthy state - skip mocking for now
	// In production tests, we'd mock the CRI socket or use dependency injection
	assert.Contains(t, metrics, "events_processed")
	assert.Contains(t, metrics, "buffer_usage")
}

func TestMetrics(t *testing.T) {
	metrics := &Metrics{}

	// Test initial state
	stats := metrics.GetMetrics()
	assert.Equal(t, uint64(0), stats["events_processed"])
	assert.Equal(t, uint64(0), stats["oom_kills_detected"])

	// Update metrics
	metrics.EventsProcessed.Add(10)
	metrics.OOMKillsDetected.Add(2)

	stats = metrics.GetMetrics()
	assert.Equal(t, uint64(10), stats["events_processed"])
	assert.Equal(t, uint64(2), stats["oom_kills_detected"])
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
	}{
		{
			name:        "valid default config",
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name: "invalid ring buffer size",
			config: Config{
				Name:                "test",
				EventBufferSize:     1000,
				BatchSize:           100,
				RingBufferSize:      1000, // Not power of 2
				PollInterval:        100 * time.Millisecond,
				FlushInterval:       100 * time.Millisecond,
				HealthCheckInterval: 30 * time.Second,
				HealthCheckTimeout:  5 * time.Second,
			},
			expectError: true,
		},
		{
			name: "batch size too large",
			config: Config{
				Name:                "test",
				EventBufferSize:     100,
				BatchSize:           200, // Larger than buffer
				RingBufferSize:      1024,
				PollInterval:        100 * time.Millisecond,
				FlushInterval:       100 * time.Millisecond,
				HealthCheckInterval: 30 * time.Second,
				HealthCheckTimeout:  5 * time.Second,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConcurrentAccess(t *testing.T) {
	config := DefaultConfig()
	collector, err := NewCollector("test", config)
	require.NoError(t, err)

	// Test concurrent ring buffer access
	var wg sync.WaitGroup
	numGoroutines := 10
	eventsPerGoroutine := 100

	// Writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				event := collector.eventPool.Get()
				event.Type = EventOOM
				event.ExitCode = int32(id*100 + j)

				if !collector.ringBuffer.Write(event) {
					collector.eventPool.Put(event) // Return to pool if write failed
				}
			}
		}(i)
	}

	// Readers
	var readCount int32
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				event := collector.ringBuffer.Read()
				if event == nil {
					time.Sleep(time.Millisecond) // Brief pause
					continue
				}

				// Validate event
				assert.Equal(t, EventOOM, event.Type)
				assert.GreaterOrEqual(t, event.ExitCode, int32(0))

				collector.eventPool.Put(event)
				readCount++

				if readCount >= int32(numGoroutines*eventsPerGoroutine) {
					return
				}
			}
		}()
	}

	wg.Wait()

	// Verify metrics
	assert.Greater(t, collector.metrics.EventsProcessed.Load(), uint64(0))
}

// Benchmarks

func BenchmarkEventCreation(b *testing.B) {
	config := DefaultConfig()
	collector, _ := NewCollector("bench", config)

	status := &cri.ContainerStatus{
		Id:       "benchmark-container-123456789",
		ExitCode: 137,
		Labels: map[string]string{
			"io.kubernetes.pod.uid":       "benchmark-pod-uid-123456789",
			"io.kubernetes.pod.name":      "benchmark-pod",
			"io.kubernetes.pod.namespace": "benchmark",
		},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			collector.createEvent(status, EventOOM)

			// Read and return to pool to complete cycle
			if event := collector.ringBuffer.Read(); event != nil {
				collector.eventPool.Put(event)
			}
		}
	})
}

func BenchmarkRingBuffer(b *testing.B) {
	buffer := NewRingBuffer()
	events := make([]*Event, 1000)

	// Pre-allocate events
	for i := range events {
		events[i] = &Event{
			Type:     EventOOM,
			ExitCode: int32(i),
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			event := events[i%len(events)]

			// Write
			if !buffer.Write(event) {
				// Buffer full, read some events
				for j := 0; j < 10; j++ {
					if buffer.Read() == nil {
						break
					}
				}
				buffer.Write(event)
			}

			i++
		}
	})
}

func BenchmarkEventPool(b *testing.B) {
	pool := NewEventPool()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			event := pool.Get()
			event.SetContainerID("test-container")
			event.Type = EventOOM
			pool.Put(event)
		}
	})
}

func BenchmarkContainerFiltering(b *testing.B) {
	config := Config{
		KubernetesOnly:          true,
		ExcludeSystemContainers: true,
		ExcludeNamespaces:       []string{"kube-system", "kube-public"},
	}

	containers := []*ContainerInfo{
		{
			Name: "app-container",
			Labels: map[string]string{
				"io.kubernetes.pod.uid":       "test-uid",
				"io.kubernetes.pod.namespace": "default",
			},
		},
		{
			Name:  "pause-container",
			Image: "registry.k8s.io/pause:3.9",
			Labels: map[string]string{
				"io.kubernetes.container.name": "POD",
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		container := containers[i%len(containers)]
		_ = config.ShouldIncludeContainer(container)
	}
}

// Test helper functions

func createTestContainer(id, name, namespace string, isSystem bool) *ContainerInfo {
	container := &ContainerInfo{
		ID:   id,
		Name: name,
		Labels: map[string]string{
			"io.kubernetes.pod.uid":       "test-uid-" + id,
			"io.kubernetes.pod.name":      name,
			"io.kubernetes.pod.namespace": namespace,
		},
	}

	if isSystem {
		container.Name = "k8s_POD_" + name
		container.Image = "registry.k8s.io/pause:3.9"
		container.Labels["io.kubernetes.container.name"] = "POD"
	}

	return container
}

func createOOMEvent(containerID, podName, namespace string) *Event {
	event := &Event{
		Type:        EventOOM,
		ExitCode:    137,
		Signal:      9,
		OOMKilled:   1,
		PodName:     podName,
		Namespace:   namespace,
		Timestamp:   time.Now().UnixNano(),
		MemoryUsage: 2 * 1024 * 1024 * 1024,   // 2GB
		MemoryLimit: 1.5 * 1024 * 1024 * 1024, // 1.5GB
		Reason:      "OOMKilled",
		Message:     "Container killed due to OOM",
	}
	event.SetContainerID(containerID)
	return event
}

// OTEL Integration Tests

func TestOTELCollectorInstrumentation(t *testing.T) {
	// Setup in-memory tracer for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	otel.SetTracerProvider(tp)

	// Setup in-memory meter for testing
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(
		metric.WithReader(reader),
	)
	otel.SetMeterProvider(mp)

	config := DefaultConfig()
	config.TracingEnabled = true
	config.MetricsEnabled = true
	config.SocketPath = "/tmp/otel-test.sock"

	collector, err := NewCollector("otel-test-cri", config)
	require.NoError(t, err)
	require.NotNil(t, collector.tracer)
	require.NotNil(t, collector.meter)

	ctx := context.Background()

	// Test that metrics instruments are created
	assert.NotNil(t, collector.eventsProcessed)
	assert.NotNil(t, collector.eventsDropped)
	assert.NotNil(t, collector.oomKillsDetected)
	assert.NotNil(t, collector.processingLatency)
	assert.NotNil(t, collector.activeContainers)
	assert.NotNil(t, collector.criErrors)
	assert.NotNil(t, collector.batchSize)
	assert.NotNil(t, collector.checksPerformed)
	assert.NotNil(t, collector.bufferUsage)

	// Test that we can record metrics
	collector.eventsProcessed.Add(ctx, 5)
	collector.oomKillsDetected.Add(ctx, 2)
	collector.activeContainers.Add(ctx, 10)

	// Read metrics
	metricData := &metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, metricData)
	assert.NoError(t, err)

	// Cleanup
	tp.Shutdown(ctx)
	mp.Shutdown(ctx)
}

func TestOTELEBPFInstrumentation(t *testing.T) {
	// Skip if not Linux
	if runtime.GOOS != "linux" {
		t.Skip("eBPF collector only available on Linux")
	}

	// Setup test tracer
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	otel.SetTracerProvider(tp)

	config := DefaultConfig()
	config.EnableEBPF = true
	config.TracingEnabled = true
	config.MetricsEnabled = true

	// Create eBPF collector (will fallback to regular if eBPF unavailable)
	collector, err := NewEBPFCollector("ebpf-test", config)
	if err != nil {
		t.Skip("eBPF not available in test environment")
	}

	require.NotNil(t, collector.tracer)
	require.NotNil(t, collector.meter)

	// Test eBPF-specific metrics
	assert.NotNil(t, collector.ebpfLoadsTotal)
	assert.NotNil(t, collector.ebpfLoadErrors)
	assert.NotNil(t, collector.ebpfAttachTotal)
	assert.NotNil(t, collector.ebpfAttachErrors)
	assert.NotNil(t, collector.ebpfEventsTotal)
	assert.NotNil(t, collector.ebpfEventsDropped)
	assert.NotNil(t, collector.kernelOOMKills)

	ctx := context.Background()

	// Test metric recording
	collector.ebpfLoadsTotal.Add(ctx, 1)
	collector.kernelOOMKills.Add(ctx, 3)

	// Verify spans were created during initialization
	spans := exporter.GetSpans()
	assert.Greater(t, len(spans), 0)

	// Check for expected span names
	spanNames := make([]string, len(spans))
	for i, span := range spans {
		spanNames[i] = span.Name
	}

	// Should contain eBPF initialization spans
	assert.Contains(t, spanNames, "cri.ebpf.initialize")

	// Cleanup
	tp.Shutdown(ctx)
}

func TestOTELConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
	}{
		{
			name: "valid OTEL config",
			config: Config{
				Name:                "test",
				TracingEnabled:      true,
				TracingSampleRate:   0.5,
				MetricsEnabled:      true,
				MetricsInterval:     30 * time.Second,
				OTLPEndpoint:        "localhost:4317",
				SpanBufferSize:      2048,
				SpanBatchTimeout:    5 * time.Second,
				SpanBatchSize:       512,
				EventBufferSize:     1000,
				BatchSize:           100,
				RingBufferSize:      1024,
				PollInterval:        100 * time.Millisecond,
				FlushInterval:       100 * time.Millisecond,
				HealthCheckInterval: 30 * time.Second,
				HealthCheckTimeout:  5 * time.Second,
			},
			expectError: false,
		},
		{
			name: "invalid sample rate",
			config: Config{
				Name:                "test",
				TracingEnabled:      true,
				TracingSampleRate:   1.5, // Invalid: > 1.0
				MetricsEnabled:      true,
				OTLPEndpoint:        "localhost:4317",
				EventBufferSize:     1000,
				BatchSize:           100,
				RingBufferSize:      1024,
				PollInterval:        100 * time.Millisecond,
				FlushInterval:       100 * time.Millisecond,
				HealthCheckInterval: 30 * time.Second,
				HealthCheckTimeout:  5 * time.Second,
			},
			expectError: true,
		},
		{
			name: "invalid span buffer size",
			config: Config{
				Name:                "test",
				TracingEnabled:      true,
				TracingSampleRate:   0.1,
				SpanBufferSize:      0, // Invalid: must be positive
				OTLPEndpoint:        "localhost:4317",
				EventBufferSize:     1000,
				BatchSize:           100,
				RingBufferSize:      1024,
				PollInterval:        100 * time.Millisecond,
				FlushInterval:       100 * time.Millisecond,
				HealthCheckInterval: 30 * time.Second,
				HealthCheckTimeout:  5 * time.Second,
			},
			expectError: true,
		},
		{
			name: "missing OTLP endpoint",
			config: Config{
				Name:                "test",
				TracingEnabled:      true,
				TracingSampleRate:   0.1,
				MetricsEnabled:      true,
				OTLPEndpoint:        "", // Invalid: required when OTEL enabled
				EventBufferSize:     1000,
				BatchSize:           100,
				RingBufferSize:      1024,
				PollInterval:        100 * time.Millisecond,
				FlushInterval:       100 * time.Millisecond,
				HealthCheckInterval: 30 * time.Second,
				HealthCheckTimeout:  5 * time.Second,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOTELConfigOptimization(t *testing.T) {
	tests := []struct {
		name         string
		environment  string
		expectedLoad int
		checks       func(t *testing.T, config Config)
	}{
		{
			name:         "production optimization",
			environment:  "production",
			expectedLoad: 5000,
			checks: func(t *testing.T, config Config) {
				assert.Equal(t, 0.01, config.TracingSampleRate)
				assert.Equal(t, 5*time.Second, config.SpanBatchTimeout)
				assert.Equal(t, 1024, config.SpanBatchSize)
				assert.Equal(t, 30*time.Second, config.MetricsInterval)
			},
		},
		{
			name:         "high load production optimization",
			environment:  "production",
			expectedLoad: 15000,
			checks: func(t *testing.T, config Config) {
				assert.Equal(t, 0.001, config.TracingSampleRate) // Very low sampling
				assert.Equal(t, 8192, config.SpanBufferSize)     // Larger buffer
			},
		},
		{
			name:         "staging optimization",
			environment:  "staging",
			expectedLoad: 1000,
			checks: func(t *testing.T, config Config) {
				assert.Equal(t, 0.1, config.TracingSampleRate)
				assert.Equal(t, 3*time.Second, config.SpanBatchTimeout)
				assert.Equal(t, 15*time.Second, config.MetricsInterval)
			},
		},
		{
			name:         "development optimization",
			environment:  "development",
			expectedLoad: 100,
			checks: func(t *testing.T, config Config) {
				assert.Equal(t, 1.0, config.TracingSampleRate) // Full sampling
				assert.Equal(t, 1*time.Second, config.SpanBatchTimeout)
				assert.Equal(t, 10*time.Second, config.MetricsInterval)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.OptimizeOTELForEnvironment(tt.environment, tt.expectedLoad)

			assert.Equal(t, tt.environment, config.DeploymentEnvironment)
			tt.checks(t, config)
		})
	}
}

func TestOTELInitialization(t *testing.T) {
	config := DefaultConfig()
	config.TracingEnabled = true
	config.MetricsEnabled = true
	config.OTLPEndpoint = "localhost:4317"
	config.OTLPInsecure = true
	config.ServiceName = "test-cri-collector"
	config.ServiceVersion = "1.0.0-test"
	config.DeploymentEnvironment = "test"

	ctx := context.Background()

	// Test OTEL initialization
	shutdown, err := InitializeOTEL(ctx, &config)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Verify global providers are set
	tracer := otel.Tracer("test")
	assert.NotNil(t, tracer)

	meter := otel.Meter("test")
	assert.NotNil(t, meter)

	// Test shutdown
	shutdown()
}

func TestOTELEndpointValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
	}{
		{
			name: "valid endpoint",
			config: Config{
				TracingEnabled: true,
				OTLPEndpoint:   "localhost:4317",
			},
			expectError: false,
		},
		{
			name: "valid endpoint with scheme",
			config: Config{
				TracingEnabled: true,
				OTLPEndpoint:   "http://jaeger:14268",
			},
			expectError: false,
		},
		{
			name: "missing port",
			config: Config{
				TracingEnabled: true,
				OTLPEndpoint:   "localhost",
			},
			expectError: true,
		},
		{
			name: "missing endpoint with tracing enabled",
			config: Config{
				TracingEnabled: true,
				OTLPEndpoint:   "",
			},
			expectError: true,
		},
		{
			name: "missing endpoint but features disabled",
			config: Config{
				TracingEnabled: false,
				MetricsEnabled: false,
				OTLPEndpoint:   "",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.ValidateOTELEndpoint()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func BenchmarkOTELCollectorWithTracing(b *testing.B) {
	// Setup minimal tracing for benchmark
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(0.01)), // 1% sampling
	)
	otel.SetTracerProvider(tp)

	config := DefaultConfig()
	config.TracingEnabled = true
	config.TracingSampleRate = 0.01 // Low sampling for benchmark
	config.SocketPath = "/tmp/bench-cri.sock"

	collector, err := NewCollector("bench-cri", config)
	require.NoError(b, err)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate container state check with tracing
		collector.checksPerformed.Add(ctx, 1)
	}
}

func BenchmarkOTELEBPFEventProcessing(b *testing.B) {
	if runtime.GOOS != "linux" {
		b.Skip("eBPF only available on Linux")
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(0.001)), // Very low sampling
	)
	otel.SetTracerProvider(tp)

	config := DefaultConfig()
	config.EnableEBPF = true
	config.TracingEnabled = true
	config.TracingSampleRate = 0.001

	collector, err := NewEBPFCollector("bench-ebpf", config)
	if err != nil {
		b.Skip("eBPF not available in test environment")
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate eBPF event processing with tracing
		collector.ebpfEventsTotal.Add(ctx, 1)
	}
}
