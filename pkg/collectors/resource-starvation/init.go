package resourcestarvation

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the resource starvation collector factory with the collector registry
// Disabled until orchestrator API is available
/*
func init() {
	RegisterResourceStarvationCollector()
}
*/

// RegisterResourceStarvationCollector registers the resource starvation collector factory with the orchestrator
func RegisterResourceStarvationCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert YAML config to resource starvation specific config
		starvationConfig := NewDefaultConfig()
		starvationConfig.Name = name

		// Apply configuration from YAML
		if config != nil {
			if config.BufferSize > 0 {
				starvationConfig.EventChannelSize = config.BufferSize
			}
			starvationConfig.EnableK8sEnrichment = true // Always enable for operational monitoring

			// Map YAML fields to resource starvation config
			if config.EnableEBPF {
				// eBPF is always enabled on Linux, but we can respect the disable flag
			}

			// Map noise reduction to sampling rate
			if config.NoiseReduction > 0 && config.NoiseReduction < 1.0 {
				starvationConfig.SampleRate = 1.0 - config.NoiseReduction
			}

			// Map poll interval to pattern detection window
			if config.PollInterval != "" {
				if interval, err := time.ParseDuration(config.PollInterval); err == nil {
					starvationConfig.PatternWindowSec = int(interval.Seconds())
				}
			}
		}

		// Create the resource starvation collector (base collector)
		baseCollector, err := NewCollector(starvationConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create resource starvation collector %s: %w", name, err)
		}

		return baseCollector, nil
	}

	// Register the factory with the orchestrator
	// Disabled until orchestrator API is available
	_ = factory
	// orchestrator.RegisterCollectorFactory("resource-starvation", factory)
}

// CreateResourceStarvationCollector creates a new resource starvation collector with the given configuration
func CreateResourceStarvationCollector(config *collectors.CollectorConfig) (collectors.Collector, error) {
	starvationConfig := NewDefaultConfig()

	if config != nil {
		starvationConfig.EventChannelSize = config.BufferSize
		if config.Labels != nil {
			// Store labels for later use in event metadata
		}
	}

	return NewCollector(starvationConfig, zap.NewNop())
}
