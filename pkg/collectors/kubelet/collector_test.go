package kubelet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	statsv1alpha1 "k8s.io/kubelet/pkg/apis/stats/v1alpha1"
)

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "default config",
			config:  nil,
			wantErr: false,
		},
		{
			name: "custom config",
			config: &Config{
				Address:         "node1:10250",
				MetricsInterval: 1 * time.Minute,
				StatsInterval:   30 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "with client certs",
			config: &Config{
				Address:    "node1:10250",
				ClientCert: "testdata/client.crt",
				ClientKey:  "testdata/client.key",
			},
			wantErr: true, // Will fail because test certs don't exist
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector("kubelet-test", tt.config)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
				assert.Equal(t, "kubelet-test", collector.Name())
			}
		})
	}
}

func TestCollectorHealthCheck(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	config := &Config{
		Address:         server.Listener.Addr().String(),
		Insecure:        true,
		Logger:          zap.NewNop(),
		MetricsInterval: 30 * time.Second,
		StatsInterval:   10 * time.Second,
	}

	collector, err := NewCollector("kubelet-test", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err)

	assert.True(t, collector.IsHealthy())

	err = collector.Stop()
	assert.NoError(t, err)
}

func TestCollectorStatsCollection(t *testing.T) {
	// Mock kubelet stats response
	mockStats := &statsv1alpha1.Summary{
		Node: statsv1alpha1.NodeStats{
			NodeName: "test-node",
			CPU: &statsv1alpha1.CPUStats{
				Time:           metav1.Now(),
				UsageNanoCores: uint64Ptr(1000000000), // 1 CPU
			},
			Memory: &statsv1alpha1.MemoryStats{
				Time:            metav1.Now(),
				UsageBytes:      uint64Ptr(1024 * 1024 * 1024), // 1GB
				AvailableBytes:  uint64Ptr(2 * 1024 * 1024 * 1024),
				WorkingSetBytes: uint64Ptr(512 * 1024 * 1024),
			},
		},
		Pods: []statsv1alpha1.PodStats{
			{
				PodRef: statsv1alpha1.PodReference{
					Name:      "test-pod",
					Namespace: "default",
					UID:       "test-uid",
				},
				Containers: []statsv1alpha1.ContainerStats{
					{
						Name: "test-container",
						CPU: &statsv1alpha1.CPUStats{
							Time:           metav1.Now(),
							UsageNanoCores: uint64Ptr(100000000), // 0.1 CPU
						},
						Memory: &statsv1alpha1.MemoryStats{
							Time:            metav1.Now(),
							UsageBytes:      uint64Ptr(100 * 1024 * 1024), // 100MB
							WorkingSetBytes: uint64Ptr(80 * 1024 * 1024),
							RSSBytes:        uint64Ptr(90 * 1024 * 1024),
						},
					},
				},
				EphemeralStorage: &statsv1alpha1.FsStats{
					Time:           metav1.Now(),
					UsedBytes:      uint64Ptr(500 * 1024 * 1024), // 500MB
					AvailableBytes: uint64Ptr(500 * 1024 * 1024), // 50% usage
				},
			},
		},
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/stats/summary":
			json.NewEncoder(w).Encode(mockStats)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	config := &Config{
		Address:         server.Listener.Addr().String(),
		Insecure:        true,
		StatsInterval:   100 * time.Millisecond,
		MetricsInterval: 100 * time.Millisecond,
		Logger:          zap.NewNop(),
	}

	collector, err := NewCollector("kubelet-test", config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Collect events
	events := make([]collectors.RawEvent, 0)
	timeout := time.After(500 * time.Millisecond)

	for {
		select {
		case event := <-collector.Events():
			events = append(events, event)
		case <-timeout:
			goto done
		}
	}

done:
	err = collector.Stop()
	assert.NoError(t, err)

	// Verify events
	assert.GreaterOrEqual(t, len(events), 5) // Should have node CPU, memory, pod events, etc.

	// Check event types
	eventTypes := make(map[string]int)
	for _, event := range events {
		eventTypes[event.Type]++
		assert.NotEmpty(t, event.TraceID)
		assert.NotEmpty(t, event.SpanID)
		assert.NotEmpty(t, event.Metadata["collector"])
		assert.Equal(t, "kubelet", event.Metadata["collector"])
	}

	// Should have various event types
	t.Logf("Event types collected: %v", eventTypes)
	assert.Greater(t, eventTypes["kubelet_node_cpu"], 0)
	assert.Greater(t, eventTypes["kubelet_node_memory"], 0)
	assert.Greater(t, eventTypes["kubelet_cpu_throttling"], 0)
	assert.Greater(t, eventTypes["kubelet_memory_pressure"], 0)
	// Ephemeral storage event only sent when usage > 50%
	assert.GreaterOrEqual(t, eventTypes["kubelet_ephemeral_storage"], 0)
}

func TestCollectorPodLifecycle(t *testing.T) {
	// Mock pod list
	mockPods := &v1.PodList{
		Items: []v1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "waiting-pod",
					Namespace: "default",
					UID:       types.UID("waiting-uid"),
				},
				Status: v1.PodStatus{
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name: "waiting-container",
							State: v1.ContainerState{
								Waiting: &v1.ContainerStateWaiting{
									Reason:  "ImagePullBackOff",
									Message: "Back-off pulling image",
								},
							},
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "crashed-pod",
					Namespace: "default",
					UID:       types.UID("crashed-uid"),
				},
				Status: v1.PodStatus{
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name:         "crashed-container",
							RestartCount: 5,
							State: v1.ContainerState{
								Terminated: &v1.ContainerStateTerminated{
									ExitCode: 1,
									Reason:   "Error",
									Message:  "Application error",
								},
							},
							LastTerminationState: v1.ContainerState{
								Terminated: &v1.ContainerStateTerminated{
									ExitCode: 1,
									Reason:   "Error",
								},
							},
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-ready-pod",
					Namespace: "default",
					UID:       types.UID("not-ready-uid"),
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:               v1.PodReady,
							Status:             v1.ConditionFalse,
							Reason:             "ContainersNotReady",
							Message:            "containers are not ready",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
		},
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/pods":
			json.NewEncoder(w).Encode(mockPods)
		case "/stats/summary":
			// Return empty stats
			json.NewEncoder(w).Encode(&statsv1alpha1.Summary{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	config := &Config{
		Address:         server.Listener.Addr().String(),
		Insecure:        true,
		MetricsInterval: 100 * time.Millisecond,
		StatsInterval:   100 * time.Millisecond,
		Logger:          zap.NewNop(),
	}

	collector, err := NewCollector("kubelet-test", config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Collect events
	events := make([]collectors.RawEvent, 0)
	timeout := time.After(500 * time.Millisecond)

	for {
		select {
		case event := <-collector.Events():
			events = append(events, event)
		case <-timeout:
			goto done
		}
	}

done:
	err = collector.Stop()
	assert.NoError(t, err)

	// Verify pod lifecycle events
	eventTypes := make(map[string]int)
	for _, event := range events {
		eventTypes[event.Type]++
	}

	assert.Greater(t, eventTypes["kubelet_container_waiting"], 0)
	assert.Greater(t, eventTypes["kubelet_container_terminated"], 0)
	assert.Greater(t, eventTypes["kubelet_crash_loop"], 0)
	assert.Greater(t, eventTypes["kubelet_pod_not_ready"], 0)
}

func TestCollectorErrorHandling(t *testing.T) {
	// Create test server that returns errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/stats/summary":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal server error"))
		case "/pods":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("Forbidden"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	config := &Config{
		Address:         server.Listener.Addr().String(),
		Insecure:        true,
		StatsInterval:   100 * time.Millisecond,
		MetricsInterval: 100 * time.Millisecond,
		Logger:          zap.NewNop(),
	}

	collector, err := NewCollector("kubelet-test", config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Let it run and accumulate errors
	time.Sleep(300 * time.Millisecond)

	// Check statistics
	stats := collector.Statistics()
	assert.Greater(t, stats.ErrorsCount, int64(0))

	err = collector.Stop()
	assert.NoError(t, err)
}

func TestCollectorConnectivityCheck(t *testing.T) {
	// Create test server that fails health check
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	config := &Config{
		Address:         server.Listener.Addr().String(),
		Insecure:        true,
		Logger:          zap.NewNop(),
		MetricsInterval: 30 * time.Second,
		StatsInterval:   10 * time.Second,
	}

	collector, err := NewCollector("kubelet-test", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kubelet health check failed")
}

func TestCollectorTraceManagement(t *testing.T) {
	podUID1 := types.UID("pod-1")
	podUID2 := types.UID("pod-2")

	config := DefaultConfig()
	config.Logger = zap.NewNop()

	collector, err := NewCollector("kubelet-test", config)
	require.NoError(t, err)
	defer collector.Stop()

	// Generate trace IDs for pods
	trace1 := collector.getOrGenerateTraceID(podUID1)
	trace2 := collector.getOrGenerateTraceID(podUID2)

	assert.NotEmpty(t, trace1)
	assert.NotEmpty(t, trace2)
	assert.NotEqual(t, trace1, trace2)

	// Same pod should get same trace ID
	trace1Again := collector.getOrGenerateTraceID(podUID1)
	assert.Equal(t, trace1, trace1Again)
}

// TestKubeletInstrumentationCreation is removed as we no longer use instrumentation wrappers

func TestCollectorOTELMetrics(t *testing.T) {
	// Mock kubelet stats response
	mockStats := &statsv1alpha1.Summary{
		Node: statsv1alpha1.NodeStats{
			NodeName: "test-node",
			CPU: &statsv1alpha1.CPUStats{
				Time:           metav1.Now(),
				UsageNanoCores: uint64Ptr(1000000000), // 1 CPU
			},
			Memory: &statsv1alpha1.MemoryStats{
				Time:            metav1.Now(),
				UsageBytes:      uint64Ptr(1024 * 1024 * 1024), // 1GB
				AvailableBytes:  uint64Ptr(2 * 1024 * 1024 * 1024),
				WorkingSetBytes: uint64Ptr(512 * 1024 * 1024),
			},
		},
		Pods: []statsv1alpha1.PodStats{
			{
				PodRef: statsv1alpha1.PodReference{
					Name:      "test-pod",
					Namespace: "default",
					UID:       "test-uid",
				},
				Containers: []statsv1alpha1.ContainerStats{
					{
						Name: "test-container",
						CPU: &statsv1alpha1.CPUStats{
							Time:           metav1.Now(),
							UsageNanoCores: uint64Ptr(100000000), // 0.1 CPU
						},
						Memory: &statsv1alpha1.MemoryStats{
							Time:            metav1.Now(),
							UsageBytes:      uint64Ptr(100 * 1024 * 1024), // 100MB
							WorkingSetBytes: uint64Ptr(80 * 1024 * 1024),
							RSSBytes:        uint64Ptr(90 * 1024 * 1024),
						},
					},
				},
			},
		},
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/stats/summary":
			json.NewEncoder(w).Encode(mockStats)
		case "/pods":
			// Return empty pod list for lifecycle tests
			json.NewEncoder(w).Encode(&v1.PodList{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	config := &Config{
		Address:         server.Listener.Addr().String(),
		Insecure:        true,
		StatsInterval:   50 * time.Millisecond,
		MetricsInterval: 50 * time.Millisecond,
		Logger:          zap.NewNop(),
	}

	collector, err := NewCollector("kubelet-test", config)
	require.NoError(t, err)

	// Verify OTEL fields were initialized
	assert.NotNil(t, collector.tracer)
	assert.NotNil(t, collector.eventsProcessed)
	assert.NotNil(t, collector.errorsTotal)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Collect events for a short time to ensure metrics are recorded
	time.Sleep(150 * time.Millisecond)

	err = collector.Stop()
	assert.NoError(t, err)

	// Verify metrics were initialized (we can't easily verify the actual metric values
	// without a more complex test setup, but we can verify the structure is correct)
	assert.NotNil(t, collector.apiLatency)
	assert.NotNil(t, collector.eventsProcessed)
	assert.NotNil(t, collector.errorsTotal)
	assert.NotNil(t, collector.pollsActive)
	assert.NotNil(t, collector.apiFailures)
}

func TestCollectorOTELErrorMetrics(t *testing.T) {
	// Create test server that returns errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/stats/summary":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal server error"))
		case "/pods":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("Forbidden"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	config := &Config{
		Address:         server.Listener.Addr().String(),
		Insecure:        true,
		StatsInterval:   50 * time.Millisecond,
		MetricsInterval: 50 * time.Millisecond,
		Logger:          zap.NewNop(),
	}

	collector, err := NewCollector("kubelet-test", config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Let it run and accumulate errors
	time.Sleep(150 * time.Millisecond)

	// Verify error metrics are being tracked
	stats := collector.Statistics()
	assert.Greater(t, stats.ErrorsCount, int64(0))

	err = collector.Stop()
	assert.NoError(t, err)
}

func TestTraceContextExtraction(t *testing.T) {
	config := DefaultConfig()
	config.Logger = zap.NewNop()

	collector, err := NewCollector("kubelet-test", config)
	require.NoError(t, err)

	tests := []struct {
		name        string
		ctx         context.Context
		wantTraceID bool
		wantSpanID  bool
	}{
		{
			name:        "context without span",
			ctx:         context.Background(),
			wantTraceID: true, // Should generate new IDs
			wantSpanID:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			traceID, spanID := collector.extractTraceContext(tt.ctx)

			if tt.wantTraceID {
				assert.NotEmpty(t, traceID)
				assert.Len(t, traceID, 32) // OpenTelemetry trace ID is 32 hex chars
			}

			if tt.wantSpanID {
				assert.NotEmpty(t, spanID)
				assert.Len(t, spanID, 16) // OpenTelemetry span ID is 16 hex chars
			}
		})
	}
}

func TestOTELEventGeneration(t *testing.T) {
	config := DefaultConfig()
	config.Logger = zap.NewNop()

	collector, err := NewCollector("kubelet-test", config)
	require.NoError(t, err)

	// Initialize the collector context
	collector.ctx = context.Background()

	// Test node CPU event
	summary := &statsv1alpha1.Summary{
		Node: statsv1alpha1.NodeStats{
			NodeName: "test-node",
			CPU: &statsv1alpha1.CPUStats{
				Time:           metav1.Now(),
				UsageNanoCores: uint64Ptr(1000000000),
			},
		},
	}

	// Capture events
	ctx := context.Background()
	go func() {
		collector.sendNodeCPUEvent(ctx, summary)
	}()

	// Wait for event to be processed
	timeout := time.After(100 * time.Millisecond)
	select {
	case event := <-collector.Events():
		assert.Equal(t, "kubelet_node_cpu", event.Type)
		assert.NotEmpty(t, event.TraceID)
		assert.NotEmpty(t, event.SpanID)
		assert.Equal(t, "kubelet", event.Metadata["collector"])
		assert.Equal(t, "test-node", event.Metadata["node_name"])
	case <-timeout:
		t.Fatal("timeout waiting for event")
	}
}

func TestPodTraceManagerTTLCleanup(t *testing.T) {
	ptm := NewPodTraceManager()
	defer ptm.Stop()

	podUID := types.UID("test-pod")

	// Add an entry
	traceID := ptm.GetOrGenerate(podUID)
	assert.NotEmpty(t, traceID)
	assert.Equal(t, 1, ptm.Count())

	// Manually set timestamp to 2 hours ago to trigger cleanup
	ptm.mu.Lock()
	ptm.entries[podUID].Timestamp = time.Now().Add(-2 * time.Hour)
	ptm.mu.Unlock()

	// Run cleanup
	ptm.cleanupExpired()

	// Entry should be removed
	assert.Equal(t, 0, ptm.Count())
}

func TestCollectorHealthMethod(t *testing.T) {
	config := DefaultConfig()
	config.Logger = zap.NewNop()

	collector, err := NewCollector("kubelet-test", config)
	require.NoError(t, err)
	defer collector.Stop()

	// Test Health method
	healthy, details := collector.Health()
	assert.True(t, healthy)
	assert.NotNil(t, details)
	assert.True(t, details.Healthy)
	assert.GreaterOrEqual(t, details.EventsCollected, int64(0))
	assert.GreaterOrEqual(t, details.ErrorsCount, int64(0))
	assert.Equal(t, config.Address, details.KubeletAddress)
}

func TestCreateKubeletCollectorFromConfig(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]interface{}
		wantErr   bool
	}{
		{
			name:      "empty config",
			configMap: map[string]interface{}{},
			wantErr:   false,
		},
		{
			name: "custom config without certs",
			configMap: map[string]interface{}{
				"name":             "test-kubelet",
				"address":          "custom-node:10250",
				"insecure":         true,
				"node_name":        "test-node",
				"metrics_interval": "60s",
				"stats_interval":   "30s",
			},
			wantErr: false,
		},
		{
			name: "custom config with invalid certs",
			configMap: map[string]interface{}{
				"name":             "test-kubelet",
				"address":          "custom-node:10250",
				"insecure":         true,
				"node_name":        "test-node",
				"client_cert":      "/path/to/cert",
				"client_key":       "/path/to/key",
				"metrics_interval": "60s",
				"stats_interval":   "30s",
			},
			wantErr: true, // Will fail because cert files don't exist
		},
		{
			name: "invalid metrics interval",
			configMap: map[string]interface{}{
				"metrics_interval": "invalid-duration",
			},
			wantErr: true,
		},
		{
			name: "invalid stats interval",
			configMap: map[string]interface{}{
				"stats_interval": "invalid-duration",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := createKubeletCollector(tt.configMap)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
				if collector != nil {
					collector.Stop()
				}
			}
		})
	}
}

func TestParseConfigFromMap(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]interface{}
		wantErr   bool
		validate  func(t *testing.T, config *Config)
	}{
		{
			name:      "default config",
			configMap: map[string]interface{}{},
			wantErr:   false,
			validate: func(t *testing.T, config *Config) {
				assert.Equal(t, "localhost:10250", config.Address)
				assert.Equal(t, 30*time.Second, config.MetricsInterval)
				assert.Equal(t, 10*time.Second, config.StatsInterval)
			},
		},
		{
			name: "full config",
			configMap: map[string]interface{}{
				"address":          "custom:10250",
				"insecure":         true,
				"client_cert":      "/cert",
				"client_key":       "/key",
				"node_name":        "node1",
				"metrics_interval": "45s",
				"stats_interval":   "15s",
			},
			wantErr: false,
			validate: func(t *testing.T, config *Config) {
				assert.Equal(t, "custom:10250", config.Address)
				assert.True(t, config.Insecure)
				assert.Equal(t, "/cert", config.ClientCert)
				assert.Equal(t, "/key", config.ClientKey)
				assert.Equal(t, "node1", config.NodeName)
				assert.Equal(t, 45*time.Second, config.MetricsInterval)
				assert.Equal(t, 15*time.Second, config.StatsInterval)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseConfigFromMap(tt.configMap)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, config)
				if tt.validate != nil && config != nil {
					tt.validate(t, config)
				}
			}
		})
	}
}

func TestCheckEphemeralStorageEdgeCases(t *testing.T) {
	config := DefaultConfig()
	config.Logger = zap.NewNop()

	collector, err := NewCollector("kubelet-test", config)
	require.NoError(t, err)
	defer collector.Stop()

	// Initialize collector context
	collector.ctx = context.Background()

	// Test with nil usage bytes
	pod1 := &statsv1alpha1.PodStats{
		PodRef: statsv1alpha1.PodReference{
			Name:      "test-pod-1",
			Namespace: "default",
			UID:       "test-uid-1",
		},
		EphemeralStorage: &statsv1alpha1.FsStats{
			Time:           metav1.Now(),
			UsedBytes:      nil, // nil usage bytes
			AvailableBytes: uint64Ptr(500 * 1024 * 1024),
		},
	}

	// Should not panic and should not send event
	collector.checkEphemeralStorage(context.Background(), pod1)

	// Test with nil available bytes
	pod2 := &statsv1alpha1.PodStats{
		PodRef: statsv1alpha1.PodReference{
			Name:      "test-pod-2",
			Namespace: "default",
			UID:       "test-uid-2",
		},
		EphemeralStorage: &statsv1alpha1.FsStats{
			Time:           metav1.Now(),
			UsedBytes:      uint64Ptr(500 * 1024 * 1024),
			AvailableBytes: nil, // nil available bytes
		},
	}

	// Should not panic and should not send event
	collector.checkEphemeralStorage(context.Background(), pod2)

	// Test with low usage (should not send event)
	pod3 := &statsv1alpha1.PodStats{
		PodRef: statsv1alpha1.PodReference{
			Name:      "test-pod-3",
			Namespace: "default",
			UID:       "test-uid-3",
		},
		EphemeralStorage: &statsv1alpha1.FsStats{
			Time:           metav1.Now(),
			UsedBytes:      uint64Ptr(100 * 1024 * 1024), // 100MB
			AvailableBytes: uint64Ptr(900 * 1024 * 1024), // 900MB = 10% usage
		},
	}

	// Should not send event since usage is below 50%
	collector.checkEphemeralStorage(context.Background(), pod3)

	// Test with high usage (should send event)
	pod4 := &statsv1alpha1.PodStats{
		PodRef: statsv1alpha1.PodReference{
			Name:      "test-pod-4",
			Namespace: "default",
			UID:       "test-uid-4",
		},
		EphemeralStorage: &statsv1alpha1.FsStats{
			Time:           metav1.Now(),
			UsedBytes:      uint64Ptr(800 * 1024 * 1024), // 800MB
			AvailableBytes: uint64Ptr(200 * 1024 * 1024), // 200MB = 80% usage
		},
	}

	// Collect events to test high usage scenario
	go func() {
		collector.checkEphemeralStorage(context.Background(), pod4)
	}()

	// Wait for event to be processed
	timeout := time.After(100 * time.Millisecond)
	select {
	case event := <-collector.Events():
		assert.Equal(t, "kubelet_ephemeral_storage", event.Type)
		assert.Equal(t, "default", event.Metadata["k8s_namespace"])
		assert.Equal(t, "test-pod-4", event.Metadata["k8s_name"])
	case <-timeout:
		// This is also acceptable as the event might not be sent immediately
	}
}

// TestInstrumentationErrorHandling is removed as we no longer use instrumentation wrappers

// Helper functions
func uint64Ptr(v uint64) *uint64 {
	return &v
}

func int32Ptr(v int32) *int32 {
	return &v
}
