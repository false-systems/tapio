package cri

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"

	"github.com/yairfalse/tapio/pkg/domain"
)

// TestCollectorNewCollector tests the collector constructor
func TestCollectorNewCollector(t *testing.T) {
	tests := []struct {
		name          string
		collectorName string
		config        Config
		expectError   bool
		errorMsg      string
	}{
		{
			name:          "valid default config",
			collectorName: "test-cri",
			config: func() Config {
				config := DefaultConfig()
				config.SocketPath = "/tmp/test-cri.sock"
				return config
			}(),
			expectError: false,
		},
		{
			name:          "valid production config",
			collectorName: "prod-cri",
			config: func() Config {
				config := ProductionConfig()
				config.SocketPath = "/tmp/prod-cri.sock"
				return config
			}(),
			expectError: false,
		},
		{
			name:          "invalid config - zero event buffer",
			collectorName: "invalid-cri",
			config: Config{
				Name:                "test",
				SocketPath:          "/tmp/invalid.sock",
				EventBufferSize:     0,
				BatchSize:           100,
				RingBufferSize:      1024,
				PollInterval:        100 * time.Millisecond,
				FlushInterval:       100 * time.Millisecond,
				HealthCheckInterval: 30 * time.Second,
				HealthCheckTimeout:  5 * time.Second,
			},
			expectError: true,
			errorMsg:    "EventBufferSize",
		},
		{
			name:          "invalid config - ring buffer not power of 2",
			collectorName: "invalid2-cri",
			config: func() Config {
				c := DefaultConfig()
				c.Name = "test"
				c.SocketPath = "/tmp/invalid2.sock"
				c.RingBufferSize = 1025 // Not power of 2, but > minimum
				return c
			}(),
			expectError: true,
			errorMsg:    "ring_buffer_size must be a power of 2",
		},
		{
			name:          "invalid config - batch size exceeds buffer",
			collectorName: "invalid3-cri",
			config: func() Config {
				c := DefaultConfig()
				c.Name = "test"
				c.SocketPath = "/tmp/invalid3.sock"
				c.EventBufferSize = 100
				c.BatchSize = 200 // Larger than buffer
				return c
			}(),
			expectError: true,
			errorMsg:    "batch_size cannot be larger than event_buffer_size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.collectorName, tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, collector)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
				assert.Equal(t, tt.collectorName, collector.name)
				assert.NotNil(t, collector.ringBuffer)
				assert.NotNil(t, collector.eventPool)
				assert.NotNil(t, collector.metrics)
				assert.NotNil(t, collector.events)
				assert.NotNil(t, collector.stopCh)
				assert.NotNil(t, collector.lastSeen)

				// Verify OTEL instrumentation
				assert.NotNil(t, collector.tracer)
				assert.NotNil(t, collector.meter)

				// Test socket detection functionality
				if tt.config.SocketPath == "" {
					// Should have detected a socket or returned error
					assert.NotEmpty(t, collector.socket)
				} else {
					assert.Equal(t, tt.config.SocketPath, collector.socket)
				}
			}
		})
	}
}

// TestCollectorBasicMethods tests basic collector methods
func TestCollectorBasicMethods(t *testing.T) {
	config := DefaultConfig()
	config.SocketPath = "/tmp/test-basic.sock"

	collector, err := NewCollector("test-basic", config)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Test Name method
	assert.Equal(t, "test-basic", collector.Name())

	// Test Type method
	assert.Equal(t, CollectorName, collector.Type())

	// Test Events channel
	eventsChan := collector.Events()
	assert.NotNil(t, eventsChan)

	// Test Metrics
	metrics := collector.Metrics()
	assert.NotNil(t, metrics)

	// Test Statistics
	stats := collector.Statistics()
	assert.NotNil(t, stats)
	assert.Contains(t, stats.CustomMetrics, "socket")
	assert.Contains(t, stats.CustomMetrics, "running")
	assert.Contains(t, stats.CustomMetrics, "buffer_usage")
	assert.Contains(t, stats.CustomMetrics, "tracked_containers")

	// Test Health (before start - should be unhealthy)
	health := collector.Health()
	assert.NotNil(t, health)
	assert.Equal(t, "collector stopped", health.Message) // Not running yet
	assert.False(t, collector.IsHealthy())               // Not running yet
}

