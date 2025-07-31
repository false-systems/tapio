package etcd

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the etcd collector factory
	registry.Register("etcd", NewCollectorFromConfig)
}

// NewCollectorFromConfig creates a new etcd collector from configuration
func NewCollectorFromConfig(config map[string]interface{}) (collectors.Collector, error) {
	// Get name from config or use default
	name := "etcd"
	if n, ok := config["name"].(string); ok {
		name = n
	}

	// Create minimal etcd collector
	collector, err := NewCollector(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd collector: %w", err)
	}

	return collector, nil
}
