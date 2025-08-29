package kubelet

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the kubelet collector factory with the collector registry
func init() {
	// Register the kubelet collector factory
	RegisterKubeletCollector()
}

// RegisterKubeletCollector registers the kubelet collector factory with the orchestrator
func RegisterKubeletCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert YAML config to kubelet-specific config
		kubeletConfig := DefaultConfig()
		kubeletConfig.Logger = logger

		// Apply configuration from YAML
		if config != nil {
			if config.Address != "" {
				kubeletConfig.Address = config.Address
			}
			kubeletConfig.Insecure = config.Insecure
			if config.PollInterval != "" {
				if interval, err := time.ParseDuration(config.PollInterval); err == nil {
					kubeletConfig.MetricsInterval = interval
					kubeletConfig.StatsInterval = interval
				}
			}
			// Map YAML fields to kubelet specific features
			if config.EnablePodLifecycle {
				// This would enable pod lifecycle monitoring if supported
			}
			if config.EnableResourceMetrics {
				// This would enable resource metrics if supported
			}
		}

		// Create the kubelet collector
		collector, err := NewCollector(name, kubeletConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubelet collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterCollectorFactory("kubelet", factory)
}
