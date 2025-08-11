package systemd

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors/factory"
)

func init() {
	// Register the Systemd collector typed factory with error handling
	factoryInstance := NewSystemdFactory()
	if err := factory.RegisterTypedFactory("systemd", factoryInstance); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register Systemd typed factory: %v", err)
		log.Printf("Systemd collector will not be available")
	}
}
