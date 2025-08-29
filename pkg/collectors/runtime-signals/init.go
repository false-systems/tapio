//go:build linux
// +build linux

package runtimesignals

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the runtime-signals collector factory with the collector registry
func init() {
	// Register the runtime-signals collector factory
	RegisterRuntimeSignalsCollector()
}

// RegisterRuntimeSignalsCollector registers the runtime-signals collector factory with the orchestrator
func RegisterRuntimeSignalsCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Create the runtime-signals collector - it uses minimal configuration
		collector, err := NewCollector(name)
		if err != nil {
			return nil, fmt.Errorf("failed to create runtime-signals collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterCollectorFactory("runtime-signals", factory)
}
