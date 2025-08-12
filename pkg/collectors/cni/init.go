package cni

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors/factory"
)

func init() {
	// Register the CNI collector typed factory with error handling
	factoryInstance := NewCNIFactory()
	if err := factory.RegisterTypedFactory("cni", factoryInstance); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register CNI typed factory: %v", err)
		log.Printf("CNI collector will not be available")
	}
}

