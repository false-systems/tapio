//go:build linux
// +build linux

package syscallerrors

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the syscall-errors collector factory with the collector registry
func init() {
	// Register the syscall-errors collector factory
	RegisterSyscallErrorsCollector()
}

// RegisterSyscallErrorsCollector registers the syscall-errors collector factory with the orchestrator
func RegisterSyscallErrorsCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert YAML config to syscall-errors-specific config
		syscallConfig := DefaultConfig()

		// Apply configuration from YAML
		if config != nil {
			if config.BufferSize > 0 {
				syscallConfig.EventChannelSize = config.BufferSize
			}
			if config.SyscallSamplingRate > 0 {
				syscallConfig.RateLimitMs = config.SyscallSamplingRate
			}
		}

		// Create the syscall-errors collector
		collector, err := NewCollector(logger, syscallConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create syscall-errors collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterCollectorFactory("syscall-errors", factory)
}
