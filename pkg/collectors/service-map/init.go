package servicemap

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the service-map collector with the orchestrator
func init() {
	orchestrator.RegisterCollectorFactory("service-map", NewServiceMapCollector)
}

// NewServiceMapCollector creates a new service map collector for the orchestrator
func NewServiceMapCollector(yamlConfig map[string]interface{}, logger *zap.Logger) (collectors.Collector, error) {
	// Parse YAML config into our Config struct
	config := DefaultConfig()
	
	// Handle enabled flag
	if enabled, ok := yamlConfig["enabled"].(bool); ok {
		config.Enabled = enabled
	}
	
	// Handle buffer size
	if bufferSize, ok := yamlConfig["buffer_size"].(int); ok {
		config.BufferSize = bufferSize
	}
	
	// Handle namespaces
	if namespaces, ok := yamlConfig["namespaces"].([]interface{}); ok {
		config.Namespaces = make([]string, 0, len(namespaces))
		for _, ns := range namespaces {
			if nsStr, ok := ns.(string); ok {
				config.Namespaces = append(config.Namespaces, nsStr)
			}
		}
	}
	
	// Handle exclude namespaces
	if excludeNs, ok := yamlConfig["exclude_namespaces"].([]interface{}); ok {
		config.ExcludeNamespaces = make([]string, 0, len(excludeNs))
		for _, ns := range excludeNs {
			if nsStr, ok := ns.(string); ok {
				config.ExcludeNamespaces = append(config.ExcludeNamespaces, nsStr)
			}
		}
	}
	
	// Handle K8s discovery
	if enableK8s, ok := yamlConfig["enable_k8s_discovery"].(bool); ok {
		config.EnableK8sDiscovery = enableK8s
	}
	
	// Handle eBPF
	if enableEBPF, ok := yamlConfig["enable_ebpf"].(bool); ok {
		config.EnableEBPF = enableEBPF
	}
	
	// Handle auto-detect type
	if autoDetect, ok := yamlConfig["auto_detect_type"].(bool); ok {
		config.AutoDetectType = autoDetect
	}
	
	// Handle visualization
	if enableViz, ok := yamlConfig["enable_visualization"].(bool); ok {
		config.EnableVisualization = enableViz
	}
	
	// Handle filtering options
	if includeExternal, ok := yamlConfig["include_external_services"].(bool); ok {
		config.IncludeExternalServices = includeExternal
	}
	
	if minConnections, ok := yamlConfig["min_connection_count"].(int); ok {
		config.MinConnectionCount = minConnections
	}
	
	if ignoreSystem, ok := yamlConfig["ignore_system_namespaces"].(bool); ok {
		config.IgnoreSystemNamespaces = ignoreSystem
	}
	
	// Create collector
	collector, err := NewCollector("service-map", config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create service-map collector: %w", err)
	}
	
	return collector, nil
}