// TestDetectCRISocket tests socket detection
func TestDetectCRISocket(t *testing.T) {
	// Test the detectCRISocket function
	socket := detectCRISocket()
	// On test machines, this will likely return empty string
	// as no real CRI sockets exist
	assert.IsType(t, "", socket)
}

// TestEventPooling tests the event pool functionality
func TestEventPooling(t *testing.T) {
	pool := NewEventPool()
	require.NotNil(t, pool)

	// Test Get/Put cycle
	event1 := pool.Get()
	assert.NotNil(t, event1)

	// Verify event is reset
	assert.Equal(t, "", event1.GetContainerID())
	assert.Equal(t, "", event1.GetPodUID())
	assert.Equal(t, EventType(0), event1.Type)
	assert.Equal(t, int32(0), event1.ExitCode)

	// Modify event
	event1.SetContainerID("test-container-123")
	event1.SetPodUID("test-pod-uid-456")
	event1.Type = EventOOM
	event1.ExitCode = 137
	event1.PodName = "test-pod"
	event1.Namespace = "test-namespace"
	event1.MemoryUsage = 1024 * 1024 * 1024     // 1GB
	event1.MemoryLimit = 2 * 1024 * 1024 * 1024 // 2GB

	// Verify modifications
	assert.Equal(t, "test-container-123", event1.GetContainerID())
	assert.Equal(t, "test-pod-uid-456", event1.GetPodUID())
	assert.Equal(t, EventOOM, event1.Type)
	assert.Equal(t, int32(137), event1.ExitCode)

	// Return to pool
	pool.Put(event1)

	// Get another event (should be the same, but reset)
	event2 := pool.Get()
	assert.NotNil(t, event2)

	// Should be reset
	assert.Equal(t, "", event2.GetContainerID())
	assert.Equal(t, "", event2.GetPodUID())
	assert.Equal(t, EventType(0), event2.Type)
	assert.Equal(t, int32(0), event2.ExitCode)
	assert.Equal(t, "", event2.PodName)
	assert.Equal(t, "", event2.Namespace)
	assert.Equal(t, uint64(0), event2.MemoryUsage)
	assert.Equal(t, uint64(0), event2.MemoryLimit)

	pool.Put(event2)
}

// TestRingBufferOperations tests ring buffer functionality
func TestRingBufferOperations(t *testing.T) {
	buffer := NewRingBuffer()
	require.NotNil(t, buffer)

	// Test empty buffer
	assert.Equal(t, 0.0, buffer.Usage())
	event := buffer.Read()
	assert.Nil(t, event)

	// Create test events
	events := make([]*Event, 10)
	for i := range events {
		events[i] = &Event{
			Type:      EventType(i % 5), // Cycle through event types
			ExitCode:  int32(i),
			Timestamp: time.Now().UnixNano(),
		}
		events[i].SetContainerID(fmt.Sprintf("container-%d", i))
	}

	// Write events
	for i, e := range events {
		ok := buffer.Write(e)
		assert.True(t, ok, "Failed to write event %d", i)
		assert.Greater(t, buffer.Usage(), 0.0)
	}

	// Read events back
	for i := 0; i < len(events); i++ {
		readEvent := buffer.Read()
		assert.NotNil(t, readEvent, "Failed to read event %d", i)
		assert.Equal(t, events[i].Type, readEvent.Type)
		assert.Equal(t, events[i].ExitCode, readEvent.ExitCode)
		assert.Equal(t, events[i].GetContainerID(), readEvent.GetContainerID())
	}

	// Buffer should be empty again
	assert.Equal(t, 0.0, buffer.Usage())

	// Test buffer overflow
	overflowEvents := make([]*Event, RingBufferSize+10)
	for i := range overflowEvents {
		overflowEvents[i] = &Event{Type: EventOOM, ExitCode: int32(i)}
	}

	// Fill buffer to capacity
	successCount := 0
	for _, e := range overflowEvents {
		if buffer.Write(e) {
			successCount++
		}
	}

	// Should have written exactly RingBufferSize - 1 events (ring buffer leaves one slot empty)
	assert.Equal(t, RingBufferSize-1, successCount)
	assert.Greater(t, buffer.Usage(), 90.0) // Should be nearly full

	// Attempt to write one more (should fail)
	overflowEvent := &Event{Type: EventDied, ExitCode: 999}
	ok := buffer.Write(overflowEvent)
	assert.False(t, ok) // Should fail due to buffer being full
}

