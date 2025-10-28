package deployments

import (
	"fmt"

	"github.com/yairfalse/tapio/internal/base"
	"k8s.io/client-go/kubernetes"
)

// Config holds deployments observer configuration
type Config struct {
	// K8s client for watching Deployments
	Clientset kubernetes.Interface

	// Namespace to watch (empty = all namespaces)
	Namespace string

	// Event emitter (OTEL, Tapio, or both)
	Emitter base.Emitter

	// Output configuration
	Output base.OutputConfig
}

// Validate checks config is valid
func (c *Config) Validate() error {
	if c.Clientset == nil {
		return fmt.Errorf("clientset is required")
	}
	return nil
}
