package systemdapi

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

func init() {
	RegisterSystemDAPICollector()
}

// RegisterSystemDAPICollector registers the SystemD API collector factory
func RegisterSystemDAPICollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert orchestrator config to SystemD-API specific config
		systemdConfig := DefaultConfig()
		systemdConfig.Name = name

		// Map buffer size
		if config.BufferSize > 0 {
			systemdConfig.BufferSize = config.BufferSize
		}

		// Map SystemD-specific settings
		systemdConfig.EnableJournal = true // Default to true for journal monitoring

		// Create collector
		collector, err := NewCollector(systemdConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create SystemD-API collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register with orchestrator
	orchestrator.RegisterCollectorFactory("systemd-api", factory)
}