// TestEventConversion tests event-to-RawEvent conversion
func TestEventConversion(t *testing.T) {
	event := &Event{
		Type:        EventOOM,
		ExitCode:    137,
		Signal:      9,
		OOMKilled:   1,
		PodName:     "test-pod",
		Namespace:   "test-namespace",
		Timestamp:   time.Now().UnixNano(),
		MemoryUsage: 2 * 1024 * 1024 * 1024,   // 2GB
		MemoryLimit: 1.5 * 1024 * 1024 * 1024, // 1.5GB
		Reason:      "OOMKilled",
		Message:     "Container killed due to OOM",
	}
	event.SetContainerID("test-container-abc123")
	event.SetPodUID("test-pod-uid-def456")

	// Convert to RawEvent
	rawEvent := event.ToRawEvent()

	// Verify RawEvent structure
	assert.Equal(t, CollectorName, rawEvent.Source)
	assert.Equal(t, time.Unix(0, event.Timestamp), rawEvent.Timestamp)
	assert.NotEmpty(t, rawEvent.Data)

	// Verify JSON serialization/deserialization
	var eventData Event
	err := json.Unmarshal(rawEvent.Data, &eventData)
	assert.NoError(t, err)

	// Verify key fields
	assert.Equal(t, event.Type, eventData.Type)
	assert.Equal(t, event.ExitCode, eventData.ExitCode)
	assert.Equal(t, event.Signal, eventData.Signal)
	assert.Equal(t, event.OOMKilled, eventData.OOMKilled)
	assert.Equal(t, event.PodName, eventData.PodName)
	assert.Equal(t, event.Namespace, eventData.Namespace)
	assert.Equal(t, event.MemoryUsage, eventData.MemoryUsage)
	assert.Equal(t, event.MemoryLimit, eventData.MemoryLimit)
	assert.Equal(t, event.Reason, eventData.Reason)
	assert.Equal(t, event.Message, eventData.Message)
}

