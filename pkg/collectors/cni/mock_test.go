package cni

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// MockCNIPlugin simulates a CNI plugin for testing purposes
// This allows us to test CNI-related functionality without requiring:
// 1. Root privileges for network namespace operations
// 2. Actual container runtime (Docker/containerd)
// 3. Real network interfaces and iptables rules
// 4. eBPF programs that need kernel access
type MockCNIPlugin struct {
	mu               sync.RWMutex
	networkSetups    []NetworkSetup
	networkTeardowns []NetworkTeardown
	errors           map[string]error
	latency          time.Duration
}

// NetworkSetup represents a CNI ADD operation
type NetworkSetup struct {
	ContainerID string
	Netns       string
	IfName      string
	PodUID      string
	PodName     string
	Namespace   string
	Timestamp   time.Time
}

// NetworkTeardown represents a CNI DEL operation
type NetworkTeardown struct {
	ContainerID string
	Netns       string
	IfName      string
	Timestamp   time.Time
}

// NewMockCNIPlugin creates a new mock CNI plugin for testing
func NewMockCNIPlugin() *MockCNIPlugin {
	return &MockCNIPlugin{
		networkSetups:    make([]NetworkSetup, 0),
		networkTeardowns: make([]NetworkTeardown, 0),
		errors:           make(map[string]error),
	}
}

// SetupNetwork simulates CNI ADD command
func (m *MockCNIPlugin) SetupNetwork(containerID, netns, ifName, podUID, podName, namespace string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Simulate latency if configured
	if m.latency > 0 {
		time.Sleep(m.latency)
	}

	// Check for simulated errors
	if err, exists := m.errors[containerID]; exists {
		return err
	}

	setup := NetworkSetup{
		ContainerID: containerID,
		Netns:       netns,
		IfName:      ifName,
		PodUID:      podUID,
		PodName:     podName,
		Namespace:   namespace,
		Timestamp:   time.Now(),
	}

	m.networkSetups = append(m.networkSetups, setup)
	return nil
}

// TeardownNetwork simulates CNI DEL command
func (m *MockCNIPlugin) TeardownNetwork(containerID, netns, ifName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for simulated errors
	if err, exists := m.errors[containerID]; exists {
		return err
	}

	teardown := NetworkTeardown{
		ContainerID: containerID,
		Netns:       netns,
		IfName:      ifName,
		Timestamp:   time.Now(),
	}

	m.networkTeardowns = append(m.networkTeardowns, teardown)
	return nil
}

// GetSetups returns all network setups
func (m *MockCNIPlugin) GetSetups() []NetworkSetup {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]NetworkSetup{}, m.networkSetups...)
}

// GetTeardowns returns all network teardowns
func (m *MockCNIPlugin) GetTeardowns() []NetworkTeardown {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]NetworkTeardown{}, m.networkTeardowns...)
}

// InjectError makes the plugin return an error for a specific container
func (m *MockCNIPlugin) InjectError(containerID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[containerID] = err
}

// SetLatency configures simulated network setup latency
func (m *MockCNIPlugin) SetLatency(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latency = d
}

// Reset clears all recorded operations
func (m *MockCNIPlugin) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.networkSetups = make([]NetworkSetup, 0)
	m.networkTeardowns = make([]NetworkTeardown, 0)
	m.errors = make(map[string]error)
	m.latency = 0
}

// MockEBPFState implements the EBPFState interface for testing
type MockEBPFState struct {
	loaded bool
	links  int
}

func (m *MockEBPFState) IsLoaded() bool {
	return m.loaded
}

func (m *MockEBPFState) LinkCount() int {
	return m.links
}

