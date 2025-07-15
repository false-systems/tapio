package k8s

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/unified"
)

// Factory creates K8s collectors
type Factory struct{}

// NewFactory creates a new K8s collector factory
func NewFactory() unified.Factory {
	return &Factory{}
}

// Create creates a new K8s collector instance
func (f *Factory) Create(config unified.CollectorConfig) (unified.Collector, error) {
	if config.Type != unified.CollectorTypeK8s {
		return nil, fmt.Errorf("invalid collector type: %s (expected: %s)", config.Type, unified.CollectorTypeK8s)
	}

	// Validate K8s-specific configuration
	if err := f.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid K8s configuration: %w", err)
	}

	// Create the K8s collector
	collector, err := NewCollector(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s collector: %w", err)
	}

	return collector, nil
}

// SupportedTypes returns the collector types this factory creates
func (f *Factory) SupportedTypes() []string {
	return []string{unified.CollectorTypeK8s}
}

// ValidateConfig validates K8s collector configuration
func (f *Factory) ValidateConfig(config unified.CollectorConfig) error {
	if config.Type != unified.CollectorTypeK8s {
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

	// Validate K8s-specific configuration in Extra
	if config.Extra != nil {
		if watchTimeout, exists := config.Extra["watch_timeout"]; exists {
			if timeout, ok := watchTimeout.(string); ok {
				if _, err := time.ParseDuration(timeout); err != nil {
					return fmt.Errorf("invalid watch_timeout: %w", err)
				}
			}
		}

		if retryBackoff, exists := config.Extra["retry_backoff"]; exists {
			if backoff, ok := retryBackoff.(string); ok {
				if _, err := time.ParseDuration(backoff); err != nil {
					return fmt.Errorf("invalid retry_backoff: %w", err)
				}
			}
		}

		if maxRetries, exists := config.Extra["max_retries"]; exists {
			if retries, ok := maxRetries.(float64); ok {
				if retries < 0 {
					return fmt.Errorf("max_retries must be non-negative")
				}
			}
		}
	}

	return nil
}

// DefaultConfig returns the default configuration for K8s collectors
func (f *Factory) DefaultConfig(collectorType string) unified.CollectorConfig {
	if collectorType != unified.CollectorTypeK8s {
		return unified.CollectorConfig{}
	}

	return unified.CollectorConfig{
		Name:            "k8s",
		Type:            unified.CollectorTypeK8s,
		Enabled:         true,
		SamplingRate:    1.0,
		EventBufferSize: 5000,
		MaxEventsPerSec: 2000,
		MinSeverity:     unified.SeverityInfo,
		MaxMemoryMB:     128,
		MaxCPUMilli:     100,
		Labels: map[string]string{
			"collector": "k8s",
		},
		Tags: map[string]string{
			"source": "kubernetes",
		},
		Extra: map[string]interface{}{
			// K8s-specific defaults
			"in_cluster":    true,
			"kube_config":   "",
			"namespace":     "",
			"watch_timeout": "30s",
			"retry_backoff": "5s",
			"max_retries":   3,
		},
	}
}

// K8sConfigValidator validates K8s-specific configuration
type K8sConfigValidator struct{}

// Validate validates the K8s configuration
func (v *K8sConfigValidator) Validate(config unified.CollectorConfig) error {
	factory := &Factory{}
	return factory.ValidateConfig(config)
}

// init registers the K8s factory globally
func init() {
	factory := NewFactory()
	if err := unified.RegisterCollectorFactory(unified.CollectorTypeK8s, factory); err != nil {
		// Log error but don't panic during init
		// In a real implementation, you might want to use a logger here
	}
}
