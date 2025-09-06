//go:build linux
// +build linux

package kernel

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/observers"
	"github.com/yairfalse/tapio/pkg/observers/orchestrator"
	"go.uber.org/zap"
)

// init registers the kernel observer factory with the observer registry
func init() {
	// Register the kernel observer factory
	RegisterKernelObserver()
}

// RegisterKernelObserver registers the kernel observer factory with the orchestrator
func RegisterKernelObserver() {
	factory := func(name string, config *orchestrator.ObserverConfigData, logger *zap.Logger) (observers.Observer, error) {
		// Convert YAML config to kernel-specific config
		kernelConfig := NewDefaultConfig(name)

		// Apply configuration from YAML
		if config != nil {
			if config.BufferSize > 0 {
				kernelConfig.BufferSize = config.BufferSize
			}
			kernelConfig.EnableEBPF = config.EnableEBPF
		}

		// Create the kernel observer
		observer, err := NewObserver(name, kernelConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create kernel observer %s: %w", name, err)
		}

		return observer, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterObserverFactory("kernel", factory)
}
