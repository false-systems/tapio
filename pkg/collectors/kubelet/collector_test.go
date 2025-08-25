package kubelet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	statsv1alpha1 "k8s.io/kubelet/pkg/apis/stats/v1alpha1"
)

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name:        "default config",
			config:      nil,
			expectError: false,
		},
		{
			name:        "valid config",
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name: "invalid config with invalid cert paths",
			config: &Config{
				Address:    "localhost:10250",
				ClientCert: "/non/existent/cert.pem",
				ClientKey:  "/non/existent/key.pem",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector("test-kubelet", tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
				assert.Equal(t, "test-kubelet", collector.Name())
				assert.True(t, collector.IsHealthy())

				// Test OTEL instrumentation is initialized
				assert.NotNil(t, collector.tracer)
			}
		})
	}
}

func TestCollectorLifecycle(t *testing.T) {
	// Create mock kubelet server
	mockKubelet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		case "/stats/summary":
			// Mock stats response
			statsResponse := `{
				"node": {
					"nodeName": "test-node",
					"cpu": {
						"time": "2024-01-01T00:00:00Z",
						"usageNanoCores": 1000000000,
						"usageCoreNanoSeconds": 5000000000
					},
					"memory": {
						"time": "2024-01-01T00:00:00Z",
						"availableBytes": 4000000000,
						"usageBytes": 2000000000,
						"workingSetBytes": 1500000000
					}
				},
				"pods": []
			}`
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(statsResponse))
		case "/pods":
			// Mock pods response
			podsResponse := `{
				"kind": "PodList",
				"apiVersion": "v1",
				"items": []
			}`
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(podsResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockKubelet.Close()

	// Create config pointing to mock server
	config := &Config{
		Address:         mockKubelet.URL[7:], // Remove http:// prefix
		Insecure:        true,                // Use HTTP for testing
		StatsInterval:   100 * time.Millisecond,
		MetricsInterval: 100 * time.Millisecond,
		Logger:          zap.NewNop(),
	}

	collector, err := NewCollector("test-kubelet", config)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Test Start
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Test events channel
	eventsChan := collector.Events()
	assert.NotNil(t, eventsChan)

	// Wait for some events to be collected
	eventCount := 0
	timeout := time.After(500 * time.Millisecond)
	for eventCount < 2 {
		select {
		case event := <-eventsChan:
			assert.NotNil(t, event)
			assert.NotEmpty(t, event.EventID)
			assert.Equal(t, "test-kubelet", event.Source)
			assert.False(t, event.Timestamp.IsZero())
			eventCount++

			// Check event data is properly typed
			if kubeletData, ok := event.GetKubeletData(); ok {
				assert.NotNil(t, kubeletData)
				assert.NotEmpty(t, kubeletData.EventType)
			}
		case <-timeout:
			t.Log("Timeout waiting for events")
			break
		}
	}

	// Verify we got some events
	assert.Greater(t, eventCount, 0, "Should have received at least one event")

	// Test Stop
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

func TestCollectorConnectivityCheck(t *testing.T) {
	tests := []struct {
		name        string
		serverSetup func() *httptest.Server
		expectError bool
	}{
		{
			name: "healthy kubelet",
			serverSetup: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/healthz" {
						w.WriteHeader(http.StatusOK)
						w.Write([]byte("ok"))
					}
				}))
			},
			expectError: false,
		},
		{
			name: "unhealthy kubelet",
			serverSetup: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/healthz" {
						w.WriteHeader(http.StatusServiceUnavailable)
						w.Write([]byte("unhealthy"))
					}
				}))
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockServer := tt.serverSetup()
			defer mockServer.Close()

			config := &Config{
				Address:         mockServer.URL[7:], // Remove http:// prefix
				Insecure:        true,
				Logger:          zap.NewNop(),
				MetricsInterval: 30 * time.Second,
				StatsInterval:   10 * time.Second,
				RequestTimeout:  10 * time.Second,
				MaxRetries:      3,
			}

			collector, err := NewCollector("test-kubelet", config)
			require.NoError(t, err)

			ctx := context.Background()
			err = collector.Start(ctx)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				collector.Stop()
			}
		})
	}
}

