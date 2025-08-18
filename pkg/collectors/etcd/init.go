package etcd

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors"
)

func init() {
	// Register the ETCD collector factory with error handling
	factoryInstance := NewETCDFactory()
	if err := collectors.RegisterCollectorFactory("etcd", factoryInstance); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register ETCD factory: %v", err)
		log.Printf("ETCD collector will not be available")
	}
}
