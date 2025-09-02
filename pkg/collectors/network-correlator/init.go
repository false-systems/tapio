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
			if bufferSize, ok := config.Config["buffer_size"].(int); ok {
				correlatorConfig.BufferSize = bufferSize
			}

			if interfaces, ok := config.Config["interfaces"].([]string); ok {
				correlatorConfig.Interfaces = interfaces
			}

			if enableK8s, ok := config.Config["enable_k8s_metadata"].(bool); ok {
				correlatorConfig.EnableK8sMetadata = enableK8s
			}

			if enablePolicy, ok := config.Config["enable_policy_check"].(bool); ok {
				correlatorConfig.EnablePolicyCheck = enablePolicy
			}
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
