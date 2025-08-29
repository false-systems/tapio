package etcdmetrics

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the etcd-metrics collector factory with the collector registry
func init() {
	// Register the etcd-metrics collector factory
	RegisterEtcdMetricsCollector()
}

// RegisterEtcdMetricsCollector registers the etcd-metrics collector factory with the orchestrator
func RegisterEtcdMetricsCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert YAML config to etcd-metrics-specific config
		etcdConfig := Config{
			BufferSize: 10000, // Default
		}

		// Apply configuration from YAML
		if config != nil {
			if len(config.Endpoints) > 0 {
				etcdConfig.Endpoints = config.Endpoints
			}
			etcdConfig.EnableWatch = config.EnableWatch
			if config.CertFile != "" {
				etcdConfig.CertFile = config.CertFile
			}
			if config.KeyFile != "" {
				etcdConfig.KeyFile = config.KeyFile
			}
			if config.CAFile != "" {
				etcdConfig.CAFile = config.CAFile
			}
		}

		// Create the etcd-metrics collector
		collector, err := NewCollector(name, etcdConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create etcd-metrics collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterCollectorFactory("etcd-metrics", factory)
}
