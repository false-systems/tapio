package cni

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the CNI collector typed factory with error handling
	factory := NewCNIFactory()
	if err := registry.RegisterTypedFactory("cni", factory); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register CNI typed factory: %v", err)
		log.Printf("CNI collector will not be available")
	}

	// Also register legacy factory for backward compatibility
	if err := registry.Register("cni", CreateLegacyCollector); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register CNI legacy factory: %v", err)
	}
}

// CreateLegacyCollector creates a new CNI collector from map config
// DEPRECATED: This is for backward compatibility. Use the typed factory instead.
func CreateLegacyCollector(config map[string]interface{}) (collectors.Collector, error) {
	// Get name from config or use default
	name := "cni"
	if n, ok := config["name"].(string); ok {
		name = n
	}

	// For legacy support, just use the simple NewCollector function
	// which uses default configuration
	return NewCollector(name)
}

// DefaultConfig returns default CNI configuration
// DEPRECATED: Use config.NewCNIConfig instead
func DefaultConfig() Config {
	return Config{
		BufferSize: 10000,
		EnableEBPF: true,
	}
}
