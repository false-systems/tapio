//go:build linux
// +build linux

package memory_leak_hunter

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the memory-leak-hunter collector factory with the collector registry
func init() {
	// Register the memory-leak-hunter collector factory
	RegisterMemoryLeakHunterCollector()
}

// RegisterMemoryLeakHunterCollector registers the memory-leak-hunter collector factory with the orchestrator
func RegisterMemoryLeakHunterCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Create the memory-leak-hunter collector
		collector, err := NewCollector(name, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create memory-leak-hunter collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterCollectorFactory("memory-leak-hunter", factory)
}
