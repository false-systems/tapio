package etcd

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors/factory"
)

func init() {
	// Register the ETCD collector typed factory with error handling
	factoryInstance := NewETCDFactory()
	if err := factory.RegisterTypedFactory("etcd", factoryInstance); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register ETCD typed factory: %v", err)
		log.Printf("ETCD collector will not be available")
	}
}