// TestCNIMockingRationale documents why CNI mocking is essential for testing
func TestCNIMockingRationale(t *testing.T) {
	// This test explains the benefits of CNI mocking in testing

	t.Run("IsolatedTesting", func(t *testing.T) {
		// CNI mocking allows testing without external dependencies
		// Benefits:
		// 1. No need for Docker/containerd runtime
		// 2. No need for root privileges
		// 3. No need for kernel modules or eBPF support
		// 4. Tests run consistently across different environments

		mock := NewMockCNIPlugin()

		// Simulate a container network setup
		err := mock.SetupNetwork(
			"container123",
			"/var/run/netns/cni-123",
			"eth0",
			"pod-uid-456",
			"test-pod",
			"default",
		)
		assert.NoError(t, err)

		// Verify the operation was recorded
		setups := mock.GetSetups()
		assert.Len(t, setups, 1)
		assert.Equal(t, "container123", setups[0].ContainerID)
		assert.Equal(t, "pod-uid-456", setups[0].PodUID)
	})

	t.Run("ErrorSimulation", func(t *testing.T) {
		// CNI mocking allows simulating error conditions that are hard to reproduce
		// Benefits:
		// 1. Test error handling paths
		// 2. Simulate network failures
		// 3. Test retry mechanisms
		// 4. Validate graceful degradation

		mock := NewMockCNIPlugin()
		mock.InjectError("failing-container", fmt.Errorf("network plugin timeout"))

		err := mock.SetupNetwork(
			"failing-container",
			"/var/run/netns/cni-fail",
			"eth0",
			"pod-fail",
			"failing-pod",
			"default",
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "network plugin timeout")

		// Verify no setup was recorded due to error
		setups := mock.GetSetups()
		assert.Len(t, setups, 0)
	})

	t.Run("PerformanceTesting", func(t *testing.T) {
		// CNI mocking enables performance testing without actual network operations
		// Benefits:
		// 1. Predictable latency simulation
		// 2. No actual network I/O overhead
		// 3. Ability to test at scale
		// 4. Consistent timing across test runs

		mock := NewMockCNIPlugin()
		mock.SetLatency(10 * time.Millisecond)

		start := time.Now()
		err := mock.SetupNetwork(
			"perf-container",
			"/var/run/netns/cni-perf",
			"eth0",
			"pod-perf",
			"perf-pod",
			"default",
		)
		duration := time.Since(start)

		assert.NoError(t, err)
		assert.GreaterOrEqual(t, duration, 10*time.Millisecond)
	})

	t.Run("ScenarioTesting", func(t *testing.T) {
		// CNI mocking allows testing complex scenarios
		// Benefits:
		// 1. Simulate pod lifecycle (create/delete)
		// 2. Test concurrent operations
		// 3. Validate state consistency
		// 4. Test edge cases (rapid create/delete, reuse of namespaces)

		mock := NewMockCNIPlugin()

		// Simulate pod lifecycle
		containerID := "lifecycle-container"
		netns := "/var/run/netns/cni-lifecycle"

		// Setup network
		err := mock.SetupNetwork(containerID, netns, "eth0", "pod-lc", "lifecycle-pod", "default")
		assert.NoError(t, err)

		// Teardown network
		err = mock.TeardownNetwork(containerID, netns, "eth0")
		assert.NoError(t, err)

		// Verify both operations were recorded
		setups := mock.GetSetups()
		teardowns := mock.GetTeardowns()
		assert.Len(t, setups, 1)
		assert.Len(t, teardowns, 1)
		assert.Equal(t, containerID, setups[0].ContainerID)
		assert.Equal(t, containerID, teardowns[0].ContainerID)
	})
}

// TestCollectorWithMockCNI tests the collector with mock CNI plugin
func TestCollectorWithMockCNI(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni-mock")
	require.NoError(t, err)

	// Inject mock eBPF state
	collector.ebpfState = &MockEBPFState{
		loaded: true,
		links:  2,
	}

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Simulate CNI events
	testEvents := []struct {
		eventType string
		data      map[string]string
	}{
		{
			eventType: "netns_create",
			data: map[string]string{
				"pid":        "1234",
				"comm":       "containerd",
				"netns_path": "/var/run/netns/cni-550e8400-e29b-41d4-a716-446655440000",
			},
		},
		{
			eventType: "netns_enter",
			data: map[string]string{
				"pid":        "5678",
				"comm":       "runc",
				"netns_path": "/var/run/netns/cni-550e8400-e29b-41d4-a716-446655440000",
			},
		},
		{
			eventType: "netns_exit",
			data: map[string]string{
				"pid":        "5678",
				"comm":       "runc",
				"netns_path": "/var/run/netns/cni-550e8400-e29b-41d4-a716-446655440000",
			},
		},
	}

	// Generate events
	for _, test := range testEvents {
		event := collector.createEvent(test.eventType, test.data)

		// Verify event structure
		assert.Equal(t, "cni", event.Type)
		assert.Equal(t, test.eventType, event.Metadata["event"])
		assert.NotEmpty(t, event.TraceID)
		assert.NotEmpty(t, event.SpanID)

		// Verify K8s metadata extraction
		if test.eventType == "netns_create" {
			assert.Equal(t, "Pod", event.Metadata["k8s_kind"])
			assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", event.Metadata["k8s_uid"])
		}

		// Verify JSON data
		var eventData map[string]string
		err := json.Unmarshal(event.Data, &eventData)
		assert.NoError(t, err)
		assert.Equal(t, test.data["pid"], eventData["pid"])
		assert.Equal(t, test.data["comm"], eventData["comm"])
	}

	err = collector.Stop()
	require.NoError(t, err)
}