func TestEventGeneration(t *testing.T) {
	// Test node CPU event generation
	config := DefaultConfig()
	config.Insecure = true // For testing
	collector, err := NewCollector("test", config)
	require.NoError(t, err)

	// Create mock stats response
	mockSummary := createMockStatsSummary()

	ctx := context.Background()

	// Try to start collector, skip test if kubelet is not available
	err = collector.Start(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "connectivity check failed") {
			t.Skip("Kubelet not available for testing - skipping test")
		}
		require.NoError(t, err) // Fail for other errors
	}
	defer collector.Stop()

	// Capture events
	eventsChan := collector.Events()

	// Send node CPU event
	go collector.sendNodeCPUEvent(ctx, mockSummary)

	// Wait for event
	select {
	case event := <-eventsChan:
		assert.NotNil(t, event)
		assert.Equal(t, domain.EventTypeKubeletNodeCPU, event.Type)
		assert.Equal(t, "test", event.Source)

		// Check kubelet data
		kubeletData, ok := event.GetKubeletData()
		require.True(t, ok)
		assert.Equal(t, "node_cpu", kubeletData.EventType)
		assert.NotNil(t, kubeletData.NodeMetrics)
		assert.Equal(t, "test-node", kubeletData.NodeMetrics.NodeName)
		assert.Equal(t, uint64(1000000000), kubeletData.NodeMetrics.CPUUsageNano)

	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for CPU event")
	}
}

func TestEventDataTypes(t *testing.T) {
	tests := []struct {
		name          string
		eventType     domain.CollectorEventType
		expectedField string
	}{
		{"node_cpu", domain.EventTypeKubeletNodeCPU, "NodeMetrics"},
		{"node_memory", domain.EventTypeKubeletNodeMemory, "NodeMetrics"},
		{"cpu_throttling", domain.EventTypeKubeletCPUThrottling, "ContainerMetrics"},
		{"memory_pressure", domain.EventTypeKubeletMemoryPressure, "ContainerMetrics"},
		{"ephemeral_storage", domain.EventTypeKubeletEphemeralStorage, "StorageEvent"},
		{"container_waiting", domain.EventTypeKubeletContainerWaiting, "PodLifecycle"},
		{"container_terminated", domain.EventTypeKubeletContainerTerminated, "PodLifecycle"},
		{"crash_loop", domain.EventTypeKubeletCrashLoop, "PodLifecycle"},
		{"pod_not_ready", domain.EventTypeKubeletPodNotReady, "PodLifecycle"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &domain.CollectorEvent{
				Type: tt.eventType,
				EventData: domain.EventDataContainer{
					Kubelet: &domain.KubeletData{
						EventType: tt.name,
					},
				},
			}

			kubeletData, ok := event.GetKubeletData()
			require.True(t, ok)
			assert.Equal(t, tt.name, kubeletData.EventType)
		})
	}
}

func TestHealthAndStatistics(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	// Test Health
	healthy, healthStatus := collector.Health()
	assert.True(t, healthy)
	assert.NotNil(t, healthStatus)
	assert.True(t, healthStatus.Healthy)
	assert.Equal(t, "localhost:10250", healthStatus.KubeletAddress)

	// Test Statistics
	stats := collector.Statistics()
	assert.NotNil(t, stats)
	assert.Equal(t, int64(0), stats.EventsCollected)
	assert.Equal(t, int64(0), stats.ErrorsCount)
}

func TestPodTraceManager(t *testing.T) {
	ptm := NewPodTraceManager()
	defer ptm.Stop()

	// Test GetOrGenerate
	podUID := types.UID("test-pod-123")
	traceID1 := ptm.GetOrGenerate(podUID)
	assert.NotEmpty(t, traceID1)

	// Should get same trace ID for same pod
	traceID2 := ptm.GetOrGenerate(podUID)
	assert.Equal(t, traceID1, traceID2)

	// Different pod should get different trace ID
	traceID3 := ptm.GetOrGenerate(types.UID("different-pod-456"))
	assert.NotEqual(t, traceID1, traceID3)

	// Test count
	assert.Equal(t, 2, ptm.Count())
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name:        "valid config",
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name: "invalid - empty address",
			config: &Config{
				Address: "",
			},
			expectError: true,
		},
		{
			name: "invalid - too short metrics interval",
			config: &Config{
				Address:         "localhost:10250",
				MetricsInterval: 1 * time.Second,
			},
			expectError: true,
		},
		{
			name: "invalid - negative max retries",
			config: &Config{
				Address:    "localhost:10250",
				MaxRetries: -1,
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

// Helper functions for tests

func createMockStatsSummary() *statsv1alpha1.Summary {
	cpuUsage := uint64(1000000000)      // 1 CPU core
	memUsage := uint64(2000000000)      // 2GB
	memAvailable := uint64(4000000000)  // 4GB
	memWorkingSet := uint64(1500000000) // 1.5GB

	return &statsv1alpha1.Summary{
		Node: statsv1alpha1.NodeStats{
			NodeName: "test-node",
			CPU: &statsv1alpha1.CPUStats{
				Time:                 metav1.NewTime(time.Now()),
				UsageNanoCores:       &cpuUsage,
				UsageCoreNanoSeconds: &cpuUsage,
			},
			Memory: &statsv1alpha1.MemoryStats{
				Time:            metav1.NewTime(time.Now()),
				AvailableBytes:  &memAvailable,
				UsageBytes:      &memUsage,
				WorkingSetBytes: &memWorkingSet,
			},
		},
		Pods: []statsv1alpha1.PodStats{},
	}
}
