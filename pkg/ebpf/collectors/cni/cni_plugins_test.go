package cni

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/events/opinionated"
)

func TestNetworkConfig_Validation(t *testing.T) {
	config := &NetworkConfig{
		CNIVersion: "0.4.0",
		Name:       "test-network",
		Type:       "calico",
		IPAM: &IPAMConfig{
			Type:    "calico-ipam",
			Subnet:  "10.244.0.0/16",
			Gateway: "10.244.0.1",
		},
		DNS: &DNSConfig{
			Nameservers: []string{"10.96.0.10"},
			Domain:      "cluster.local",
		},
		Capabilities: map[string]bool{
			"portMappings": true,
			"bandwidth":    true,
		},
	}

	assert.Equal(t, "0.4.0", config.CNIVersion)
	assert.Equal(t, "test-network", config.Name)
	assert.Equal(t, "calico", config.Type)
	assert.NotNil(t, config.IPAM)
	assert.NotNil(t, config.DNS)
	assert.True(t, config.Capabilities["portMappings"])
}

func TestCNIEvent_Validation(t *testing.T) {
	event := &CNIEvent{
		Type:      "ADD",
		Timestamp: time.Now(),
		PodName:   "test-pod",
		Namespace: "default",
		PodIP:     "10.244.1.10",
		Interface: "eth0",
		Duration:  50 * time.Millisecond,
		Result: &CNIResult{
			CNIVersion: "0.4.0",
			Interfaces: []Interface{
				{
					Name:    "eth0",
					Mac:     "02:42:ac:11:00:02",
					Sandbox: "/var/run/netns/test",
				},
			},
			IPs: []IPConfig{
				{
					Version:   "4",
					Interface: 0,
					Address:   "10.244.1.10/24",
					Gateway:   "10.244.1.1",
				},
			},
		},
		Metadata: map[string]interface{}{
			"plugin": "calico",
			"node":   "worker-1",
		},
	}

	assert.Equal(t, "ADD", event.Type)
	assert.Equal(t, "test-pod", event.PodName)
	assert.Equal(t, "default", event.Namespace)
	assert.Equal(t, "10.244.1.10", event.PodIP)
	assert.Equal(t, "eth0", event.Interface)
	assert.NotNil(t, event.Result)
	assert.Len(t, event.Result.Interfaces, 1)
	assert.Len(t, event.Result.IPs, 1)
}

