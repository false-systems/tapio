package kubeapi

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

func init() {
	RegisterKubeAPICollector()
}

// RegisterKubeAPICollector registers the KubeAPI collector factory
func RegisterKubeAPICollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert orchestrator config to KubeAPI specific config
		kubeConfig := DefaultConfig()
		kubeConfig.Name = name

		// Map buffer size
		if config.BufferSize > 0 {
			kubeConfig.BufferSize = config.BufferSize
		}

		// Map KubeAPI specific settings
		kubeConfig.WatchNamespaces = true
		kubeConfig.WatchDeployments = true
		kubeConfig.WatchServices = true
		kubeConfig.WatchPods = true
		kubeConfig.WatchEvents = true

		// Create collector
		collector, err := NewCollector(kubeConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create KubeAPI collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register with orchestrator
	orchestrator.RegisterCollectorFactory("kubeapi", factory)
}
