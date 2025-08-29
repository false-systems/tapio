package network

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the network collector factory with the collector registry
func init() {
	// Register the network collector factory
	RegisterNetworkCollector()
}

// RegisterNetworkCollector registers the network collector factory with the orchestrator
func RegisterNetworkCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert YAML config to network-specific config
		networkConfig := &NetworkCollectorConfig{
			BufferSize:         10000, // Default
			FlushInterval:      5 * time.Second,
			EnableIPv4:         true,
			EnableTCP:          true,
			EnableUDP:          true,
			MaxEventsPerSecond: 1000,
			SamplingRate:       1.0,
		}

		// Apply configuration from YAML
		if config != nil {
			if config.BufferSize > 0 {
				networkConfig.BufferSize = config.BufferSize
			}
			if config.NoiseReduction > 0 {
				networkConfig.SamplingRate = 1.0 - config.NoiseReduction
			}
			networkConfig.EnableHTTP = config.EnableHTTP
			networkConfig.EnableGRPC = config.EnableGRPC
			if len(config.HTTPPorts) > 0 {
				networkConfig.HTTPPorts = config.HTTPPorts
			}
			if len(config.HTTPSPorts) > 0 {
				networkConfig.HTTPSPorts = config.HTTPSPorts
			}
		}

		// Create the network collector
		collector, err := NewCollector(name, networkConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create network collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterCollectorFactory("network", factory)
}
