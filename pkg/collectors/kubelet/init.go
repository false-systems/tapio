package kubelet

import (
	"log"

	"github.com/yairfalse/tapio/pkg/collectors/factory"
)

func init() {
	// Register the Kubelet collector typed factory with error handling
	factoryInstance := NewKubeletFactory()
	if err := factory.RegisterTypedFactory("kubelet", factoryInstance); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register Kubelet typed factory: %v", err)
		log.Printf("Kubelet collector will not be available")
	}
}
