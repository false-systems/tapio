package cni

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBasicCNICollector(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval:     5 * time.Second,
		CNIConfigPath:          "/etc/cni/net.d",
		CNIBinPath:             "/opt/cni/bin",
		SupportedCNIPlugins:    []string{"calico", "flannel"},
		EnableNetworkFlows:     true,
		EnableDNSMonitoring:    true,
		EnablePolicyMonitoring: true,
		FlowCacheSize:          1000,
		DNSCacheSize:           500,
		MaxConcurrentFlows:     100,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)
	assert.NotNil(t, collector)

	// Test basic properties
	assert.Equal(t, "cni-collector", collector.name)
	assert.NotNil(t, collector.config)
	assert.NotNil(t, collector.metrics)
	assert.NotNil(t, collector.flowCache)
	assert.NotNil(t, collector.meshAnalyzer)
}

func TestCNICollectorGetMetrics(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
		EnableNetworkFlows: true,
		FlowCacheSize:      100,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	metrics := collector.GetMetrics()

	// Verify basic metrics exist
	assert.Contains(t, metrics, "events_collected")
	assert.Contains(t, metrics, "flows_tracked")
	assert.Contains(t, metrics, "dns_queries")
	assert.Contains(t, metrics, "policy_violations")
	assert.Contains(t, metrics, "uptime_seconds")
	assert.Contains(t, metrics, "cpu_usage_percent")
	assert.Contains(t, metrics, "memory_usage_mb")
}

func TestFlowCacheBasic(t *testing.T) {
	cache := NewFlowCache(3, 1*time.Minute)

	// Test flow addition
	flow := &NetworkFlow{
		FlowID: "test-flow-1",
		State:  FlowStateActive,
	}

	cache.AddFlow(flow)

	// Test flow retrieval
	retrieved := cache.GetFlow("test-flow-1")
	assert.NotNil(t, retrieved)
	assert.Equal(t, "test-flow-1", retrieved.FlowID)

	// Test active flows
	activeFlows := cache.GetActiveFlows()
	assert.Len(t, activeFlows, 1)

	// Test stats
	stats := cache.GetStats()
	assert.Equal(t, 1, stats.TotalFlows)
	assert.Equal(t, 1, stats.ActiveFlows)
}

func TestNetworkFlowBasic(t *testing.T) {
	flow := &NetworkFlow{
		FlowID:          "test-flow",
		SourceIP:        []byte{10, 244, 1, 10},
		DestinationIP:   []byte{10, 244, 2, 20},
		SourcePort:      8080,
		DestinationPort: 80,
		Protocol:        6, // TCP
		State:           FlowStateActive,
		StartTime:       time.Now().Add(-1 * time.Minute),
		LastSeen:        time.Now(),
	}

	// Test key generation
	key := flow.GenerateKey()
	assert.Contains(t, key, "10.244.1.10")
	assert.Contains(t, key, "10.244.2.20")
	assert.Contains(t, key, "8080")
	assert.Contains(t, key, "80")
	assert.Contains(t, key, "6")

	// Test RTT calculation
	rtt := flow.CalculateRTT()
	assert.Greater(t, rtt, 50*time.Millisecond)
}

func TestCNIPluginBasic(t *testing.T) {
	// Test Calico plugin creation
	calicoPlugin, err := NewCalicoPlugin("/test/config", "/test/bin")
	assert.NoError(t, err)
	assert.Equal(t, "calico", calicoPlugin.Name())
	assert.Equal(t, "v3.20.0", calicoPlugin.Version())

	config, err := calicoPlugin.GetNetworkConfig()
	assert.NoError(t, err)
	assert.Equal(t, "calico-network", config.Name)

	// Test Flannel plugin creation
	flannelPlugin, err := NewFlannelPlugin("/test/config", "/test/bin")
	assert.NoError(t, err)
	assert.Equal(t, "flannel", flannelPlugin.Name())
	assert.Equal(t, "v0.15.1", flannelPlugin.Version())
}
