package otel

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the OTEL collector factory with the collector registry
func init() {
	// Register the OTEL collector factory
	RegisterOTELCollector()
}

// RegisterOTELCollector registers the OTEL collector factory with the orchestrator
func RegisterOTELCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert YAML config to OTEL-specific config
		otelConfig := &Config{
			BufferSize: 10000, // Default
		}

		// Apply configuration from YAML
		if config != nil {
			if config.BufferSize > 0 {
				otelConfig.BufferSize = config.BufferSize
			}
			if config.Endpoint != "" {
				otelConfig.Endpoint = config.Endpoint
			}
			if config.Protocol != "" {
				otelConfig.Protocol = config.Protocol
			}
			if config.Headers != nil {
				otelConfig.Headers = config.Headers
			}
		}

		// Create the OTEL collector
		collector, err := NewCollector(name, otelConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTEL collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterCollectorFactory("otel", factory)
}
