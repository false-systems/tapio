package cni

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the CNI collector factory
	registry.Register("cni", NewCollectorFromConfig)
}

// NewCollectorFromConfig creates a new CNI collector from configuration
func NewCollectorFromConfig(config map[string]interface{}) (collectors.Collector, error) {
	// Get name from config or use default
	name := "cni"
	if n, ok := config["name"].(string); ok {
		name = n
	}

	// Create minimal CNI collector
	collector, err := NewCollector(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create CNI collector: %w", err)
	}

	return collector, nil
}
