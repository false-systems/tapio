//go:build linux
// +build linux

package kernel

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the kernel collector factory with the collector registry
func init() {
	// Register the kernel collector factory
	RegisterKernelCollector()
}

// RegisterKernelCollector registers the kernel collector factory with the orchestrator
func RegisterKernelCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert YAML config to kernel-specific config
		kernelConfig := NewDefaultConfig(name)

		// Apply configuration from YAML
		if config != nil {
			if config.BufferSize > 0 {
				kernelConfig.BufferSize = config.BufferSize
			}
			kernelConfig.EnableEBPF = config.EnableEBPF
		}

		// Create the kernel collector
		collector, err := NewCollector(name, kernelConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create kernel collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterCollectorFactory("kernel", factory)
}
