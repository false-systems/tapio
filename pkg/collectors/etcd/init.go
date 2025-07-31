package etcd

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the etcd collector factory
	registry.Register("etcd", NewCollectorFromConfig)
}

// NewCollectorFromConfig creates a new etcd collector from configuration
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

	// Create etcd collector
	collector, err := NewCollector(collectorConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd collector: %w", err)
	}

	return collector, nil
}
