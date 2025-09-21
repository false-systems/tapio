package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestNewObserver tests observer creation
func TestNewObserver(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		expectErr bool
	}{
		{
			name: "default config without K8s",
			config: &Config{
				BufferSize:          100,
				ConnectionTableSize: 1000,
				ConnectionTimeout:   time.Minute,
				CleanupInterval:     time.Second,
				EnableK8sMapping:    false, // Disable K8s to avoid client creation
			},
			expectErr: false,
		},
		{
			name: "custom config without K8s",
			config: &Config{
				BufferSize:          200,
				ConnectionTableSize: 2000,
				ConnectionTimeout:   2 * time.Minute,
				CleanupInterval:     2 * time.Second,
				EnableK8sMapping:    false,
			},
			expectErr: false,
		},
		{
			name: "invalid config - zero buffer",
			config: &Config{
				BufferSize:          0,
				ConnectionTableSize: 1000,
				ConnectionTimeout:   time.Minute,
				CleanupInterval:     time.Second,
			},
			expectErr: true,
		},
		{
			name: "config with K8s disabled",
			config: &Config{
				BufferSize:          100,
				ConnectionTableSize: 1000,
				ConnectionTimeout:   time.Minute,
				CleanupInterval:     time.Second,
				EnableK8sMapping:    false,
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			observer, err := NewObserver("test", tt.config, logger)

			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, observer)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, observer)
				assert.Equal(t, "test", observer.name)
				assert.NotNil(t, observer.connectionsTracker)

				// Check K8s enricher based on config
				if tt.config != nil && !tt.config.EnableK8sMapping {
					assert.Nil(t, observer.k8sMapper)
				}
			}
		})
	}
}

// TestObserverLifecycle tests Start and Stop
func TestObserverLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		BufferSize:          100,
		ConnectionTableSize: 1000,
		ConnectionTimeout:   time.Minute,
		CleanupInterval:     time.Second,
		EnableK8sMapping:    false, // Disable K8s to avoid client creation
	}

	observer, err := NewObserver("test", config, logger)
	require.NoError(t, err)
	require.NotNil(t, observer)

	ctx := context.Background()

	// Test Start
	err = observer.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, observer.IsHealthy())

	// Give some time for goroutines to start
	time.Sleep(100 * time.Millisecond)

	// Test Stop
	err = observer.Stop()
	assert.NoError(t, err)
	assert.False(t, observer.IsHealthy())
}

// TestObserverEvents tests event channel
func TestObserverEvents(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		BufferSize:          10,
		ConnectionTableSize: 100,
		ConnectionTimeout:   time.Minute,
		CleanupInterval:     time.Second,
		EnableK8sMapping:    false,
	}

	observer, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	// Get event channel
	eventCh := observer.Events()
	assert.NotNil(t, eventCh)

	// Start observer
	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Send test event through the channel manager
	testEvent := &domain.CollectorEvent{
		EventID:   "test-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeNetworkConnection,
		Source:    "test",
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				EventType: "connect",
				Protocol:  "TCP",
				SrcIP:     "10.0.0.1",
				DstIP:     "10.0.0.2",
				SrcPort:   8080,
				DstPort:   3306,
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "test",
				"version":  "1.0.0",
			},
		},
	}

	sent := observer.EventChannelManager.SendEvent(testEvent)
	assert.True(t, sent)

	// Receive event
	select {
	case event := <-eventCh:
		assert.Equal(t, "test-1", event.EventID)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Stop observer
	err = observer.Stop()
	assert.NoError(t, err)
}

// TestObserverStatistics tests Statistics method
func TestObserverStatistics(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		BufferSize:          100,
		ConnectionTableSize: 1000,
		ConnectionTimeout:   time.Minute,
		CleanupInterval:     time.Second,
		EnableK8sMapping:    false,
	}
	observer, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	stats := observer.Statistics()
	assert.NotNil(t, stats)
	assert.Equal(t, int64(0), stats.EventsProcessed)
	assert.Equal(t, int64(0), stats.ErrorCount)
	assert.False(t, stats.LastEventTime.IsZero())
}

// TestObserverGetStats tests GetStats method
func TestObserverGetStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		BufferSize:          100,
		ConnectionTableSize: 1000,
		ConnectionTimeout:   time.Minute,
		CleanupInterval:     time.Second,
		EnableK8sMapping:    false,
	}
	observer, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	stats := observer.GetStats()
	assert.Equal(t, uint64(0), stats.ActiveConnections)
	assert.Equal(t, uint64(0), stats.ServicesDiscovered)
	assert.Equal(t, uint64(0), stats.ServiceFlows)
	assert.False(t, stats.K8sMappingEnabled) // We disabled K8s in config
}

// TestObserverGetServiceMap tests GetServiceMap method
func TestObserverGetServiceMap(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test without K8s
	config := &Config{
		BufferSize:          100,
		ConnectionTableSize: 1000,
		ConnectionTimeout:   time.Minute,
		CleanupInterval:     time.Second,
		EnableK8sMapping:    false,
	}

	observer, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	serviceMap := observer.GetServiceMap()
	assert.NotNil(t, serviceMap)
	assert.Empty(t, serviceMap)
}