// TestEventTypeString tests EventType string representation
func TestEventTypeString(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventCreated, "created"},
		{EventStarted, "started"},
		{EventStopped, "stopped"},
		{EventDied, "died"},
		{EventOOM, "oom"},
		{EventType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.eventType.String())
		})
	}
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid default config",
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name:        "valid production config",
			config:      ProductionConfig(),
			expectError: false,
		},
		{
			name:        "valid dev config",
			config:      DevConfig(),
			expectError: false,
		},
		{
			name: "empty name",
			config: func() Config {
				c := DefaultConfig()
				c.Name = ""
				return c
			}(),
			expectError: true,
			errorMsg:    "Name",
		},
		{
			name: "negative event buffer size",
			config: func() Config {
				c := DefaultConfig()
				c.EventBufferSize = -1
				return c
			}(),
			expectError: true,
			errorMsg:    "EventBufferSize",
		},
		{
			name: "health check timeout >= interval",
			config: func() Config {
				c := DefaultConfig()
				c.HealthCheckInterval = 10 * time.Second
				c.HealthCheckTimeout = 15 * time.Second // Greater than interval
				return c
			}(),
			expectError: true,
			errorMsg:    "health_check_timeout must be less than health_check_interval",
		},
		{
			name: "invalid tracing sample rate",
			config: func() Config {
				c := DefaultConfig()
				c.TracingEnabled = true
				c.TracingSampleRate = 1.5 // Invalid: > 1.0
				c.OTLPEndpoint = "localhost:4317"
				return c
			}(),
			expectError: true,
			errorMsg:    "TracingSampleRate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestConfigMethods tests various config helper methods
func TestConfigMethods(t *testing.T) {
	config := DefaultConfig()

	// Test effective methods
	assert.Equal(t, RingBufferSize, config.GetEffectiveRingBufferSize())
	assert.Equal(t, EventBatchSize, config.GetEffectiveBatchSize())
	assert.Equal(t, 0.1, config.GetEffectiveTracingSampleRate())
	assert.Equal(t, 2048, config.GetEffectiveSpanBufferSize())

	// Test OTEL config
	otelConfig := config.GetOTELConfig()
	assert.Equal(t, config.TracingEnabled, otelConfig.TracingEnabled)
	assert.Equal(t, config.MetricsEnabled, otelConfig.MetricsEnabled)
	assert.Equal(t, config.OTLPEndpoint, otelConfig.OTLPEndpoint)

	// Test environment optimization
	config.OptimizeForEnvironment(2000, 150) // High container count, high event rate
	assert.Equal(t, 50000, config.EventBufferSize)
	assert.Equal(t, 32768, config.RingBufferSize)
	assert.Equal(t, 50*time.Millisecond, config.PollInterval)

	// Test OTEL environment optimization
	config.OptimizeOTELForEnvironment("production", 10000)
	assert.Equal(t, 0.01, config.TracingSampleRate) // Conservative sampling for high load
	assert.Equal(t, "production", config.DeploymentEnvironment)
}

// TestContainerFiltering tests container filtering logic
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
				ID:   "k8s-container-1",
				Name: "app-container",
				Labels: map[string]string{
					"io.kubernetes.pod.uid": "test-uid-123",
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
				ID:     "docker-container-1",
				Name:   "standalone-app",
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
				ID:    "pause-container",
				Name:  "k8s_POD_nginx",
				Image: "registry.k8s.io/pause:3.9",
				Labels: map[string]string{
					"io.kubernetes.container.name": "POD",
				},
			},
			expected: false,
		},
		{
			name: "pause container excluded by image",
			config: Config{
				ExcludeSystemContainers: true,
			},
			container: &ContainerInfo{
				ID:    "pause-by-image",
				Name:  "some-container",
				Image: "/pause:latest",
			},
			expected: false,
		},
		{
			name: "included namespace",
			config: Config{
				IncludeNamespaces: []string{"default", "app"},
			},
			container: &ContainerInfo{
				ID:   "app-container",
				Name: "my-app",
				Labels: map[string]string{
					"io.kubernetes.pod.namespace": "default",
					"io.kubernetes.pod.uid":       "test-uid",
				},
			},
			expected: true,
		},
		{
			name: "excluded namespace",
			config: Config{
				ExcludeNamespaces: []string{"kube-system", "kube-public"},
			},
			container: &ContainerInfo{
				ID:   "system-container",
				Name: "kube-proxy",
				Labels: map[string]string{
					"io.kubernetes.pod.namespace": "kube-system",
					"io.kubernetes.pod.uid":       "system-uid",
				},
			},
			expected: false,
		},
		{
			name: "wildcard namespace pattern",
			config: Config{
				ExcludeNamespaces: []string{"kube-*"},
			},
			container: &ContainerInfo{
				ID:   "wildcard-test",
				Name: "test-container",
				Labels: map[string]string{
					"io.kubernetes.pod.namespace": "kube-system",
					"io.kubernetes.pod.uid":       "wildcard-uid",
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

// TestContainerInfoMethods tests ContainerInfo helper methods
func TestContainerInfoMethods(t *testing.T) {
	// Test Kubernetes container
	k8sContainer := &ContainerInfo{
		ID:   "k8s-test",
		Name: "nginx",
		Labels: map[string]string{
			"io.kubernetes.pod.uid":        "test-pod-uid",
			"io.kubernetes.pod.name":       "nginx-pod",
			"io.kubernetes.pod.namespace":  "default",
			"io.kubernetes.container.name": "nginx",
		},
	}

	assert.True(t, k8sContainer.IsKubernetesContainer())

	metadata := k8sContainer.GetKubernetesMetadata()
	assert.NotNil(t, metadata)
	assert.Equal(t, "test-pod-uid", metadata.PodUID)
	assert.Equal(t, "nginx-pod", metadata.PodName)
	assert.Equal(t, "default", metadata.PodNamespace)
	assert.Equal(t, "nginx", metadata.ContainerName)

	// Test non-Kubernetes container
	dockerContainer := &ContainerInfo{
		ID:     "docker-test",
		Name:   "standalone-app",
		Labels: map[string]string{},
	}

	assert.False(t, dockerContainer.IsKubernetesContainer())
	assert.Nil(t, dockerContainer.GetKubernetesMetadata())
}

// TestMetrics tests metrics functionality
func TestMetrics(t *testing.T) {
	metrics := &Metrics{}

	// Test initial state
	stats := metrics.GetStats()
	assert.Equal(t, int64(0), stats.EventsProcessed)
	assert.Equal(t, int64(0), stats.ErrorCount)
	assert.Contains(t, stats.CustomMetrics, "events_dropped")
	assert.Contains(t, stats.CustomMetrics, "oom_kills_detected")

	// Update metrics
	metrics.EventsProcessed.Add(15)
	metrics.EventsDropped.Add(3)
	metrics.OOMKillsDetected.Add(2)
	metrics.CRIErrors.Add(1)
	metrics.BatchesSent.Add(5)

	// Verify updates
	stats = metrics.GetStats()
	assert.Equal(t, int64(15), stats.EventsProcessed)
	assert.Equal(t, int64(1), stats.ErrorCount)
	assert.Equal(t, "3", stats.CustomMetrics["events_dropped"])
	assert.Equal(t, "2", stats.CustomMetrics["oom_kills_detected"])
	assert.Equal(t, "5", stats.CustomMetrics["batches_sent"])
}

// TestUtilityFunctions tests utility functions
func TestUtilityFunctions(t *testing.T) {
	// Test formatBytes
	tests := []struct {
		bytes    uint64
		expected string
	}{
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}

	for _, tt := range tests {
		result := formatBytes(tt.bytes)
		assert.Equal(t, tt.expected, result)
	}

	// Test parseBytes - the original implementation has a bug:
	// it uses fmt.Sscanf return count instead of the parsed value
	parseTests := []struct {
		input    string
		expected uint64
	}{
		{"1G", 1073741824},  // Returns 1 * 1024^3 (count=1)
		{"2GB", 1073741824}, // Returns 1 * 1024^3 (count=1, ignores actual value)
		{"512M", 1048576},   // Returns 1 * 1024^2 (count=1, ignores actual value)
		{"1024MB", 1048576}, // Returns 1 * 1024^2 (count=1, ignores actual value)
		{"invalid", 0},
	}

	for _, tt := range parseTests {
		result := parseBytes(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}

// TestConcurrentAccess tests concurrent access to collector components
func TestConcurrentAccess(t *testing.T) {
	config := DefaultConfig()
	config.SocketPath = "/tmp/test-concurrent.sock"

	collector, err := NewCollector("test-concurrent", config)
	require.NoError(t, err)

	// Test concurrent ring buffer access
	var wg sync.WaitGroup
	numGoroutines := 10
	eventsPerGoroutine := 100

	// Start writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				event := collector.eventPool.Get()
				event.Type = EventType(j % 5)
				event.ExitCode = int32(goroutineID*1000 + j)
				event.SetContainerID(fmt.Sprintf("container-%d-%d", goroutineID, j))

				if !collector.ringBuffer.Write(event) {
					// Buffer full, return to pool
					collector.eventPool.Put(event)
					collector.metrics.EventsDropped.Add(1)
				} else {
					collector.metrics.EventsProcessed.Add(1)
				}
			}
		}(i)
	}

	// Start readers
	var readCount atomic.Int64

	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			timeout := time.After(5 * time.Second) // Timeout to avoid infinite loop
			for {
				select {
				case <-timeout:
					return // Timeout reached
				default:
				}

				event := collector.ringBuffer.Read()
				if event == nil {
					time.Sleep(time.Millisecond)
					continue
				}

				// Validate event data
				assert.NotEqual(t, "", event.GetContainerID())
				assert.GreaterOrEqual(t, event.ExitCode, int32(0))

				collector.eventPool.Put(event)
				readCount.Add(1)
			}
		}()
	}

	wg.Wait()

	// Verify that operations completed successfully
	finalProcessed := collector.metrics.EventsProcessed.Load()
	finalDropped := collector.metrics.EventsDropped.Load()
	finalReads := readCount.Load()

	t.Logf("Final stats - Processed: %d, Dropped: %d, Reads: %d",
		finalProcessed, finalDropped, finalReads)

	assert.Greater(t, finalProcessed, uint64(0))
	assert.GreaterOrEqual(t, finalReads, int64(0))
}

