package kubeapi

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the KubeAPI collector factory with error handling
	if err := registry.Register("kubeapi", NewCollectorFromConfig); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register KubeAPI collector: %v", err)
		log.Printf("KubeAPI collector will not be available")
	}
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
