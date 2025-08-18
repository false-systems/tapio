package cri

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors/factory"
)

// init registers the CRI collector with the factory system
func init() {
	// Register the CRI collector typed factory with error handling
	factoryInstance := NewCRIFactory()
	if err := factory.RegisterTypedFactory("cri", factoryInstance); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register CRI typed factory: %v", err)
		log.Printf("CRI collector will not be available")
	}
}
