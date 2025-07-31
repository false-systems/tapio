package k8s

import (
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the K8s collector factory
	registry.Register("k8s", NewCollectorFromConfig)
}

// NewCollectorFromConfig creates a new K8s collector from configuration
func NewCollectorFromConfig(config map[string]interface{}) (collectors.Collector, error) {
	// Extract name from config, default to "k8s"
	name := "k8s"
	if n, ok := config["name"].(string); ok {
		name = n
	}

	// Create minimal K8s collector
	return NewCollector(name)
}
