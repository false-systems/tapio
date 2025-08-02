package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// mockEtcdServer creates an embedded etcd server for testing
func setupTestEtcd(t *testing.T) (*embed.Etcd, *clientv3.Client, func()) {
	cfg := embed.NewConfig()
	cfg.Dir = t.TempDir()
	cfg.LogLevel = "warn"
	cfg.Logger = "zap"

	// Use random available ports
	peerURL, _ := url.Parse("http://localhost:0")
	clientURL, _ := url.Parse("http://localhost:0")
	cfg.ListenPeerUrls = []url.URL{*peerURL}
	cfg.ListenClientUrls = []url.URL{*clientURL}

	e, err := embed.StartEtcd(cfg)
	require.NoError(t, err)

	select {
	case <-e.Server.ReadyNotify():
	case <-time.After(10 * time.Second):
		t.Fatal("etcd server took too long to start")
	}

	// Create client
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{e.Clients[0].Addr().String()},
		DialTimeout: 5 * time.Second,
	})
	require.NoError(t, err)

	cleanup := func() {
		client.Close()
		e.Close()
	}

	return e, client, cleanup
}

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected Config
	}{
		{
			name:   "default config",
			config: Config{},
			expected: Config{
				Endpoints: []string{"localhost:2379"},
			},
		},
		{
			name: "custom config",
			config: Config{
				Endpoints: []string{"etcd1:2379", "etcd2:2379"},
				Username:  "user",
				Password:  "pass",
			},
			expected: Config{
				Endpoints: []string{"etcd1:2379", "etcd2:2379"},
				Username:  "user",
				Password:  "pass",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector("test-etcd", tt.config)
			require.NoError(t, err)
			assert.Equal(t, "test-etcd", collector.Name())
			assert.Equal(t, tt.expected, collector.config)
			assert.True(t, collector.IsHealthy())
		})
	}
}

func TestCollectorBasicOperations(t *testing.T) {
	collector, err := NewCollector("test", Config{})
	require.NoError(t, err)

	// Test Name
	assert.Equal(t, "test", collector.Name())

	// Test IsHealthy before start
	assert.True(t, collector.IsHealthy())

	// Test Events channel
	events := collector.Events()
	assert.NotNil(t, events)

	// Test Stop before start (should not panic)
	err = collector.Stop()
	assert.NoError(t, err)
}

