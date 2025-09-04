package networkcorrelator

import (
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

func init() {
	// Register factory with orchestrator
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert generic config to our specific config
		correlatorConfig := DefaultConfig()
		correlatorConfig.Name = name

		// Apply any custom configuration
		if config != nil {
			if config.BufferSize > 0 {
				correlatorConfig.BufferSize = config.BufferSize
			}

			// Network-correlator specific settings from EnableCorrelation field
			correlatorConfig.EnableK8sMetadata = config.EnableCorrelation
			correlatorConfig.EnablePolicyCheck = config.EnableCorrelation
		}

		return NewCollector(name, correlatorConfig, logger)
	}

	orchestrator.RegisterCollectorFactory("network-correlator", factory)
}

// RegisterNetworkCorrelator can be called explicitly if init() isn't used
func RegisterNetworkCorrelator() {
	// Trigger init() registration
	// This is useful for explicit registration in main.go
}
