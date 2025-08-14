package collectors

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors/config"
)

// CollectorFactory creates collectors from type-safe configurations
type CollectorFactory interface {
	// CreateCollector creates a new collector instance from configuration
	CreateCollector(config config.CollectorConfig) (Collector, error)

	// SupportedTypes returns the collector types this factory supports
	SupportedTypes() []string

	// ValidateConfig validates configuration without creating a collector
	ValidateConfig(config config.CollectorConfig) error
}

// FactoryRegistry manages collector factories
type FactoryRegistry struct {
	factories map[string]CollectorFactory
}

// NewFactoryRegistry creates a new factory registry
func NewFactoryRegistry() *FactoryRegistry {
	return &FactoryRegistry{
		factories: make(map[string]CollectorFactory),
	}
}

// RegisterFactory registers a collector factory for given types
func (r *FactoryRegistry) RegisterFactory(collectorType string, factory CollectorFactory) error {
	if collectorType == "" {
		return fmt.Errorf("collector type cannot be empty")
	}

	if factory == nil {
		return fmt.Errorf("factory cannot be nil")
	}

	if _, exists := r.factories[collectorType]; exists {
		return fmt.Errorf("factory for type '%s' already registered", collectorType)
	}

	r.factories[collectorType] = factory
	return nil
}

// CreateCollector creates a collector from configuration
func (r *FactoryRegistry) CreateCollector(collectorType string, config config.CollectorConfig) (Collector, error) {
	factory, exists := r.factories[collectorType]
	if !exists {
		return nil, fmt.Errorf("no factory registered for collector type '%s'", collectorType)
	}

	return factory.CreateCollector(config)
}

// GetSupportedTypes returns all supported collector types
func (r *FactoryRegistry) GetSupportedTypes() []string {
	types := make([]string, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}
	return types
}

// ValidateConfig validates configuration for a specific collector type
func (r *FactoryRegistry) ValidateConfig(collectorType string, config config.CollectorConfig) error {
	factory, exists := r.factories[collectorType]
	if !exists {
		return fmt.Errorf("no factory registered for collector type '%s'", collectorType)
	}

	return factory.ValidateConfig(config)
}

// Global factory registry
var GlobalFactoryRegistry = NewFactoryRegistry()

// RegisterCollectorFactory is a convenience function to register with the global registry
func RegisterCollectorFactory(collectorType string, factory CollectorFactory) error {
	return GlobalFactoryRegistry.RegisterFactory(collectorType, factory)
}

// CreateCollectorFromConfig creates a collector using the global registry
func CreateCollectorFromConfig(collectorType string, config config.CollectorConfig) (Collector, error) {
	return GlobalFactoryRegistry.CreateCollector(collectorType, config)
}

// BaseCollectorFactory provides common functionality for collector factories
type BaseCollectorFactory struct {
	name          string
	supportedType string
}

// NewBaseCollectorFactory creates a new base factory
func NewBaseCollectorFactory(name, supportedType string) *BaseCollectorFactory {
	return &BaseCollectorFactory{
		name:          name,
		supportedType: supportedType,
	}
}

// SupportedTypes returns the supported collector type
func (f *BaseCollectorFactory) SupportedTypes() []string {
	return []string{f.supportedType}
}

// ValidateConfig performs basic validation
func (f *BaseCollectorFactory) ValidateConfig(config config.CollectorConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	return config.Validate()
}
