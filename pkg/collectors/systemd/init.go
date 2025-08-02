package systemd

import (
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	registry.Register("systemd", CreateCollector)
}

// CreateCollector creates a new systemd collector from config
func CreateCollector(config map[string]interface{}) (collectors.Collector, error) {
	// Parse configuration
	collectorConfig := collectors.DefaultCollectorConfig()

	// Override with provided config
	if bufferSize, ok := config["buffer_size"].(int); ok {
		collectorConfig.BufferSize = bufferSize
	}

	// Use factory function - get name from config or default
	name := "systemd"
	if n, ok := config["name"].(string); ok {
		name = n
	}

	return NewCollector(name)
}
