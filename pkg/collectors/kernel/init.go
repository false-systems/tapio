package kernel

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors/factory"
	"go.uber.org/zap"
)

func init() {
	// Register the Kernel collector typed factory with error handling
	// Use a noop logger for init - real logger will be injected later
	logger := zap.NewNop()
	factoryInstance := NewKernelFactory(logger)
	if err := factory.RegisterTypedFactory("kernel", factoryInstance); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register Kernel typed factory: %v", err)
		log.Printf("Kernel collector will not be available")
	}
}
