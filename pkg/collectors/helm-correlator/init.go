package helmcorrelator

import (
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

func init() {
	// Register factory with orchestrator
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert generic config to our specific config
		helmConfig := DefaultConfig()
		helmConfig.Name = name

		// Apply any custom configuration
		if config != nil {
			if config.BufferSize > 0 {
				helmConfig.BufferSize = config.BufferSize
			}

			helmConfig.EnableEBPF = config.EnableEBPF

			// For now, enable K8s watching by default since we don't have a specific field
			helmConfig.EnableK8sWatching = true
		}

		return NewCollector(name, helmConfig)
	}

	orchestrator.RegisterCollectorFactory("helm-correlator", factory)
}

// RegisterHelmCorrelator can be called explicitly if init() isn't used
func RegisterHelmCorrelator() {
	// Trigger init() registration
	// This is useful for explicit registration in main.go
}