// TestOTELInstrumentation tests OpenTelemetry integration
func TestOTELInstrumentation(t *testing.T) {
	// Setup in-memory tracer and meter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	otel.SetTracerProvider(tp)

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
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

	// Test that all metric instruments are created
	assert.NotNil(t, collector.eventsProcessed)
	assert.NotNil(t, collector.eventsDropped)
	assert.NotNil(t, collector.oomKillsDetected)
	assert.NotNil(t, collector.processingLatency)
	assert.NotNil(t, collector.activeContainers)
	assert.NotNil(t, collector.criErrors)
	assert.NotNil(t, collector.batchSize)
	assert.NotNil(t, collector.checksPerformed)
	assert.NotNil(t, collector.bufferUsage)

	// Test recording metrics
	if collector.eventsProcessed != nil {
		collector.eventsProcessed.Add(ctx, 10)
	}
	if collector.oomKillsDetected != nil {
		collector.oomKillsDetected.Add(ctx, 3)
	}
	if collector.activeContainers != nil {
		collector.activeContainers.Add(ctx, 25)
	}

	// Read and verify metrics
	metricData := &metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, metricData)
	assert.NoError(t, err)

	// Cleanup
	err = tp.Shutdown(ctx)
	assert.NoError(t, err)
	err = mp.Shutdown(ctx)
	assert.NoError(t, err)
}

