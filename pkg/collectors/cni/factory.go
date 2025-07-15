package cni

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/unified"
)

// Factory creates CNI collectors
type Factory struct{}

// NewFactory creates a new CNI collector factory
func NewFactory() unified.Factory {
	return &Factory{}
}

// Create creates a new CNI collector instance
func (f *Factory) Create(config unified.CollectorConfig) (unified.Collector, error) {
	if config.Type != unified.CollectorTypeCNI {
		return nil, fmt.Errorf("invalid collector type: %s (expected: %s)", config.Type, unified.CollectorTypeCNI)
	}

	// Validate CNI-specific configuration
	if err := f.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid CNI configuration: %w", err)
	}

	// Create the CNI collector
	collector, err := NewCNICollector(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create CNI collector: %w", err)
	}

	return collector, nil
}

// SupportedTypes returns the collector types this factory creates
func (f *Factory) SupportedTypes() []string {
	return []string{unified.CollectorTypeCNI}
}

// ValidateConfig validates CNI collector configuration
func (f *Factory) ValidateConfig(config unified.CollectorConfig) error {
	if config.Type != unified.CollectorTypeCNI {
		return fmt.Errorf("invalid collector type: %s", config.Type)
	}

	if config.Name == "" {
		return fmt.Errorf("collector name is required")
	}

	if config.EventBufferSize <= 0 {
		return fmt.Errorf("event buffer size must be positive")
	}

	if config.MaxEventsPerSec <= 0 {
		return fmt.Errorf("max events per second must be positive")
	}

	// Validate CNI-specific configuration in Extra
	if config.Extra != nil {
		if collectionInterval, exists := config.Extra["collection_interval"]; exists {
			if interval, ok := collectionInterval.(string); ok {
				if _, err := time.ParseDuration(interval); err != nil {
					return fmt.Errorf("invalid collection_interval: %w", err)
				}
			}
		}

		if flowCacheSize, exists := config.Extra["flow_cache_size"]; exists {
			if size, ok := flowCacheSize.(float64); ok {
				if size <= 0 {
					return fmt.Errorf("flow_cache_size must be positive")
				}
			}
		}

		if maxFlows, exists := config.Extra["max_concurrent_flows"]; exists {
			if flows, ok := maxFlows.(float64); ok {
				if flows <= 0 {
					return fmt.Errorf("max_concurrent_flows must be positive")
				}
			}
		}
	}

	return nil
}

// DefaultConfig returns the default configuration for CNI collectors
func (f *Factory) DefaultConfig(collectorType string) unified.CollectorConfig {
	if collectorType != unified.CollectorTypeCNI {
		return unified.CollectorConfig{}
	}

	return unified.CollectorConfig{
		Name:            "cni",
		Type:            unified.CollectorTypeCNI,
		Enabled:         true,
		SamplingRate:    1.0,
		EventBufferSize: 10000,
		MaxEventsPerSec: 5000,
		MinSeverity:     unified.SeverityInfo,
		MaxMemoryMB:     256,
		MaxCPUMilli:     200,
		Labels: map[string]string{
			"collector": "cni",
		},
		Tags: map[string]string{
			"source": "network",
		},
		Extra: map[string]interface{}{
			// CNI-specific defaults
			"collection_interval":      "1s",
			"cni_config_path":          "/etc/cni/net.d",
			"cni_bin_path":             "/opt/cni/bin",
			"supported_cni_plugins":    []string{"calico", "flannel", "cilium", "weave"},
			"enable_network_flows":     true,
			"enable_dns_monitoring":    true,
			"enable_policy_monitoring": true,
			"flow_cache_size":          10000,
			"dns_cache_size":           1000,
			"max_concurrent_flows":     50000,
		},
	}
}

// CNIConfigValidator validates CNI-specific configuration
type CNIConfigValidator struct{}

// Validate validates the CNI configuration
func (v *CNIConfigValidator) Validate(config unified.CollectorConfig) error {
	factory := &Factory{}
	return factory.ValidateConfig(config)
}

// init registers the CNI factory globally
func init() {
	factory := NewFactory()
	if err := unified.RegisterCollectorFactory(unified.CollectorTypeCNI, factory); err != nil {
		// Log error but don't panic during init
		// In a real implementation, you might want to use a logger here
	}
}
