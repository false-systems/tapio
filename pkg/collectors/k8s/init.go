package k8s

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the K8s collector factory
	registry.Register("k8s", NewCollectorFromConfig)
}

// NewCollectorFromConfig creates a new K8s collector from configuration
func NewCollectorFromConfig(config map[string]interface{}) (collectors.Collector, error) {
	// Parse configuration
	collectorConfig := collectors.DefaultCollectorConfig()

	// Override with provided config
	if bufferSize, ok := config["buffer_size"].(int); ok {
		collectorConfig.BufferSize = bufferSize
	}

	if labels, ok := config["labels"].(map[string]interface{}); ok {
		for k, v := range labels {
			if str, ok := v.(string); ok {
				collectorConfig.Labels[k] = str
			}
		}
	}

	// K8s specific configuration
	if namespace, ok := config["namespace"].(string); ok {
		collectorConfig.Labels["namespace"] = namespace
	}

	if kubeconfig, ok := config["kubeconfig"].(string); ok {
		collectorConfig.Labels["kubeconfig"] = kubeconfig
	}

	// Create K8s collector
	collector, err := NewCollector(collectorConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s collector: %w", err)
	}

	return collector, nil
}