//go:build linux
// +build linux

package criebpf

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the cri-ebpf collector factory with the collector registry
func init() {
	// Register the cri-ebpf collector factory
	RegisterCRIeBPFCollector()
}

// RegisterCRIeBPFCollector registers the cri-ebpf collector factory with the orchestrator
func RegisterCRIeBPFCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert YAML config to cri-ebpf-specific config
		criEbpfConfig := &Config{
			Name:       name,
			BufferSize: 10000, // Default
		}

		// Apply configuration from YAML
		if config != nil {
			if config.BufferSize > 0 {
				criEbpfConfig.BufferSize = config.BufferSize
			}
			criEbpfConfig.EnableOOMKill = config.EnableOOMKill
			criEbpfConfig.EnableMemoryPressure = config.EnableMemoryPressure
			criEbpfConfig.EnableProcessExit = config.EnableProcessExit
			criEbpfConfig.EnableProcessFork = config.EnableProcessFork
		}

		// Create the cri-ebpf collector (will use stub on non-Linux)
		collector, err := NewCollector(name, criEbpfConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create cri-ebpf collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterCollectorFactory("cri-ebpf", factory)
}
