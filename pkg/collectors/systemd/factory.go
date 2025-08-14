package systemd

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// Factory creates Systemd collectors
type Factory struct{}

// NewFactory creates a new Systemd collector factory
func NewFactory() *Factory {
	return &Factory{}
}

// Create creates a new Systemd collector with the given name and config
func (f *Factory) Create(name string, config Config) (collectors.Collector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid systemd config: %w", err)
	}
	
	collector, err := NewCollector(name, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create systemd collector: %w", err)
	}
	
	return collector, nil
}

// CreateDefault creates a new Systemd collector with default configuration
func (f *Factory) CreateDefault(name string) (collectors.Collector, error) {
	return f.Create(name, DefaultConfig())
}

// GetCollectorType returns the collector type name
func (f *Factory) GetCollectorType() string {
	return "systemd"
}