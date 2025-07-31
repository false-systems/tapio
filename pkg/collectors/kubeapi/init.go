package kubeapi

import (
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the KubeAPI collector factory
	registry.Register("kubeapi", NewCollectorFromConfig)
}

// NewCollectorFromConfig creates a new KubeAPI collector from configuration
func NewCollectorFromConfig(config map[string]interface{}) (collectors.Collector, error) {
	// Extract name from config, default to "kubeapi"
	name := "kubeapi"
	if n, ok := config["name"].(string); ok {
		name = n
	}

	// Create minimal KubeAPI collector
	return NewCollector(name)
}
