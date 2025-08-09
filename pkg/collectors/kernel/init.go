package kernel

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the Kernel collector factory with error handling
	if err := registry.Register("kernel", func(config map[string]interface{}) (collectors.Collector, error) {
		return NewModularCollector("kernel")
	}); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register Kernel collector: %v", err)
		log.Printf("Kernel collector will not be available")
	}
}