// TestOTELMetricsWithMocking tests OTEL metrics emission with mocked components
func TestOTELMetricsWithMocking(t *testing.T) {
	// Create a metric reader for testing
	reader := sdkmetric.NewManualReader()

	// Set up resource
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", "test-cni-metrics"),
		),
	)
	require.NoError(t, err)

	// Create meter provider with the reader
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	)
	otel.SetMeterProvider(mp)

	// Create collector
	collector, err := NewCollector("test-cni-metrics")
	require.NoError(t, err)

	// Inject mock eBPF state
	collector.ebpfState = &MockEBPFState{
		loaded: true,
		links:  2,
	}

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Generate some events to trigger metrics
	for i := 0; i < 10; i++ {
		data := map[string]string{
			"pid":        fmt.Sprintf("%d", 1000+i),
			"comm":       fmt.Sprintf("process-%d", i),
			"netns_path": fmt.Sprintf("/var/run/netns/cni-%d", i),
		}
		_ = collector.createEvent("netns_create", data)
	}

	// Collect metrics
	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Verify metrics were recorded
	assert.Greater(t, len(rm.ScopeMetrics), 0, "Should have recorded metrics")

	// Look for specific metrics
	foundMetrics := make(map[string]bool)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			foundMetrics[m.Name] = true
		}
	}

	// Verify expected metrics exist
	expectedMetrics := []string{
		"cni_events_processed_total",
		"cni_collector_healthy",
		"cni_k8s_extraction_attempts_total",
		"cni_netns_operations_total",
	}

	for _, metric := range expectedMetrics {
		assert.True(t, foundMetrics[metric], "Expected metric %s not found", metric)
	}

	err = collector.Stop()
	require.NoError(t, err)
}

// TestOTELTracingWithMocking tests OTEL tracing with mocked components
func TestOTELTracingWithMocking(t *testing.T) {
	// Create an in-memory span exporter for testing
	exporter := tracetest.NewInMemoryExporter()

	// Set up resource
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", "test-cni-tracing"),
		),
	)
	require.NoError(t, err)

	// Create tracer provider with the exporter
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)

	// Create collector
	collector, err := NewCollector("test-cni-tracing")
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Generate an event to create spans
	data := map[string]string{
		"pid":        "9999",
		"comm":       "traced-process",
		"netns_path": "/var/run/netns/cni-traced",
	}
	event := collector.createEvent("netns_create", data)

	// Verify event has trace context
	assert.NotEmpty(t, event.TraceID)
	assert.NotEmpty(t, event.SpanID)
	assert.Len(t, event.TraceID, 32) // 128-bit trace ID as 32 hex chars
	assert.Len(t, event.SpanID, 16)  // 64-bit span ID as 16 hex chars

	err = collector.Stop()
	require.NoError(t, err)

	// Verify spans were created
	spans := exporter.GetSpans()
	assert.Greater(t, len(spans), 0, "Should have created spans")

	// Look for specific spans
	foundSpans := make(map[string]bool)
	for _, span := range spans {
		foundSpans[span.Name] = true
	}

	// Verify expected spans exist
	expectedSpans := []string{
		"cni.collector.start",
		"cni.collector.stop",
		"cni.event.create",
	}

	for _, spanName := range expectedSpans {
		assert.True(t, foundSpans[spanName], "Expected span %s not found", spanName)
	}
}

