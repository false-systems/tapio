package storageio

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the storage-io collector factory with the collector registry
func init() {
	// Register the storage-io collector factory
	RegisterStorageIOCollector()
}

// RegisterStorageIOCollector registers the storage-io collector factory with the orchestrator
func RegisterStorageIOCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert YAML config to storage-io-specific config
		storageConfig := DefaultConfig()

		// Apply configuration from YAML
		if config != nil {
			if config.BufferSize > 0 {
				storageConfig.BufferSize = config.BufferSize
			}
			storageConfig.EnableEBPF = config.EnableEBPF
			if config.LatencyThresholdMS > 0 {
				storageConfig.SlowIOThresholdMs = config.LatencyThresholdMS
			}
			if len(config.MonitorPaths) > 0 {
				storageConfig.MonitoredK8sPaths = config.MonitorPaths
			}
		}

		// Create the storage-io collector
		collector, err := NewCollector(name, storageConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create storage-io collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterCollectorFactory("storage-io", factory)
}
