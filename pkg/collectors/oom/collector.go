//go:build linux

package oom

import (
	"fmt"
	"strconv"

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
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	if config == nil {
		config = NewConfig()
	}

	return NewOOMCollector(config.OOMConfig, logger)
}

// GetDefaultConfig returns the default configuration
func GetDefaultConfig() *Config {
	return NewConfig()
}

// ConfigFromMap creates configuration from a map (for generic factory usage)
// This avoids the forbidden map[string]interface{} pattern by using typed conversion
// Expected keys: enable_prediction, prediction_threshold_percent, ring_buffer_size,
// high_pressure_threshold_percent, event_batch_size, max_events_per_second,
// collect_cmdline, collect_environment, collect_memory_details, exclude_system_processes
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
		v, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return fmt.Errorf("parsing prediction_threshold_percent: %w", err)
		}
		config.PredictionThresholdPct = uint32(v)
	case "ring_buffer_size":
		v, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return fmt.Errorf("parsing ring_buffer_size: %w", err)
		}
		config.RingBufferSize = uint32(v)
	case "high_pressure_threshold_percent":
		v, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return fmt.Errorf("parsing high_pressure_threshold_percent: %w", err)
		}
		config.HighPressureThresholdPct = uint32(v)
	case "event_batch_size":
		v, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return fmt.Errorf("parsing event_batch_size: %w", err)
		}
		config.EventBatchSize = uint32(v)
	case "max_events_per_second":
		v, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return fmt.Errorf("parsing max_events_per_second: %w", err)
		}
		config.MaxEventsPerSecond = uint32(v)
	case "collect_cmdline":
		config.CollectCmdline = value == "true"
	case "collect_environment":
		config.CollectEnvironment = value == "true"
	case "collect_memory_details":
		config.CollectMemoryDetails = value == "true"
	case "exclude_system_processes":
		config.ExcludeSystemProcesses = value == "true"
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