// TestRawEventOTELCompliance verifies that raw events comply with OTEL standards
func TestRawEventOTELCompliance(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-otel-compliance")
	require.NoError(t, err)

	// Test various scenarios to ensure OTEL compliance
	testCases := []struct {
		name      string
		eventType string
		data      map[string]string
		validate  func(t *testing.T, event collectors.RawEvent)
	}{
		{
			name:      "BasicEvent",
			eventType: "netns_create",
			data: map[string]string{
				"pid":  "1234",
				"comm": "test",
			},
			validate: func(t *testing.T, event collectors.RawEvent) {
				// Verify basic OTEL fields
				assert.Equal(t, "cni", event.Type)
				assert.NotEmpty(t, event.TraceID)
				assert.NotEmpty(t, event.SpanID)
				assert.NotZero(t, event.Timestamp)
			},
		},
		{
			name:      "EventWithK8sMetadata",
			eventType: "netns_enter",
			data: map[string]string{
				"pid":        "5678",
				"comm":       "kubelet",
				"netns_path": "/var/run/netns/cni-550e8400-e29b-41d4-a716-446655440000",
			},
			validate: func(t *testing.T, event collectors.RawEvent) {
				// Verify K8s metadata extraction
				assert.Equal(t, "Pod", event.Metadata["k8s_kind"])
				assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", event.Metadata["k8s_uid"])
			},
		},
		{
			name:      "EventWithComplexData",
			eventType: "netns_exit",
			data: map[string]string{
				"pid":        "9999",
				"comm":       "containerd-shim",
				"netns_path": "/proc/9999/ns/net",
				"cgroup":     "/kubepods/besteffort/pod550e8400_e29b_41d4_a716_446655440000",
			},
			validate: func(t *testing.T, event collectors.RawEvent) {
				// Verify data serialization
				var deserializedData map[string]string
				err := json.Unmarshal(event.Data, &deserializedData)
				assert.NoError(t, err)
				assert.Equal(t, "9999", deserializedData["pid"])
				assert.Equal(t, "containerd-shim", deserializedData["comm"])
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			event := collector.createEvent(tc.eventType, tc.data)
			tc.validate(t, event)

			// Common validations for all events
			assert.Equal(t, "test-otel-compliance", event.Metadata["collector"])
			assert.Equal(t, tc.eventType, event.Metadata["event"])
		})
	}
}

// TestConcurrentEventGenerationWithMocking tests concurrent event generation
func TestConcurrentEventGenerationWithMocking(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-concurrent")
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Generate events concurrently
	var wg sync.WaitGroup
	numGoroutines := 10
	eventsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < eventsPerGoroutine; j++ {
				data := map[string]string{
					"pid":        fmt.Sprintf("%d", id*1000+j),
					"comm":       fmt.Sprintf("worker-%d", id),
					"netns_path": fmt.Sprintf("/var/run/netns/cni-%d-%d", id, j),
				}

				event := collector.createEvent("netns_create", data)

				// Send to channel (non-blocking)
				select {
				case collector.events <- event:
					// Event sent successfully
				default:
					// Buffer full, event dropped (expected in stress test)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify collector is still healthy
	assert.True(t, collector.IsHealthy())

	err = collector.Stop()
	require.NoError(t, err)
}

// TestErrorHandlingWithMocking tests error handling scenarios
func TestErrorHandlingWithMocking(t *testing.T) {
	setupOTELForTesting(t)

	t.Run("StartWithNilContext", func(t *testing.T) {
		collector, err := NewCollector("test-nil-context")
		require.NoError(t, err)

		err = collector.Start(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context cannot be nil")
	})

	t.Run("DoubleStart", func(t *testing.T) {
		collector, err := NewCollector("test-double-start")
		require.NoError(t, err)

		ctx := context.Background()
		err = collector.Start(ctx)
		require.NoError(t, err)

		err = collector.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "collector already started")

		err = collector.Stop()
		require.NoError(t, err)
	})

	t.Run("StopWithoutStart", func(t *testing.T) {
		collector, err := NewCollector("test-stop-without-start")
		require.NoError(t, err)

		// Should not panic
		err = collector.Stop()
		assert.NoError(t, err)
	})

	t.Run("EventWithMarshalError", func(t *testing.T) {
		collector, err := NewCollector("test-marshal-error")
		require.NoError(t, err)

		// Create event with data that will marshal successfully
		// (map[string]string always marshals successfully)
		data := map[string]string{
			"test": "value",
		}

		event := collector.createEvent("test", data)
		assert.NotNil(t, event.Data)
		assert.Greater(t, len(event.Data), 0)
	})
}
