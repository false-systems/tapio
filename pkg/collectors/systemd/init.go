package systemd

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the systemd collector factory with the collector registry
func init() {
	// Register the systemd collector factory
	RegisterSystemdCollector()
}

// RegisterSystemdCollector registers the systemd collector factory with the orchestrator
func RegisterSystemdCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert YAML config to systemd-specific config
		systemdConfig := Config{
			Name:       name,
			BufferSize: 10000, // Default
		}

		// Apply configuration from YAML
		if config != nil {
			if config.BufferSize > 0 {
				systemdConfig.BufferSize = config.BufferSize
			}
			systemdConfig.EnableEBPF = config.EnableEBPF
			// Note: EnableJournal could be mapped from a new YAML field
			systemdConfig.EnableJournal = true // Default to true for operational monitoring
		}

		// Create the systemd collector
		collector, err := NewCollector(name, systemdConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create systemd collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterCollectorFactory("systemd", factory)
}
