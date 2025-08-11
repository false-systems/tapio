package kubeapi

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors"
	factoryregistry "github.com/yairfalse/tapio/pkg/collectors/factory"
)

func init() {
	// Register the KubeAPI collector typed factory with error handling
	factory := NewKubeAPIFactory()
	if err := factoryregistry.RegisterTypedFactory("kubeapi", factory); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register KubeAPI typed factory: %v", err)
		log.Printf("KubeAPI collector will not be available")
	}

	// Also register legacy factory for backward compatibility
	if err := factoryregistry.Register("kubeapi", NewCollectorFromConfig); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register KubeAPI legacy factory: %v", err)
	}
}

// NewCollectorFromConfig creates a new KubeAPI collector from configuration
// DEPRECATED: This is for backward compatibility. Use the typed factory instead.
func NewCollectorFromConfig(config map[string]interface{}) (collectors.Collector, error) {
	// Extract name from config, default to "kubeapi"
	name := "kubeapi"
	if n, ok := config["name"].(string); ok {
		name = n
	}

	// Create minimal KubeAPI collector
	return NewCollector(name)
}