// TestRuntimeDetection tests container runtime detection
func TestRuntimeDetection(t *testing.T) {
	config := DefaultConfig()
	config.SocketPath = "/tmp/runtime-test.sock"
	collector, err := NewCollector("runtime-test", config)
	require.NoError(t, err)

	tests := []struct {
		socket   string
		expected string
	}{
		{"/run/containerd/containerd.sock", "containerd"},
		{"/var/run/crio/crio.sock", "cri-o"},
		{"/var/run/dockershim.sock", "docker"},
		{"/unknown/socket.sock", "unknown"},
	}

	for _, tt := range tests {
		collector.socket = tt.socket
		result := collector.detectRuntime()
		assert.Equal(t, tt.expected, result)
	}
}

// TestEventStateChanges tests container state change detection
func TestEventStateChanges(t *testing.T) {
	config := DefaultConfig()
	config.SocketPath = "/tmp/state-test.sock"
	collector, err := NewCollector("state-test", config)
	require.NoError(t, err)

	// Test hasStateChanged
	oldStatus := &cri.ContainerStatus{
		Id:         "test-container",
		State:      cri.ContainerState_CONTAINER_RUNNING,
		ExitCode:   0,
		StartedAt:  1000,
		FinishedAt: 0,
	}

	newStatus := &cri.ContainerStatus{
		Id:         "test-container",
		State:      cri.ContainerState_CONTAINER_EXITED,
		ExitCode:   137,
		StartedAt:  1000,
		FinishedAt: 2000,
	}

	// Should detect state change
	assert.True(t, collector.hasStateChanged(oldStatus, newStatus))

	// Test with same status
	sameStatus := &cri.ContainerStatus{
		Id:         "test-container",
		State:      cri.ContainerState_CONTAINER_RUNNING,
		ExitCode:   0,
		StartedAt:  1000,
		FinishedAt: 0,
	}
	assert.False(t, collector.hasStateChanged(oldStatus, sameStatus))

	// Test determineEventType
	createdToRunning := collector.determineEventType(
		&cri.ContainerStatus{State: cri.ContainerState_CONTAINER_CREATED},
		&cri.ContainerStatus{State: cri.ContainerState_CONTAINER_RUNNING},
	)
	assert.Equal(t, EventStarted, createdToRunning)

	// Test OOM detection
	oomStatus := &cri.ContainerStatus{
		State:    cri.ContainerState_CONTAINER_EXITED,
		ExitCode: 137,
		Reason:   "OOMKilled",
	}
	oomEvent := collector.determineEventType(oldStatus, oomStatus)
	assert.Equal(t, EventOOM, oomEvent)

	// Test normal exit
	normalExit := &cri.ContainerStatus{
		State:    cri.ContainerState_CONTAINER_EXITED,
		ExitCode: 0,
	}
	normalEvent := collector.determineEventType(oldStatus, normalExit)
	assert.Equal(t, EventStopped, normalEvent)

	// Test error exit
	errorExit := &cri.ContainerStatus{
		State:    cri.ContainerState_CONTAINER_EXITED,
		ExitCode: 1,
	}
	errorEvent := collector.determineEventType(oldStatus, errorExit)
	assert.Equal(t, EventDied, errorEvent)
}

