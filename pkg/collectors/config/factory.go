package config

import (
	"context"
	"fmt"
	"time"
)

// Collector interface is left as interface{} to avoid circular imports
// The actual collector types will be converted in the registry
type Collector interface{}

// CollectorFactory defines the interface for creating collectors from typed configurations
type CollectorFactory interface {
	// GetName returns the factory name/type
	GetName() string

	// GetCollectorType returns the collector type this factory creates
	GetCollectorType() string

	// CreateCollector creates a new collector instance from typed configuration
	CreateCollector(ctx context.Context, config CollectorConfig) (Collector, error)

	// ValidateConfig validates that the provided config is compatible with this factory
	ValidateConfig(config CollectorConfig) error
}

// BaseCollectorFactory provides common functionality for collector factories
type BaseCollectorFactory struct {
	name          string
	collectorType string
}

// NewBaseCollectorFactory creates a new base factory
func NewBaseCollectorFactory(name, collectorType string) *BaseCollectorFactory {
	return &BaseCollectorFactory{
		name:          name,
		collectorType: collectorType,
	}
}

// GetName returns the factory name
func (f *BaseCollectorFactory) GetName() string {
	return f.name
}

// GetCollectorType returns the collector type
func (f *BaseCollectorFactory) GetCollectorType() string {
	return f.collectorType
}

// ValidateConfig validates that config is not nil and passes its own validation
func (f *BaseCollectorFactory) ValidateConfig(config CollectorConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	if err := config.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	return nil
}

// ConfigParser provides utilities for parsing configurations from various sources
type ConfigParser struct{}

// NewConfigParser creates a new config parser
func NewConfigParser() *ConfigParser {
	return &ConfigParser{}
}

