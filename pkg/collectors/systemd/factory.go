package systemd

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/unified"
)

// Factory creates SystemD collectors
type Factory struct{}

// NewFactory creates a new SystemD collector factory
func NewFactory() unified.Factory {
	return &Factory{}
}

// Create creates a new SystemD collector instance
func (f *Factory) Create(config unified.CollectorConfig) (unified.Collector, error) {
	if config.Type != unified.CollectorTypeSystemd {
		return nil, fmt.Errorf("invalid collector type: %s (expected: %s)", config.Type, unified.CollectorTypeSystemd)
	}

	// Validate SystemD-specific configuration
	if err := f.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid SystemD configuration: %w", err)
	}

	// Create the SystemD collector
	collector, err := NewCollector(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create SystemD collector: %w", err)
	}

	return collector, nil
}

// SupportedTypes returns the collector types this factory creates
func (f *Factory) SupportedTypes() []string {
	return []string{unified.CollectorTypeSystemd}
}

// ValidateConfig validates SystemD collector configuration
func (f *Factory) ValidateConfig(config unified.CollectorConfig) error {
	if config.Type != unified.CollectorTypeSystemd {
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

	// Validate SystemD-specific configuration in Extra
	if config.Extra != nil {
		if pollInterval, exists := config.Extra["poll_interval"]; exists {
			if interval, ok := pollInterval.(string); ok {
				if _, err := time.ParseDuration(interval); err != nil {
					return fmt.Errorf("invalid poll_interval: %w", err)
				}
			}
		}

		if signalBufferSize, exists := config.Extra["signal_buffer_size"]; exists {
			if size, ok := signalBufferSize.(float64); ok {
				if size <= 0 {
					return fmt.Errorf("signal_buffer_size must be positive")
				}
			}
		}
	}

	return nil
}

// DefaultConfig returns the default configuration for SystemD collectors
func (f *Factory) DefaultConfig(collectorType string) unified.CollectorConfig {
	if collectorType != unified.CollectorTypeSystemd {
		return unified.CollectorConfig{}
	}

	return unified.CollectorConfig{
		Name:            "systemd",
		Type:            unified.CollectorTypeSystemd,
		Enabled:         true,
		SamplingRate:    1.0,
		EventBufferSize: 1000,
		MaxEventsPerSec: 500,
		MinSeverity:     unified.SeverityInfo,
		MaxMemoryMB:     64,
		MaxCPUMilli:     50,
		Labels: map[string]string{
			"collector": "systemd",
		},
		Tags: map[string]string{
			"source": "system",
		},
		Extra: map[string]interface{}{
			// SystemD-specific defaults
			"monitor_services":     true,
			"monitor_sockets":      false,
			"monitor_timers":       false,
			"exclude_system":       true,
			"poll_interval":        "30s",
			"signal_buffer_size":   1000,
			"service_whitelist":    []string{},
			"service_blacklist":    []string{},
			"monitor_all_services": false,
			"track_dependencies":   true,
		},
	}
}

// SystemDConfigValidator validates SystemD-specific configuration
type SystemDConfigValidator struct{}

// Validate validates the SystemD configuration
func (v *SystemDConfigValidator) Validate(config unified.CollectorConfig) error {
	factory := &Factory{}
	return factory.ValidateConfig(config)
}

// init registers the SystemD factory globally
func init() {
	factory := NewFactory()
	if err := unified.RegisterCollectorFactory(unified.CollectorTypeSystemd, factory); err != nil {
		// Log error but don't panic during init
		// In a real implementation, you might want to use a logger here
	}
}
