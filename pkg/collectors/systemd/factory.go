package systemd

import "github.com/yairfalse/tapio/pkg/collectors"

// New creates a new systemd collector
func New(config collectors.CollectorConfig) (collectors.Collector, error) {
	return NewCollector(config)
}

// DefaultConfig returns default configuration for systemd collector
func DefaultConfig() collectors.CollectorConfig {
	return collectors.CollectorConfig{
		BufferSize:     1000,
		MetricsEnabled: true,
		Labels: map[string]string{
			"collector": "systemd",
		},
	}
}
