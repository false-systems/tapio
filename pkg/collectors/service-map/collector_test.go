package servicemap

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewCollector(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("creates collector with default config", func(t *testing.T) {
		collector, err := NewCollector("test-service-map", nil, logger)
		require.NoError(t, err)
		assert.NotNil(t, collector)
		assert.Equal(t, "service-map", collector.Name())
	})

	t.Run("creates collector with custom config", func(t *testing.T) {
		config := &Config{
			Enabled:            true,
			BufferSize:         5000,
			EnableK8sDiscovery: false,
			EnableEBPF:         false,
		}

		collector, err := NewCollector("test-service-map", config, logger)
		require.NoError(t, err)
		assert.NotNil(t, collector)
		assert.Equal(t, 5000, collector.config.BufferSize)
		assert.False(t, collector.config.EnableK8sDiscovery)
	})
}

func TestServiceDetection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		AutoDetectType: true,
		PortMappings:   defaultPortMappings(),
		ImagePatterns:  defaultImagePatterns(),
	}

	collector, err := NewCollector("test", config, logger)
	require.NoError(t, err)

	// Test should exclude namespace
	tests := []struct {
		name      string
		namespace string
		exclude   bool
	}{
		{"system namespace", "kube-system", true},
		{"public namespace", "kube-public", true},
		{"user namespace", "default", false},
		{"app namespace", "production", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.config.IgnoreSystemNamespaces = true
			excluded := collector.shouldExcludeNamespace(tt.namespace)
			assert.Equal(t, tt.exclude, excluded)
		})
	}
}

func TestConnectionTracking(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		EnableEBPF:    false, // Don't use eBPF in tests
		ConnectionTTL: 1 * time.Second,
		BufferSize:    100,
	}

	collector, err := NewCollector("test", config, logger)
	require.NoError(t, err)

	// Add test connection
	conn := &Connection{
		SourceIP:   0x0100007F, // 127.0.0.1
		DestIP:     0x0200007F, // 127.0.0.2
		SourcePort: 8080,
		DestPort:   3306,
		Protocol:   6, // TCP
		Timestamp:  time.Now(),
	}

	collector.mu.Lock()
	connKey := "127.0.0.1:8080->127.0.0.2:3306"
	collector.connections[connKey] = conn
	collector.mu.Unlock()

	// Verify connection exists
	collector.mu.RLock()
	storedConn, exists := collector.connections[connKey]
	collector.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, uint16(8080), storedConn.SourcePort)
	assert.Equal(t, uint16(3306), storedConn.DestPort)
}

func TestHealthAndStatistics(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.EnableK8sDiscovery = false
	config.EnableEBPF = false

	collector, err := NewCollector("test", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Check health
	health := collector.Health()
	assert.NotNil(t, health)

	// Check statistics
	stats := collector.Statistics()
	assert.NotNil(t, stats)
	assert.GreaterOrEqual(t, stats.EventsProcessed, int64(0))

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)
}

func TestFilterSetup(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.IgnoreSystemNamespaces = true
	config.MinConnectionCount = 5
	config.IncludeExternalServices = false

	collector, err := NewCollector("test", config, logger)
	require.NoError(t, err)

	// Setup filters
	collector.setupDefaultFilters()

	// Verify filter statistics
	filterStats := collector.GetFilterStatistics()
	assert.NotNil(t, filterStats)
	// Should have created deny filters
	assert.Greater(t, filterStats.DenyFilters, 0)
}

func TestIPConversion(t *testing.T) {
	tests := []struct {
		ip       uint32
		expected string
	}{
		{0x0100007F, "127.0.0.1"},
		{0x0A000001, "1.0.0.10"},
		{0xC0A80001, "1.0.168.192"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := intToIP(tt.ip)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProtocolConversion(t *testing.T) {
	tests := []struct {
		protocol uint8
		expected string
	}{
		{6, "TCP"},
		{17, "UDP"},
		{1, "PROTO_1"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := protocolToString(tt.protocol)
			assert.Equal(t, tt.expected, result)
		})
	}
}
