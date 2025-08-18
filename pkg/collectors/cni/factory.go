package cni

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/config"
)

// CNIFactory creates CNI collectors from type-safe configuration
type CNIFactory struct {
	*collectors.BaseCollectorFactory
}

// NewCNIFactory creates a new CNI collector factory
func NewCNIFactory() *CNIFactory {
	return &CNIFactory{
		BaseCollectorFactory: collectors.NewBaseCollectorFactory("CNI", "cni"),
	}
}

// CreateCollector creates a new CNI collector from configuration
func (f *CNIFactory) CreateCollector(cfg config.CollectorConfig) (collectors.Collector, error) {
	cniConfig, ok := cfg.(*config.CNIConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type for CNI collector, expected *config.CNIConfig, got %T", cfg)
	}

	// The CNI collector already has a function that accepts the typed config
	collector, err := NewCollectorWithConfig(cniConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create CNI collector: %w", err)
	}

	return collector, nil
}

// SupportedTypes returns the collector types this factory supports
func (f *CNIFactory) SupportedTypes() []string {
	return f.BaseCollectorFactory.SupportedTypes()
}

// ValidateConfig validates that the provided config is compatible with this factory
func (f *CNIFactory) ValidateConfig(cfg config.CollectorConfig) error {
	// First run base validation
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Check type
	cniConfig, ok := cfg.(*config.CNIConfig)
	if !ok {
		return fmt.Errorf("invalid config type for CNI collector, expected *config.CNIConfig, got %T", cfg)
	}

	// CNI-specific validation already handled by the config's Validate method
	// which is called by the base validation
	_ = cniConfig // Use the variable to avoid unused warning

	return nil
}
