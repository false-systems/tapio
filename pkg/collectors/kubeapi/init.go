package kubeapi

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors/factory"
)

func init() {
	// Register the KubeAPI collector typed factory with error handling
	factoryInstance := NewKubeAPIFactory()
	if err := factory.RegisterTypedFactory("kubeapi", factoryInstance); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register KubeAPI typed factory: %v", err)
		log.Printf("KubeAPI collector will not be available")
	}
}
