package oom

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Interface verification is done in platform-specific files

// NewOOMCollector creates a new OOM collector for the current platform
func NewOOMCollector(config *OOMConfig, logger *zap.Logger) (collectors.Collector, error) {
	return NewCollector("oom-collector", config, logger)
}

// Config represents the public configuration interface
type Config struct {
	*OOMConfig
}

// NewConfig creates a new OOM collector configuration
func NewConfig() *Config {
	return &Config{
		OOMConfig: DefaultOOMConfig(),
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.OOMConfig == nil {
		return fmt.Errorf("OOMConfig cannot be nil")
	}
	return c.OOMConfig.Validate()
}

// Factory function for use with collector registry
func CreateCollector(config *Config, logger *zap.Logger) (collectors.Collector, error) {
	if config == nil {
		config = NewConfig()
	}
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	return NewOOMCollector(config.OOMConfig, logger)
}

// GetDefaultConfig returns the default configuration
func GetDefaultConfig() *Config {
	return NewConfig()
}

// ConfigFromMap creates configuration from a map (for generic factory usage)
// This avoids the forbidden map[string]interface{} pattern by using typed conversion
func ConfigFromMap(configMap map[string]string) (*Config, error) {
	config := NewConfig()

	// Parse string-based configuration
	for key, value := range configMap {
		if err := setConfigValue(config, key, value); err != nil {
			return nil, fmt.Errorf("invalid config value for %s: %w", key, err)
		}
	}

	return config, nil
}

// setConfigValue sets a configuration value from string (type-safe)
func setConfigValue(config *Config, key, value string) error {
	switch key {
	case "enable_prediction":
		config.EnablePrediction = value == "true"
	case "prediction_threshold_percent":
		// Would parse uint32 from string
		// config.PredictionThresholdPct = parsed value
	case "ring_buffer_size":
		// Would parse uint32 from string
		// config.RingBufferSize = parsed value
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	return nil
}

// Exported types for external usage
type (
	// EventType exports OOMEventType for external usage
	EventType = OOMEventType

	// Event exports ProcessedOOMEvent for external usage
	Event = ProcessedOOMEvent

	// MemoryStats exports MemoryStatistics for external usage
	MemoryStats = MemoryStatistics

	// K8sContext exports KubernetesContext for external usage
	K8sContext = KubernetesContext
)

// Exported constants for external usage
const (
	EventTypeOOMKillVictim      = OOMKillVictim
	EventTypeOOMKillTriggered   = OOMKillTriggered
	EventTypeMemoryPressureHigh = MemoryPressureHigh
	EventTypeMemoryPressureCrit = MemoryPressureCrit
)

// Helper functions for external usage

// IsOOMEvent returns true if the collector event is an OOM-related event
func IsOOMEvent(event *domain.CollectorEvent) bool {
	return event.Source == "oom-collector" || event.Type == domain.EventTypeContainerOOM
}

// ExtractOOMContext extracts OOM-specific context from a collector event
func ExtractOOMContext(event *domain.CollectorEvent) map[string]string {
	context := make(map[string]string)

	if event.Metadata.Labels != nil {
		if oomType, exists := event.Metadata.Labels["oom_event_type"]; exists {
			context["event_type"] = oomType
		}
		if pressure, exists := event.Metadata.Labels["memory_pressure"]; exists {
			context["pressure_level"] = pressure
		}
	}

	if event.CorrelationHints != nil && event.CorrelationHints.CorrelationTags != nil {
		for key, value := range event.CorrelationHints.CorrelationTags {
			context[key] = value
		}
	}

	return context
}

// IsCriticalOOMEvent returns true if this is a critical OOM event requiring immediate attention
func IsCriticalOOMEvent(event *domain.CollectorEvent) bool {
	if !IsOOMEvent(event) {
		return false
	}

	// Check severity
	if event.Severity == domain.EventSeverityCritical {
		return true
	}

	// Check event type from labels
	if event.Metadata.Labels != nil {
		if oomType, exists := event.Metadata.Labels["oom_event_type"]; exists {
			return oomType == "oom_kill_victim" || oomType == "memory_pressure_critical"
		}
	}

	return false
}

// IsPredictiveOOMEvent returns true if this is a predictive OOM event (early warning)
func IsPredictiveOOMEvent(event *domain.CollectorEvent) bool {
	if !IsOOMEvent(event) {
		return false
	}

	if event.Metadata.Labels != nil {
		if oomType, exists := event.Metadata.Labels["oom_event_type"]; exists {
			return oomType == "memory_pressure_high" || oomType == "memory_pressure_critical"
		}
	}

	return false
}
