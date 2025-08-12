package kernel

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors/factory"
)

func init() {
	// Register the Kernel collector typed factory with error handling
	factoryInstance := NewKernelFactory()
	if err := factory.RegisterTypedFactory("kernel", factoryInstance); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register Kernel typed factory: %v", err)
		log.Printf("Kernel collector will not be available")
	}
}
