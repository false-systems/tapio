package systemd

import "github.com/yairfalse/tapio/pkg/collectors"

// NewMinimalCollector creates a new minimal systemd collector that follows the blueprint
func NewMinimalCollector(config collectors.CollectorConfig) (collectors.Collector, error) {
	return NewMinimalSystemdCollector(config)
}