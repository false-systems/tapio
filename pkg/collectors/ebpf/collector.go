package ebpf

import (
	"github.com/yairfalse/tapio/pkg/collectors"
)

// NewCollector creates a new minimal eBPF collector
// This collector only collects raw data with no business logic
func NewCollector(config collectors.CollectorConfig) collectors.Collector {
	return NewSimpleCollector(config)
}

// DefaultConfig returns a default configuration for the eBPF collector
func DefaultConfig() collectors.CollectorConfig {
	return DefaultSimpleConfig()
}