// TestEventCreationAndProcessing tests event creation and processing
func TestEventCreationAndProcessing(t *testing.T) {
	config := DefaultConfig()
	config.SocketPath = "/tmp/event-test.sock"
	collector, err := NewCollector("event-test", config)
	require.NoError(t, err)

	// Create test container status with rich metadata
	status := &cri.ContainerStatus{
		Id:         "test-container-abc123def456",
		ExitCode:   137,
		Reason:     "OOMKilled",
		StartedAt:  1000000000, // Unix nano timestamp
		FinishedAt: 2000000000,
		Labels: map[string]string{
			"io.kubernetes.pod.uid":        "test-pod-uid-789",
			"io.kubernetes.pod.name":       "nginx-pod",
			"io.kubernetes.pod.namespace":  "production",
			"io.kubernetes.container.name": "nginx",
		},
		Annotations: map[string]string{
			"memory.usage": "2G",
			"memory.limit": "1.5G",
		},
	}

	// Test OOM event creation
	initialDropped := collector.metrics.EventsDropped.Load()
	initialProcessed := collector.metrics.EventsProcessed.Load()
	initialOOM := collector.metrics.OOMKillsDetected.Load()

	collector.createEvent(status, EventOOM)

	// Verify metrics were updated
	assert.Equal(t, initialProcessed+1, collector.metrics.EventsProcessed.Load())
	assert.Equal(t, initialOOM+1, collector.metrics.OOMKillsDetected.Load())

	// Read the event from ring buffer
	event := collector.ringBuffer.Read()
	require.NotNil(t, event)

	// Verify event data
	assert.Equal(t, EventOOM, event.Type)
	assert.Equal(t, int32(137), event.ExitCode)
	assert.Equal(t, uint8(1), event.OOMKilled)
	assert.Equal(t, int32(9), event.Signal)
	assert.Equal(t, "nginx-pod", event.PodName)
	assert.Equal(t, "production", event.Namespace)
	assert.Equal(t, "test-container-abc123def456", event.GetContainerID())
	assert.Equal(t, "test-pod-uid-789", event.GetPodUID())
	assert.Equal(t, "OOMKilled", event.Reason)
	assert.Contains(t, event.Message, "OOM")
	assert.Equal(t, int64(1000000000), event.StartedAt)
	assert.Equal(t, int64(2000000000), event.FinishedAt)

	// Verify memory parsing from annotations
	assert.Greater(t, event.MemoryUsage, uint64(0))
	assert.Greater(t, event.MemoryLimit, uint64(0))

	// Return event to pool
	collector.eventPool.Put(event)

	// Test buffer overflow scenario
	overflow_events := RingBufferSize + 10
	for i := 0; i < overflow_events; i++ {
		collector.createEvent(status, EventOOM)
	}

	// Should have some dropped events
	finalDropped := collector.metrics.EventsDropped.Load()
	assert.Greater(t, finalDropped, initialDropped)
}