// ParseFromMap parses a map[string]interface{} into a typed configuration
// This provides backward compatibility for existing YAML/JSON configurations
func (p *ConfigParser) ParseFromMap(collectorType string, configMap map[string]interface{}) (CollectorConfig, error) {
	if configMap == nil {
		configMap = make(map[string]interface{})
	}

	// Extract common fields
	name, _ := configMap["name"].(string)
	if name == "" {
		name = collectorType // Use collector type as default name
	}

	bufferSize := 10000 // default
	if bs, ok := configMap["buffer_size"].(int); ok {
		bufferSize = bs
	}

	metricsEnabled := true // default
	if me, ok := configMap["metrics_enabled"].(bool); ok {
		metricsEnabled = me
	}

	labels := make(map[string]string)
	if l, ok := configMap["labels"].(map[string]interface{}); ok {
		for k, v := range l {
			if str, ok := v.(string); ok {
				labels[k] = str
			}
		}
	}

	// Create collector-specific configurations
	switch collectorType {
	case "cni":
		config := NewCNIConfig(name)
		if enableEBPF, ok := configMap["enable_ebpf"].(bool); ok {
			config.EnableEBPF = enableEBPF
		}
		if podCIDR, ok := configMap["pod_cidr"].(string); ok {
			config.PodCIDR = podCIDR
		}
		if interfacePrefix, ok := configMap["interface_prefix"].(string); ok {
			config.InterfacePrefix = interfacePrefix
		}
		if enableNetworkPolicies, ok := configMap["enable_network_policies"].(bool); ok {
			config.EnableNetworkPolicies = enableNetworkPolicies
		}
		if trackBandwidth, ok := configMap["track_bandwidth"].(bool); ok {
			config.TrackBandwidth = trackBandwidth
		}
		return config, nil

	case "cri":
		config := NewCRIConfig(name)
		if runtimeEndpoint, ok := configMap["runtime_endpoint"].(string); ok {
			config.RuntimeEndpoint = runtimeEndpoint
		}
		if runtimeTimeout, ok := configMap["runtime_timeout"].(string); ok {
			// Parse duration string
			if duration, err := time.ParseDuration(runtimeTimeout); err == nil {
				config.RuntimeTimeout = duration
			}
		}
		if enableMemoryTracking, ok := configMap["enable_memory_tracking"].(bool); ok {
			config.EnableMemoryTracking = enableMemoryTracking
		}
		if enableCPUTracking, ok := configMap["enable_cpu_tracking"].(bool); ok {
			config.EnableCPUTracking = enableCPUTracking
		}
		if containerStatsInterval, ok := configMap["container_stats_interval"].(string); ok {
			// Parse duration string
			if duration, err := time.ParseDuration(containerStatsInterval); err == nil {
				config.ContainerStatsInterval = duration
			}
		}
		return config, nil

	case "dns":
		config := NewDNSConfig(name)
		if enableEBPF, ok := configMap["enable_ebpf"].(bool); ok {
			config.EnableEBPF = enableEBPF
		}
		if iface, ok := configMap["interface"].(string); ok {
			config.Interface = iface
		}
		if enableSocket, ok := configMap["enable_socket"].(bool); ok {
			config.EnableSocket = enableSocket
		}
		return config, nil

	case "etcd":
		config := NewETCDConfig(name)
		if enableEBPF, ok := configMap["enable_ebpf"].(bool); ok {
			config.EnableEBPF = enableEBPF
		}
		if endpoints, ok := configMap["endpoints"].([]interface{}); ok {
			config.Endpoints = []string{}
			for _, ep := range endpoints {
				if epStr, ok := ep.(string); ok {
					config.Endpoints = append(config.Endpoints, epStr)
				}
			}
		}
		if username, ok := configMap["username"].(string); ok {
			config.Username = username
		}
		if password, ok := configMap["password"].(string); ok {
			config.Password = password
		}
		// TLS config parsing
		if tlsMap, ok := configMap["tls"].(map[string]interface{}); ok {
			tlsConfig := &ETCDTLSConfig{}
			if certFile, ok := tlsMap["cert_file"].(string); ok {
				tlsConfig.CertFile = certFile
			}
			if keyFile, ok := tlsMap["key_file"].(string); ok {
				tlsConfig.KeyFile = keyFile
			}
			if caFile, ok := tlsMap["ca_file"].(string); ok {
				tlsConfig.CAFile = caFile
			}
			config.TLS = tlsConfig
		}
		return config, nil

	case "kernel":
		config := NewKernelConfig(name)
		if enableMemoryTracking, ok := configMap["enable_memory_tracking"].(bool); ok {
			config.EnableMemoryTracking = enableMemoryTracking
		}
		if enableProcessTracking, ok := configMap["enable_process_tracking"].(bool); ok {
			config.EnableProcessTracking = enableProcessTracking
		}
		if enableNetworkTracking, ok := configMap["enable_network_tracking"].(bool); ok {
			config.EnableNetworkTracking = enableNetworkTracking
		}
		if enableFileTracking, ok := configMap["enable_file_tracking"].(bool); ok {
			config.EnableFileTracking = enableFileTracking
		}
		if perfBufferSize, ok := configMap["perf_buffer_size"].(int); ok {
			config.PerfBufferSize = perfBufferSize
		}
		return config, nil

	case "kubeapi":
		// KubeAPI collector doesn't have specific config in base.go yet
		// Return a basic configuration for now
		config := &BaseConfig{
			Name:           name,
			BufferSize:     bufferSize,
			MetricsEnabled: metricsEnabled,
			Labels:         labels,
		}
		return config, nil

	case "kubelet":
		// Kubelet collector doesn't have specific config in base.go yet
		// Return a basic configuration for now
		config := &BaseConfig{
			Name:           name,
			BufferSize:     bufferSize,
			MetricsEnabled: metricsEnabled,
			Labels:         labels,
		}
		return config, nil

	case "systemd":
		// Systemd collector doesn't have specific config in base.go yet
		// Return a basic configuration for now
		config := &BaseConfig{
			Name:           name,
			BufferSize:     bufferSize,
			MetricsEnabled: metricsEnabled,
			Labels:         labels,
		}
		return config, nil

	default:
		return nil, fmt.Errorf("unknown collector type: %s", collectorType)
	}
}
