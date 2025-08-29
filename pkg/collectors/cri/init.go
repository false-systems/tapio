package cri

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the CRI collector factory with the collector registry
func init() {
	// Register the CRI collector factory
	RegisterCRICollector()
}

// RegisterCRICollector registers the CRI collector factory with the orchestrator
func RegisterCRICollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert YAML config to CRI-specific config
		criConfig := NewDefaultConfig(name)

		// Apply configuration from YAML
		if config != nil {
			if config.BufferSize > 0 {
				criConfig.BufferSize = config.BufferSize
			}
			if config.Address != "" {
				criConfig.SocketPath = config.Address // Use address field for socket path
			}
			if config.PollInterval != "" {
				if interval, err := time.ParseDuration(config.PollInterval); err == nil {
					criConfig.PollInterval = interval
				}
			}
		}

		// Create the CRI collector
		collector, err := NewCollector(name, criConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create CRI collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterCollectorFactory("cri", factory)
}
