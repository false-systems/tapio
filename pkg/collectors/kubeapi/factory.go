package kubeapi

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// Factory creates KubeAPI collectors
type Factory struct{}

// NewFactory creates a new KubeAPI collector factory
func NewFactory() *Factory {
	return &Factory{}
}

// Create creates a new KubeAPI collector with the given name and config
func (f *Factory) Create(name string, config Config) (collectors.Collector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid kubeapi config: %w", err)
	}

	collector, err := NewCollector(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubeapi collector: %w", err)
	}

	return collector, nil
}

// CreateDefault creates a new KubeAPI collector with default configuration
func (f *Factory) CreateDefault(name string) (collectors.Collector, error) {
	return f.Create(name, DefaultConfig())
}

// GetCollectorType returns the collector type name
func (f *Factory) GetCollectorType() string {
	return "kubeapi"
}