// TestProcessConnectionEvents tests connection event processing
func TestProcessConnectionEvents(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		BufferSize:          10,
		ConnectionTableSize: 100,
		ConnectionTimeout:   time.Minute,
		CleanupInterval:     time.Second,
		EnableK8sMapping:    false,
	}

	observer, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a test connection event
	testEvent := &ConnectionEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		EventType: ConnectionConnect,
		PID:       1234,
		SrcPort:   8080,
		DstPort:   3306,
	}
	copy(testEvent.SrcIP[:], []byte("10.0.0.1"))
	copy(testEvent.DstIP[:], []byte("10.0.0.2"))
	copy(testEvent.Comm[:], []byte("test-app"))

	// Start event processing in background
	go observer.processConnectionEvents(ctx)

	// Send event to tracker
	observer.connectionsTracker.eventCh <- testEvent

	// Should receive processed event
	select {
	case event := <-observer.Events():
		assert.NotNil(t, event)
		assert.Equal(t, domain.EventTypeNetworkConnection, event.Type)
		assert.NotNil(t, event.EventData.Network)
		assert.Equal(t, "TCP", event.EventData.Network.Protocol)
		assert.Equal(t, int32(8080), event.EventData.Network.SrcPort)
		assert.Equal(t, int32(3306), event.EventData.Network.DstPort)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for processed event")
	}
}

// TestSendConnectionEvent tests sendConnectionEvent
func TestSendConnectionEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		BufferSize:          100,
		ConnectionTableSize: 1000,
		ConnectionTimeout:   time.Minute,
		CleanupInterval:     time.Second,
		EnableK8sMapping:    false,
	}
	observer, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Test with nil event
	observer.sendConnectionEvent(ctx, nil)
	// Should not panic

	// Test with valid event
	testEvent := &ConnectionEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		EventType: ConnectionConnect,
		PID:       5678,
		SrcPort:   9090,
		DstPort:   443,
	}
	copy(testEvent.SrcIP[:], []byte("192.168.1.1"))
	copy(testEvent.DstIP[:], []byte("192.168.1.2"))
	copy(testEvent.Comm[:], []byte("curl"))

	observer.sendConnectionEvent(ctx, testEvent)

	// Check if event was sent
	select {
	case event := <-observer.Events():
		assert.NotNil(t, event)
		assert.Contains(t, event.EventID, "conn-5678")
		assert.Equal(t, domain.EventTypeNetworkConnection, event.Type)
		assert.Equal(t, "test", event.Source)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected event not received")
	}
}

// TestFlowTypeString tests flowTypeString helper
func TestFlowTypeString(t *testing.T) {
	tests := []struct {
		flowType FlowType
		expected string
	}{
		{FlowIntraNamespace, "intra-namespace"},
		{FlowInterNamespace, "inter-namespace"},
		{FlowExternal, "external"},
		{FlowIngress, "ingress"},
		{FlowEgress, "egress"},
		{FlowType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := flowTypeString(tt.flowType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestConfigValidate tests Config validation
func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		expectErr bool
		errMsg    string
	}{
		{
			name:      "valid config",
			config:    DefaultConfig(),
			expectErr: false,
		},
		{
			name: "invalid connection table size",
			config: &Config{
				ConnectionTableSize: 0,
				ConnectionTimeout:   time.Minute,
				BufferSize:          100,
				CleanupInterval:     time.Second,
			},
			expectErr: true,
			errMsg:    "connection_table_size must be positive",
		},
		{
			name: "invalid connection timeout",
			config: &Config{
				ConnectionTableSize: 1000,
				ConnectionTimeout:   0,
				BufferSize:          100,
				CleanupInterval:     time.Second,
			},
			expectErr: true,
			errMsg:    "connection_timeout must be positive",
		},
		{
			name: "invalid buffer size",
			config: &Config{
				ConnectionTableSize: 1000,
				ConnectionTimeout:   time.Minute,
				BufferSize:          0,
				CleanupInterval:     time.Second,
			},
			expectErr: true,
			errMsg:    "buffer_size must be positive",
		},
		{
			name: "invalid K8s refresh interval",
			config: &Config{
				ConnectionTableSize: 1000,
				ConnectionTimeout:   time.Minute,
				BufferSize:          100,
				CleanupInterval:     time.Second,
				EnableK8sMapping:    true,
				K8sRefreshInterval:  0,
				PodMappingTimeout:   time.Second,
			},
			expectErr: true,
			errMsg:    "k8s_refresh_interval must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestConfigGetEnabledLevels tests GetEnabledLevels
func TestConfigGetEnabledLevels(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected []int
	}{
		{
			name: "only connection tracking",
			config: &Config{
				EnableK8sMapping: false,
			},
			expected: []int{1},
		},
		{
			name: "with K8s mapping",
			config: &Config{
				EnableK8sMapping: true,
			},
			expected: []int{1, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			levels := tt.config.GetEnabledLevels()
			assert.Equal(t, tt.expected, levels)
		})
	}
}

// TestUpdateStats tests updateStats method
func TestUpdateStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		BufferSize:          100,
		ConnectionTableSize: 1000,
		ConnectionTimeout:   time.Minute,
		CleanupInterval:     time.Second,
		EnableK8sMapping:    false,
	}

	observer, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	// Update connection tracker stats
	observer.connectionsTracker.mu.Lock()
	observer.connectionsTracker.stats.ActiveConnections = 5
	observer.connectionsTracker.mu.Unlock()

	// Call updateStats
	observer.updateStats()

	// Verify stats were updated
	stats := observer.GetStats()
	assert.Equal(t, uint64(5), stats.ActiveConnections)
}

// BenchmarkSendConnectionEvent benchmarks event sending
func BenchmarkSendConnectionEvent(b *testing.B) {
	logger := zap.NewNop()
	observer, err := NewObserver("bench", nil, logger)
	require.NoError(b, err)

	ctx := context.Background()
	testEvent := &ConnectionEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		EventType: ConnectionConnect,
		PID:       1234,
		SrcPort:   8080,
		DstPort:   3306,
	}

	// Drain events in background
	go func() {
		for range observer.Events() {
			// Drain
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		observer.sendConnectionEvent(ctx, testEvent)
	}
}
