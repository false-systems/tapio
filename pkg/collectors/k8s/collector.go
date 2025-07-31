package k8s

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// NewCollector creates a new K8s collector based on configuration
func NewCollector(config collectors.CollectorConfig) (collectors.Collector, error) {
	// Try to create K8s collector with optional eBPF enhancement
	collector, err := NewMinimalK8sCollector(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s collector: %w", err)
	}

	return collector, nil
}

// Collector is an alias for the main K8s collector type
type Collector = MinimalK8sCollector