func TestCalicoPlugin_Creation(t *testing.T) {
	// Create temporary config file
	tempDir, err := ioutil.TempDir("", "cni-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "10-calico.conf")
	config := NetworkConfig{
		CNIVersion: "0.4.0",
		Name:       "k8s-pod-network",
		Type:       "calico",
		Calico: &CalicoConfig{
			LogLevel: "info",
			Policy:   true,
			IPAMType: "calico-ipam",
		},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	err = ioutil.WriteFile(configPath, configData, 0644)
	require.NoError(t, err)

	plugin, err := NewCalicoPlugin(configPath, "/opt/cni/bin")
	require.NoError(t, err)

	assert.Equal(t, "calico", plugin.Name())
	assert.NotEmpty(t, plugin.Version())

	networkConfig, err := plugin.GetNetworkConfig()
	assert.NoError(t, err)
	assert.Equal(t, "k8s-pod-network", networkConfig.Name)
	assert.Equal(t, "calico", networkConfig.Type)

	metrics := plugin.GetMetrics()
	assert.Equal(t, "calico", metrics["plugin"])
	assert.Contains(t, metrics, "version")
	assert.Contains(t, metrics, "policy_enabled")
}

func TestFlannelPlugin_Creation(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "cni-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "10-flannel.conf")
	config := NetworkConfig{
		CNIVersion: "0.4.0",
		Name:       "cbr0",
		Type:       "flannel",
		Flannel: &FlannelConfig{
			Network:   "10.244.0.0/16",
			SubnetLen: 24,
			Backend: &struct {
				Type string `json:"Type,omitempty"`
				VNI  int    `json:"VNI,omitempty"`
				Port int    `json:"Port,omitempty"`
			}{
				Type: "vxlan",
				VNI:  1,
				Port: 8472,
			},
		},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	err = ioutil.WriteFile(configPath, configData, 0644)
	require.NoError(t, err)

	plugin, err := NewFlannelPlugin(configPath, "/opt/cni/bin")
	require.NoError(t, err)

	assert.Equal(t, "flannel", plugin.Name())
	assert.NotEmpty(t, plugin.Version())

	networkConfig, err := plugin.GetNetworkConfig()
	assert.NoError(t, err)
	assert.Equal(t, "cbr0", networkConfig.Name)
	assert.Equal(t, "flannel", networkConfig.Type)

	metrics := plugin.GetMetrics()
	assert.Equal(t, "flannel", metrics["plugin"])
	assert.Contains(t, metrics, "backend_type")
}

func TestCiliumPlugin_Creation(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "cni-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "05-cilium.conf")
	config := NetworkConfig{
		CNIVersion: "0.4.0",
		Name:       "cilium",
		Type:       "cilium-cni",
		Cilium: &CiliumConfig{
			Debug:           false,
			EnablePolicy:    true,
			EnableLogging:   true,
			MTU:             1500,
			ClusterPoolIPv4: "10.244.0.0/16",
		},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	err = ioutil.WriteFile(configPath, configData, 0644)
	require.NoError(t, err)

	plugin, err := NewCiliumPlugin(configPath, "/opt/cni/bin")
	require.NoError(t, err)

	assert.Equal(t, "cilium", plugin.Name())
	assert.NotEmpty(t, plugin.Version())

	networkConfig, err := plugin.GetNetworkConfig()
	assert.NoError(t, err)
	assert.Equal(t, "cilium", networkConfig.Name)
	assert.Equal(t, "cilium-cni", networkConfig.Type)

	metrics := plugin.GetMetrics()
	assert.Equal(t, "cilium", metrics["plugin"])
	assert.Contains(t, metrics, "policy_enabled")
	assert.Contains(t, metrics, "debug_enabled")
}

func TestWeavePlugin_Creation(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "cni-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "10-weave.conf")
	config := NetworkConfig{
		CNIVersion: "0.4.0",
		Name:       "weave",
		Type:       "weave-net",
		Weave: &WeaveConfig{
			IPAM:             true,
			Subnet:           "10.244.0.0/16",
			MTU:              1376,
			HairpinMode:      true,
			ExposeStats:      true,
			DiscoveryEnabled: true,
		},
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	err = ioutil.WriteFile(configPath, configData, 0644)
	require.NoError(t, err)

	plugin, err := NewWeavePlugin(configPath, "/opt/cni/bin")
	require.NoError(t, err)

	assert.Equal(t, "weave", plugin.Name())
	assert.NotEmpty(t, plugin.Version())

	networkConfig, err := plugin.GetNetworkConfig()
	assert.NoError(t, err)
	assert.Equal(t, "weave", networkConfig.Name)
	assert.Equal(t, "weave-net", networkConfig.Type)

	metrics := plugin.GetMetrics()
	assert.Equal(t, "weave", metrics["plugin"])
	assert.Contains(t, metrics, "ipam_enabled")
	assert.Contains(t, metrics, "mtu")
}

func TestCNIPlugin_MonitorEvents(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "cni-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "10-calico.conf")
	config := NetworkConfig{
		CNIVersion: "0.4.0",
		Name:       "test-network",
		Type:       "calico",
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	err = ioutil.WriteFile(configPath, configData, 0644)
	require.NoError(t, err)

	plugin, err := NewCalicoPlugin(configPath, "/opt/cni/bin")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	eventCh := make(chan *CNIEvent, 10)

	// Start monitoring events
	go func() {
		plugin.MonitorEvents(ctx, eventCh)
	}()

	// Wait for an event or timeout
	select {
	case event := <-eventCh:
		assert.NotNil(t, event)
		assert.Equal(t, "ADD", event.Type)
		assert.NotEmpty(t, event.PodName)
		assert.NotEmpty(t, event.Namespace)
		assert.Contains(t, event.Metadata, "plugin")
	case <-ctx.Done():
		// Timeout is expected for this test
	}
}

func TestCNICollector_DiscoverPlugins(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "cni-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create CNI config directory
	configDir := filepath.Join(tempDir, "net.d")
	err = os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Create CNI bin directory
	binDir := filepath.Join(tempDir, "bin")
	err = os.MkdirAll(binDir, 0755)
	require.NoError(t, err)

	// Create Calico config
	calicoConfig := NetworkConfig{
		CNIVersion: "0.4.0",
		Name:       "k8s-pod-network",
		Type:       "calico",
	}
	calicoData, _ := json.Marshal(calicoConfig)
	err = ioutil.WriteFile(filepath.Join(configDir, "10-calico.conf"), calicoData, 0644)
	require.NoError(t, err)

	// Create Flannel config
	flannelConfig := NetworkConfig{
		CNIVersion: "0.4.0",
		Name:       "cbr0",
		Type:       "flannel",
	}
	flannelData, _ := json.Marshal(flannelConfig)
	err = ioutil.WriteFile(filepath.Join(configDir, "10-flannel.json"), flannelData, 0644)
	require.NoError(t, err)

	// Create mock binaries
	err = ioutil.WriteFile(filepath.Join(binDir, "calico"), []byte("#!/bin/bash\necho calico"), 0755)
	require.NoError(t, err)
	err = ioutil.WriteFile(filepath.Join(binDir, "flannel"), []byte("#!/bin/bash\necho flannel"), 0755)
	require.NoError(t, err)

	config := &CNICollectorConfig{
		CollectionInterval:    5 * time.Second,
		CNIConfigPath:        configDir,
		CNIBinPath:           binDir,
		SupportedCNIPlugins:  []string{"calico", "flannel", "cilium", "weave"},
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	err = collector.discoverCNIPlugins()
	assert.NoError(t, err)

	// Verify plugins were discovered
	assert.Contains(t, collector.cniPlugins, "calico")
	assert.Contains(t, collector.cniPlugins, "flannel")

	// Test plugin interfaces
	calicoPlugin := collector.cniPlugins["calico"]
	assert.Equal(t, "calico", calicoPlugin.Name())
	
	flannelPlugin := collector.cniPlugins["flannel"]
	assert.Equal(t, "flannel", flannelPlugin.Name())
}

func TestCNICollector_ProcessCNIEvent(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	eventCh := make(chan interface{}, 10)
	collector.eventChan = eventCh

	plugin := &CalicoPlugin{
		name:    "calico",
		version: "v3.20.0",
	}

	cniEvent := &CNIEvent{
		Type:      "ADD",
		Timestamp: time.Now(),
		PodName:   "test-pod",
		Namespace: "default",
		PodIP:     "10.244.1.10",
		Interface: "cali123456789",
		Duration:  50 * time.Millisecond,
		Result: &CNIResult{
			CNIVersion: "0.4.0",
			Interfaces: []Interface{
				{Name: "eth0", Mac: "02:42:ac:11:00:02"},
			},
			IPs: []IPConfig{
				{Version: "4", Address: "10.244.1.10/24"},
			},
		},
		Metadata: map[string]interface{}{
			"plugin": "calico",
			"node":   "worker-1",
		},
	}

	collector.processCNIEvent(plugin, cniEvent)

	// Verify event was generated
	select {
	case event := <-eventCh:
		opinionatedEvent, ok := event.(*opinionated.OpinionatedEvent)
		require.True(t, ok)
		
		assert.Contains(t, opinionatedEvent.ID, "cni-calico-ADD-")
		assert.Equal(t, opinionated.CategoryNetworkHealth, opinionatedEvent.Category)
		assert.Equal(t, "default", opinionatedEvent.Context.Namespace)
		assert.Equal(t, "test-pod", opinionatedEvent.Context.Pod)
		assert.Equal(t, opinionated.SeverityInfo, opinionatedEvent.Severity)
		
		// Check CNI-specific attributes
		assert.Equal(t, "calico", opinionatedEvent.Attributes["cni.plugin"])
		assert.Equal(t, "v3.20.0", opinionatedEvent.Attributes["cni.plugin_version"])
		assert.Equal(t, "ADD", opinionatedEvent.Attributes["cni.operation"])
		assert.Equal(t, "10.244.1.10", opinionatedEvent.Attributes["cni.pod_ip"])
		assert.Equal(t, "cali123456789", opinionatedEvent.Attributes["cni.interface"])
		assert.Equal(t, int64(50), opinionatedEvent.Attributes["cni.duration_ms"])
		
		// Check CNI result information
		assert.Equal(t, "0.4.0", opinionatedEvent.Attributes["cni.result.cni_version"])
		assert.Equal(t, 1, opinionatedEvent.Attributes["cni.result.interface_count"])
		assert.Equal(t, 1, opinionatedEvent.Attributes["cni.result.ip_count"])
		
		// Check metadata
		assert.Equal(t, "calico", opinionatedEvent.Attributes["cni.plugin"])
		assert.Equal(t, "worker-1", opinionatedEvent.Attributes["cni.node"])
		
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected CNI event but none was generated")
	}

	// Verify metrics were updated
	collector.metrics.mutex.RLock()
	assert.Greater(t, collector.metrics.EventsCollected, uint64(0))
	collector.metrics.mutex.RUnlock()
}

func TestCNICollector_AnomalyDetection(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	eventCh := make(chan interface{}, 10)
	collector.eventChan = eventCh

	plugin := &CalicoPlugin{name: "calico", version: "v3.20.0"}

	tests := []struct {
		name          string
		cniEvent      *CNIEvent
		expectedTags  []string
		expectedSeverity opinionated.EventSeverity
	}{
		{
			name: "slow CNI operation",
			cniEvent: &CNIEvent{
				Type:      "ADD",
				Timestamp: time.Now(),
				PodName:   "test-pod",
				Namespace: "default",
				Duration:  6 * time.Second,
			},
			expectedTags:     []string{"SLOW_CNI_OPERATION"},
			expectedSeverity: opinionated.SeverityHigh,
		},
		{
			name: "CNI failure",
			cniEvent: &CNIEvent{
				Type:      "ADD",
				Timestamp: time.Now(),
				PodName:   "test-pod",
				Namespace: "default",
				Error:     "timeout waiting for network",
				Duration:  2 * time.Second,
			},
			expectedTags:     []string{"CNI_FAILURE"},
			expectedSeverity: opinionated.SeverityHigh,
		},
		{
			name: "no IP allocated",
			cniEvent: &CNIEvent{
				Type:      "ADD",
				Timestamp: time.Now(),
				PodName:   "test-pod",
				Namespace: "default",
				PodIP:     "", // No IP allocated
				Duration:  100 * time.Millisecond,
			},
			expectedTags:     []string{"NO_IP_ALLOCATED"},
			expectedSeverity: opinionated.SeverityHigh,
		},
		{
			name: "unusual interface name",
			cniEvent: &CNIEvent{
				Type:      "ADD",
				Timestamp: time.Now(),
				PodName:   "test-pod",
				Namespace: "default",
				Interface: "suspicious-interface-9999",
				Duration:  100 * time.Millisecond,
			},
			expectedTags:     []string{"UNUSUAL_INTERFACE_NAME"},
			expectedSeverity: opinionated.SeverityMedium,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.processCNIEvent(plugin, tt.cniEvent)

			// Verify event contains expected anomaly tags
			select {
			case event := <-eventCh:
				opinionatedEvent, ok := event.(*opinionated.OpinionatedEvent)
				require.True(t, ok)
				
				// Check severity
				assert.Equal(t, tt.expectedSeverity, opinionatedEvent.Severity)
				
				// Check tags in data message
				if msg, exists := opinionatedEvent.Data["message"]; exists {
					for _, tag := range tt.expectedTags {
						assert.Contains(t, msg, fmt.Sprintf("[%s]", tag))
					}
				}
				
				// Check anomaly attributes
				for _, tag := range tt.expectedTags {
					anomalyKey := fmt.Sprintf("anomaly.%s", strings.ToLower(tag))
					assert.Equal(t, true, opinionatedEvent.Attributes[anomalyKey])
				}
				
			case <-time.After(100 * time.Millisecond):
				t.Fatalf("Expected CNI anomaly event for %s but none was generated", tt.name)
			}
		})
	}
}

func TestCNICollector_InterfaceNameValidation(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name      string
		ifaceName string
		expected  bool
	}{
		{
			name:      "calico interface",
			ifaceName: "cali123456789",
			expected:  true,
		},
		{
			name:      "flannel interface",
			ifaceName: "flannel.1",
			expected:  true,
		},
		{
			name:      "cilium interface",
			ifaceName: "cilium_host",
			expected:  true,
		},
		{
			name:      "weave interface",
			ifaceName: "weave",
			expected:  true,
		},
		{
			name:      "standard eth interface",
			ifaceName: "eth0",
			expected:  true,
		},
		{
			name:      "veth interface",
			ifaceName: "veth123abc",
			expected:  true,
		},
		{
			name:      "docker interface",
			ifaceName: "docker0",
			expected:  true,
		},
		{
			name:      "suspicious interface",
			ifaceName: "malicious-interface",
			expected:  false,
		},
		{
			name:      "unusual pattern",
			ifaceName: "xyz999",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isValidInterfaceName(tt.ifaceName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetPluginVersion(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "cni-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create mock binary
	binaryPath := filepath.Join(tempDir, "calico")
	err = ioutil.WriteFile(binaryPath, []byte("#!/bin/bash\necho calico"), 0755)
	require.NoError(t, err)

	version := getPluginVersion(binaryPath, "calico")
	assert.Equal(t, "v3.20.0", version) // Mock version

	// Test non-existent binary
	version = getPluginVersion("/non/existent/path", "calico")
	assert.Equal(t, "unknown", version)
}

func TestLoadNetworkConfig(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "cni-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "test.conf")
	config := NetworkConfig{
		CNIVersion: "0.4.0",
		Name:       "test-network",
		Type:       "test-plugin",
	}

	configData, err := json.Marshal(config)
	require.NoError(t, err)

	err = ioutil.WriteFile(configPath, configData, 0644)
	require.NoError(t, err)

	loadedConfig, err := loadNetworkConfig(configPath, "test-plugin")
	assert.NoError(t, err)
	assert.Equal(t, "0.4.0", loadedConfig.CNIVersion)
	assert.Equal(t, "test-network", loadedConfig.Name)
	assert.Equal(t, "test-plugin", loadedConfig.Type)

	// Test non-existent file
	_, err = loadNetworkConfig("/non/existent/path", "test")
	assert.Error(t, err)
}

// Benchmark tests for CNI plugin performance
func BenchmarkCNICollector_ProcessCNIEvent(b *testing.B) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(b, err)

	eventCh := make(chan interface{}, 1000)
	collector.eventChan = eventCh

	plugin := &CalicoPlugin{
		name:    "calico",
		version: "v3.20.0",
	}

	cniEvent := &CNIEvent{
		Type:      "ADD",
		Timestamp: time.Now(),
		PodName:   "benchmark-pod",
		Namespace: "default",
		PodIP:     "10.244.1.10",
		Interface: "cali123456789",
		Duration:  50 * time.Millisecond,
		Metadata: map[string]interface{}{
			"plugin": "calico",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cniEvent.PodName = fmt.Sprintf("benchmark-pod-%d", i)
		collector.processCNIEvent(plugin, cniEvent)
	}
}

func BenchmarkCNICollector_InterfaceNameValidation(b *testing.B) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(b, err)

	interfaceNames := []string{
		"cali123456789",
		"flannel.1",
		"cilium_host",
		"weave",
		"eth0",
		"veth123abc",
		"suspicious-interface",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ifaceName := interfaceNames[i%len(interfaceNames)]
		collector.isValidInterfaceName(ifaceName)
	}
}