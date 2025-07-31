package systemd

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
)

func init() {
	// Register with unified binary - will need to implement this
	// For now, just export the creation function
}

// CreateCollector creates a new systemd collector from config
func CreateCollector(config map[string]interface{}) (collectors.Collector, error) {
	name := "systemd"
	if n, ok := config["name"].(string); ok {
		name = n
	}

	collector, err := NewCollector(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create systemd collector: %w", err)
	}

	return collector, nil
}
