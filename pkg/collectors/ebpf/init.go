package ebpf

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	// Register the eBPF collector factory
	registry.Register("ebpf", NewCollectorFromConfig)
}

// NewCollectorFromConfig creates a new eBPF collector from configuration
func NewCollectorFromConfig(config map[string]interface{}) (collectors.Collector, error) {
	// Parse configuration
	collectorConfig := collectors.DefaultCollectorConfig()

	// Override with provided config
	if bufferSize, ok := config["buffer_size"].(int); ok {
		collectorConfig.BufferSize = bufferSize
	}

	if _, ok := config["ring_buffer_size"].(int); ok {
		// TODO: Pass to BPF program when implementing configurable ring buffer
	}

	if labels, ok := config["labels"].(map[string]interface{}); ok {
		for k, v := range labels {
			if str, ok := v.(string); ok {
				collectorConfig.Labels[k] = str
			}
		}
	}

	// Create unified collector
	collector, err := NewUnifiedCollector(collectorConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create eBPF collector: %w", err)
	}

	return collector, nil
}