func TestCollectorStartStop(t *testing.T) {
	_, testClient, cleanup := setupTestEtcd(t)
	defer cleanup()

	// Get the etcd endpoint from test client
	endpoints := testClient.Endpoints()

	config := Config{
		Endpoints: endpoints,
	}

	collector, err := NewCollector("test-etcd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test Start
	err = collector.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// Test double start should fail
	err = collector.Start(ctx)
	assert.Error(t, err)

	// Test Stop
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

func TestCollectorWatchRegistry(t *testing.T) {
	_, testClient, cleanup := setupTestEtcd(t)
	defer cleanup()

	// Get the etcd endpoint from test client
	endpoints := testClient.Endpoints()

	config := Config{
		Endpoints: endpoints,
	}

	collector, err := NewCollector("test-etcd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Give collector time to start watching
	time.Sleep(100 * time.Millisecond)

	// Put some K8s-like data in etcd
	testData := map[string]string{
		"/registry/pods/default/test-pod":         `{"kind":"Pod","metadata":{"name":"test-pod"}}`,
		"/registry/services/default/test-service": `{"kind":"Service","metadata":{"name":"test-service"}}`,
		"/registry/configmaps/default/test-cm":    `{"kind":"ConfigMap","metadata":{"name":"test-cm"}}`,
	}

	// Put data and collect events
	var events []collectors.RawEvent
	eventsChan := collector.Events()

	// Put test data
	for key, value := range testData {
		_, err := testClient.Put(ctx, key, value)
		require.NoError(t, err)
	}

	// Collect events with timeout
	timeout := time.After(2 * time.Second)
	for len(events) < len(testData) {
		select {
		case event := <-eventsChan:
			events = append(events, event)
		case <-timeout:
			t.Fatalf("timeout waiting for events, got %d, expected %d", len(events), len(testData))
		}
	}

	// Verify events
	assert.Len(t, events, len(testData))

	for _, event := range events {
		assert.Equal(t, "etcd", event.Type)
		assert.Equal(t, "test-etcd", event.Metadata["collector"])
		assert.Equal(t, "PUT", event.Metadata["event"])
		assert.NotEmpty(t, event.Metadata["resource_type"])
		assert.NotEmpty(t, event.TraceID)
		assert.NotEmpty(t, event.SpanID)

		// Parse event data
		var eventData map[string]interface{}
		err := json.Unmarshal(event.Data, &eventData)
		require.NoError(t, err)

		assert.Contains(t, eventData, "key")
		assert.Contains(t, eventData, "value")
		assert.Contains(t, eventData, "resource_type")
		assert.Contains(t, eventData, "mod_revision")
	}
}

func TestCollectorDeleteEvents(t *testing.T) {
	_, testClient, cleanup := setupTestEtcd(t)
	defer cleanup()

	endpoints := testClient.Endpoints()

	config := Config{
		Endpoints: endpoints,
	}

	collector, err := NewCollector("test-etcd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	time.Sleep(100 * time.Millisecond)

	// First put a key
	testKey := "/registry/pods/default/test-pod"
	testValue := `{"kind":"Pod","metadata":{"name":"test-pod"}}`

	_, err = testClient.Put(ctx, testKey, testValue)
	require.NoError(t, err)

	// Wait for PUT event
	eventsChan := collector.Events()
	select {
	case event := <-eventsChan:
		assert.Equal(t, "PUT", event.Metadata["event"])
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for PUT event")
	}

	// Now delete the key
	_, err = testClient.Delete(ctx, testKey)
	require.NoError(t, err)

	// Wait for DELETE event
	select {
	case event := <-eventsChan:
		assert.Equal(t, "DELETE", event.Metadata["event"])
		assert.Equal(t, "etcd", event.Type)
		assert.Equal(t, "pods", event.Metadata["resource_type"])
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for DELETE event")
	}
}

func TestExtractResourceType(t *testing.T) {
	collector, _ := NewCollector("test", Config{})

	tests := []struct {
		key      string
		expected string
	}{
		{"/registry/pods/default/test-pod", "pods"},
		{"/registry/services/kube-system/kube-dns", "services"},
		{"/registry/configmaps/default/my-config", "configmaps"},
		{"/registry/secrets/default/my-secret", "secrets"},
		{"/registry/nodes/worker-1", "nodes"},
		{"/not-registry/something", "unknown"},
		{"/registry", "unknown"},
		{"invalid-key", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := collector.extractResourceType(tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCollectorHealth(t *testing.T) {
	collector, err := NewCollector("test", Config{})
	require.NoError(t, err)

	// Test initial health
	healthy, details := collector.Health()
	assert.True(t, healthy)
	assert.Contains(t, details, "healthy")
	assert.Contains(t, details, "events_collected")
	assert.Contains(t, details, "events_dropped")
	assert.Contains(t, details, "error_count")
	assert.Contains(t, details, "client_connected")
	assert.Equal(t, false, details["client_connected"]) // No client before start

	// Test statistics
	stats := collector.Statistics()
	assert.Contains(t, stats, "events_collected")
	assert.Contains(t, stats, "events_dropped")
	assert.Contains(t, stats, "error_count")
	// Performance metrics should be present
	assert.Contains(t, stats, "perf_buffer_size")
	assert.Contains(t, stats, "perf_buffer_capacity")
	assert.Contains(t, stats, "perf_buffer_utilization")
	assert.Contains(t, stats, "perf_batches_processed")
	assert.Contains(t, stats, "perf_pool_in_use")
}

func TestCollectorConnectionFailure(t *testing.T) {
	// Use invalid endpoint
	config := Config{
		Endpoints: []string{"localhost:9999"}, // Non-existent endpoint
	}

	collector, err := NewCollector("test-etcd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start should fail due to connection error
	err = collector.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to etcd")
}

func TestCollectorConcurrency(t *testing.T) {
	_, testClient, cleanup := setupTestEtcd(t)
	defer cleanup()

	endpoints := testClient.Endpoints()

	config := Config{
		Endpoints: endpoints,
	}

	collector, err := NewCollector("test-etcd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	time.Sleep(100 * time.Millisecond)

	// Concurrent reads of health and statistics
	done := make(chan bool, 10)

	// Start multiple goroutines reading health and stats
	for i := 0; i < 5; i++ {
		go func() {
			defer func() { done <- true }()
			for j := 0; j < 10; j++ {
				collector.Health()
				collector.Statistics()
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	// Generate events while reading stats
	go func() {
		defer func() { done <- true }()
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("/registry/pods/default/pod-%d", i)
			value := fmt.Sprintf(`{"name":"pod-%d"}`, i)
			testClient.Put(ctx, key, value)
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// Wait for all goroutines
	for i := 0; i < 6; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for concurrent operations")
		}
	}

	// Verify collector is still healthy
	healthy, _ := collector.Health()
	assert.True(t, healthy)
}

func TestCollectorInterface(t *testing.T) {
	collector, err := NewCollector("test-etcd", Config{})
	require.NoError(t, err)

	// Verify it implements collectors.Collector
	var _ collectors.Collector = collector
}

func TestEventCreation(t *testing.T) {
	collector, err := NewCollector("test-etcd", Config{})
	require.NoError(t, err)

	// Test event creation
	event := collector.createEvent("test_event", map[string]interface{}{
		"key":   "value",
		"num":   123,
		"array": []int{1, 2, 3},
	})

	assert.Equal(t, "etcd", event.Type)
	assert.Equal(t, "test-etcd", event.Metadata["collector"])
	assert.Equal(t, "test_event", event.Metadata["event"])
	assert.NotNil(t, event.Data)
	assert.False(t, event.Timestamp.IsZero())
	assert.NotEmpty(t, event.TraceID)
	assert.NotEmpty(t, event.SpanID)
}

func TestPerformanceAdapterIntegration(t *testing.T) {
	_, testClient, cleanup := setupTestEtcd(t)
	defer cleanup()

	endpoints := testClient.Endpoints()

	config := Config{
		Endpoints: endpoints,
	}

	collector, err := NewCollector("test-etcd", config)
	require.NoError(t, err)
	assert.NotNil(t, collector.perfAdapter)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Give collector time to start
	time.Sleep(100 * time.Millisecond)

	// Generate many events quickly to test performance adapter
	eventsCount := 1000
	for i := 0; i < eventsCount; i++ {
		key := fmt.Sprintf("/registry/pods/default/perf-test-%d", i)
		value := fmt.Sprintf(`{"name":"perf-test-%d"}`, i)
		testClient.Put(ctx, key, value)
	}

	// Collect events with timeout
	events := make([]collectors.RawEvent, 0)
	eventsChan := collector.Events()
	timeout := time.After(5 * time.Second)

collectLoop:
	for len(events) < eventsCount {
		select {
		case event := <-eventsChan:
			events = append(events, event)
		case <-timeout:
			break collectLoop
		}
	}

	// Should have collected most events (allow some drops due to timing)
	assert.GreaterOrEqual(t, len(events), eventsCount*8/10) // At least 80%

	// Check performance metrics
	stats := collector.Statistics()
	assert.Greater(t, stats["perf_batches_processed"].(uint64), uint64(0))
	assert.Greater(t, stats["perf_buffer_capacity"].(uint64), uint64(0))

	// Verify events are properly formatted
	if len(events) > 0 {
		event := events[0]
		assert.Equal(t, "etcd", event.Type)
		assert.NotEmpty(t, event.TraceID)
		assert.NotEmpty(t, event.SpanID)
	}
}
