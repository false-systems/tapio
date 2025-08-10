package etcd

import (
	"fmt"
	"log"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the ETCD collector typed factory with error handling
	factory := NewETCDFactory()
	if err := registry.RegisterTypedFactory("etcd", factory); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register ETCD typed factory: %v", err)
		log.Printf("ETCD collector will not be available")
	}

	// Also register legacy factory for backward compatibility
	if err := registry.Register("etcd", NewCollectorFromConfig); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register ETCD legacy factory: %v", err)
	}
}

// NewCollectorFromConfig creates a new etcd collector from configuration
// DEPRECATED: This is for backward compatibility. Use the typed factory instead.
func NewCollectorFromConfig(config map[string]interface{}) (collectors.Collector, error) {
	// Get name from config or use default
	name := "etcd"
	if n, ok := config["name"].(string); ok {
		name = n
	}

	// Parse etcd config
	etcdConfig := Config{}

	// Parse endpoints
	if endpoints, ok := config["endpoints"].([]interface{}); ok {
		for _, ep := range endpoints {
			if epStr, ok := ep.(string); ok {
				etcdConfig.Endpoints = append(etcdConfig.Endpoints, epStr)
			}
		}
	}

	// Parse authentication
	if username, ok := config["username"].(string); ok {
		etcdConfig.Username = username
	}
	if password, ok := config["password"].(string); ok {
		etcdConfig.Password = password
	}

	// Create etcd collector with config
	collector, err := NewCollector(name, etcdConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd collector: %w", err)
	}

	return collector, nil
}
