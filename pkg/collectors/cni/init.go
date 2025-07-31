package cni

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the CNI collector factory
	registry.Register("cni", NewCollectorFromConfig)
}

// NewCollectorFromConfig creates a new CNI collector from configuration
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

	// Check for auto-detect setting
	autoDetect := true
	if val, ok := config["auto_detect"].(bool); ok {
		autoDetect = val
	}

	// Check for specific CNI type
	if cniType, ok := config["cni_type"].(string); ok && !autoDetect {
		collectorConfig.Labels["cni_type"] = cniType
	}

	// Create CNI collector
	collector, err := NewCollector(collectorConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create CNI collector: %w", err)
	}

	return collector, nil
}
