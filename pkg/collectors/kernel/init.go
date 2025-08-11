package kernel

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors"
	factoryregistry "github.com/yairfalse/tapio/pkg/collectors/factory"
)

func init() {
	// Register the Kernel collector typed factory with error handling
	factory := NewKernelFactory()
	if err := factoryregistry.RegisterTypedFactory("kernel", factory); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register Kernel typed factory: %v", err)
		log.Printf("Kernel collector will not be available")
	}

	// Also register legacy factory for backward compatibility
	if err := factoryregistry.Register("kernel", func(config map[string]interface{}) (collectors.Collector, error) {
		// Legacy factory - ignores config for now
		return NewModularCollector("kernel")
	}); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register Kernel legacy factory: %v", err)
	}
}
