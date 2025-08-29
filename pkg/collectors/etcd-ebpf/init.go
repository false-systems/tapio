//go:build linux
// +build linux

package etcdebpf

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

func init() {
	RegisterETCDeBPFCollector()
}

// RegisterETCDeBPFCollector registers the ETCD eBPF collector factory
func RegisterETCDeBPFCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert orchestrator config to ETCD-eBPF specific config
		etcdConfig := DefaultConfig()
		etcdConfig.Name = name

		// Map buffer size
		if config.BufferSize > 0 {
			etcdConfig.BufferSize = config.BufferSize
		}

		// Map eBPF settings
		if config.EnableEBPF {
			etcdConfig.EnableEBPF = true
		}

		// Map ETCD endpoints if provided
		if len(config.Endpoints) > 0 {
			etcdConfig.Endpoints = config.Endpoints
		}

		// Create collector
		collector, err := NewCollector(etcdConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create ETCD-eBPF collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register with orchestrator
	orchestrator.RegisterCollectorFactory("etcd-ebpf", factory)
}