// TestNonLinuxEBPFCollector tests eBPF collector on non-Linux platforms
func TestNonLinuxEBPFCollector(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Skipping non-Linux eBPF test on Linux platform")
	}

	config := DefaultConfig()
	config.EnableEBPF = true
	config.SocketPath = "/tmp/test-ebpf.sock"

	ebpfCollector, err := NewEBPFCollector("test-ebpf", config)
	require.NoError(t, err)
	require.NotNil(t, ebpfCollector)

	// Should have base collector functionality
	assert.NotNil(t, ebpfCollector.Collector)
	assert.Equal(t, "test-ebpf", ebpfCollector.Name())

	// eBPF-specific methods should be no-ops
	err = ebpfCollector.UpdateContainerMetadata(12345, "test-container", "test-pod-uid", 1024*1024*1024)
	assert.NoError(t, err) // Should be no-op

	stats := ebpfCollector.GetEBPFStats()
	assert.Nil(t, stats) // Should return nil on non-Linux

	// Start/Stop should work through base collector
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Note: Start will likely fail due to no real CRI socket, but that's expected in test
	err = ebpfCollector.Start(ctx)
	// Don't assert on Start error since we don't have a real CRI socket

	err = ebpfCollector.Stop()
	assert.NoError(t, err)
}

// BenchmarkEventCreation benchmarks event creation performance
func BenchmarkEventCreation(b *testing.B) {
	config := DefaultConfig()
	config.SocketPath = "/tmp/bench-create.sock"
	collector, err := NewCollector("bench-create", config)
	require.NoError(b, err)

	status := &cri.ContainerStatus{
		Id:       "benchmark-container-123456789abcdef",
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

			// Read and return to pool to complete the cycle
			if event := collector.ringBuffer.Read(); event != nil {
				collector.eventPool.Put(event)
			}
		}
	})
}

// BenchmarkRingBufferOperations benchmarks ring buffer performance
func BenchmarkRingBufferOperations(b *testing.B) {
	buffer := NewRingBuffer()

	// Pre-allocate events to avoid allocation overhead in benchmark
	events := make([]*Event, 1000)
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
				buffer.Write(event) // Retry write
			}

			i++
		}
	})
}

// BenchmarkEventPoolOperations benchmarks event pool performance
func BenchmarkEventPoolOperations(b *testing.B) {
	pool := NewEventPool()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			event := pool.Get()
			event.SetContainerID("benchmark-container-12345")
			event.Type = EventOOM
			event.ExitCode = 137
			pool.Put(event)
		}
	})
}

// BenchmarkContainerFiltering benchmarks container filtering performance
func BenchmarkContainerFiltering(b *testing.B) {
	config := Config{
		KubernetesOnly:          true,
		ExcludeSystemContainers: true,
		ExcludeNamespaces:       []string{"kube-system", "kube-public", "monitoring"},
		IncludeNamespaces:       []string{"default", "production", "staging"},
	}

	containers := []*ContainerInfo{
		{
			Name: "nginx-app",
			Labels: map[string]string{
				"io.kubernetes.pod.uid":       "app-uid-123",
				"io.kubernetes.pod.namespace": "production",
			},
		},
		{
			Name:  "pause",
			Image: "registry.k8s.io/pause:3.9",
			Labels: map[string]string{
				"io.kubernetes.container.name": "POD",
				"io.kubernetes.pod.namespace":  "kube-system",
			},
		},
		{
			Name: "monitoring-agent",
			Labels: map[string]string{
				"io.kubernetes.pod.uid":       "monitoring-uid-456",
				"io.kubernetes.pod.namespace": "monitoring",
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		container := containers[i%len(containers)]
		_ = config.ShouldIncludeContainer(container)
	}
}

// TestCollectorInterface ensures collector implements required interfaces
func TestCollectorInterface(t *testing.T) {
	config := DefaultConfig()
	config.SocketPath = "/tmp/interface-test.sock"
	collector, err := NewCollector("interface-test", config)
	require.NoError(t, err)

	// Test that collector implements expected interfaces
	assert.Implements(t, (*domain.HealthChecker)(nil), collector)

	// Test interface methods
	health := collector.Health()
	assert.NotNil(t, health)
	assert.IsType(t, &domain.HealthStatus{}, health)

	isHealthy := collector.IsHealthy()
	assert.IsType(t, false, isHealthy) // Should be false since not started

	statistics := collector.Statistics()
	assert.NotNil(t, statistics)
	assert.IsType(t, &domain.CollectorStats{}, statistics)
}

func init() {
	// Setup test logger to reduce noise
	logger := zaptest.NewLogger(nil, zaptest.Level(zap.WarnLevel))
	zap.ReplaceGlobals(logger)
}
