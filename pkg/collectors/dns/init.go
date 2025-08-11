package dns

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors/factory"
)

func init() {
	// Register the DNS collector typed factory with error handling
	factoryInstance := NewDNSFactory()
	if err := factory.RegisterTypedFactory("dns", factoryInstance); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register DNS typed factory: %v", err)
		log.Printf("DNS collector will not be available")
	}
}

