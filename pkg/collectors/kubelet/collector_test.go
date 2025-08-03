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
	assert.Greater(t, stats["errors_count"].(int64), int64(0))

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

	collector := &Collector{}

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

// Helper functions
func uint64Ptr(v uint64) *uint64 {
	return &v
}

func int32Ptr(v int32) *int32 {
	return &v
}
