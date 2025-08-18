package config

import (
	"context"
	"fmt"
)

// Collector interface is left as interface{} to avoid circular imports
// The actual collector types will be converted in the registry
type Collector interface{}

// CollectorFactory defines the interface for creating collectors from typed configurations
type CollectorFactory interface {
	// GetName returns the factory name/type
	GetName() string

	// GetCollectorType returns the collector type this factory creates
	GetCollectorType() string

	// CreateCollector creates a new collector instance from typed configuration
	CreateCollector(ctx context.Context, config CollectorConfig) (Collector, error)

	// ValidateConfig validates that the provided config is compatible with this factory
	ValidateConfig(config CollectorConfig) error
}

// BaseCollectorFactory provides common functionality for collector factories
type BaseCollectorFactory struct {
	name          string
	collectorType string
}

// NewBaseCollectorFactory creates a new base factory
func NewBaseCollectorFactory(name, collectorType string) *BaseCollectorFactory {
	return &BaseCollectorFactory{
		name:          name,
		collectorType: collectorType,
	}
}

// GetName returns the factory name
func (f *BaseCollectorFactory) GetName() string {
	return f.name
}

// GetCollectorType returns the collector type
func (f *BaseCollectorFactory) GetCollectorType() string {
	return f.collectorType
}

// ValidateConfig validates that config is not nil and passes its own validation
func (f *BaseCollectorFactory) ValidateConfig(config CollectorConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	if err := config.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	return nil
}
