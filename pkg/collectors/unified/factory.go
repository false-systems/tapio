package unified

import (
	"fmt"
	"sync"
)

// Factory creates collectors of specific types
type Factory interface {
	// Create a collector instance
	Create(config CollectorConfig) (Collector, error)

	// Get supported collector types
	SupportedTypes() []string

	// Validate configuration
	ValidateConfig(config CollectorConfig) error

	// Get default configuration for a type
	DefaultConfig(collectorType string) CollectorConfig
}

// GlobalFactory provides a centralized factory for all collector types
type GlobalFactory struct {
	factories map[string]Factory
	mu        sync.RWMutex
}

// NewGlobalFactory creates a new global factory
func NewGlobalFactory() *GlobalFactory {
	return &GlobalFactory{
		factories: make(map[string]Factory),
	}
}

// Register registers a factory for a specific collector type
func (gf *GlobalFactory) Register(collectorType string, factory Factory) error {
	gf.mu.Lock()
	defer gf.mu.Unlock()

	if _, exists := gf.factories[collectorType]; exists {
		return fmt.Errorf("factory for collector type %s already registered", collectorType)
	}

	gf.factories[collectorType] = factory
	return nil
}

// Create creates a collector of the specified type
func (gf *GlobalFactory) Create(config CollectorConfig) (Collector, error) {
	gf.mu.RLock()
	factory, exists := gf.factories[config.Type]
	gf.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no factory registered for collector type: %s", config.Type)
	}

	return factory.Create(config)
}

// SupportedTypes returns all registered collector types
func (gf *GlobalFactory) SupportedTypes() []string {
	gf.mu.RLock()
	defer gf.mu.RUnlock()

	types := make([]string, 0, len(gf.factories))
	for collectorType := range gf.factories {
		types = append(types, collectorType)
	}
	return types
}

// ValidateConfig validates configuration for a specific collector type
func (gf *GlobalFactory) ValidateConfig(config CollectorConfig) error {
	gf.mu.RLock()
	factory, exists := gf.factories[config.Type]
	gf.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no factory registered for collector type: %s", config.Type)
	}

	return factory.ValidateConfig(config)
}

// DefaultConfig returns default configuration for a collector type
func (gf *GlobalFactory) DefaultConfig(collectorType string) (CollectorConfig, error) {
	gf.mu.RLock()
	factory, exists := gf.factories[collectorType]
	gf.mu.RUnlock()

	if !exists {
		return CollectorConfig{}, fmt.Errorf("no factory registered for collector type: %s", collectorType)
	}

	return factory.DefaultConfig(collectorType), nil
}

// BaseFactory provides common factory functionality
type BaseFactory struct {
	collectorType string
	validator     ConfigValidator
}

// ConfigValidator validates type-specific configuration
type ConfigValidator interface {
	Validate(config CollectorConfig) error
}

// NewBaseFactory creates a new base factory
func NewBaseFactory(collectorType string, validator ConfigValidator) *BaseFactory {
	return &BaseFactory{
		collectorType: collectorType,
		validator:     validator,
	}
}

// SupportedTypes returns the supported types for this factory
func (bf *BaseFactory) SupportedTypes() []string {
	return []string{bf.collectorType}
}

// ValidateConfig validates the configuration using the provided validator
func (bf *BaseFactory) ValidateConfig(config CollectorConfig) error {
	if config.Type != bf.collectorType {
		return fmt.Errorf("invalid collector type: expected %s, got %s", bf.collectorType, config.Type)
	}

	if bf.validator != nil {
		return bf.validator.Validate(config)
	}

	return nil
}

// DefaultConfig returns a default configuration for the collector type
func (bf *BaseFactory) DefaultConfig(collectorType string) CollectorConfig {
	if collectorType != bf.collectorType {
		return CollectorConfig{}
	}

	return DefaultCollectorConfig(bf.collectorType, bf.collectorType)
}

// Singleton global factory instance
var globalFactoryInstance *GlobalFactory
var globalFactoryOnce sync.Once

// GetGlobalFactory returns the singleton global factory
func GetGlobalFactory() *GlobalFactory {
	globalFactoryOnce.Do(func() {
		globalFactoryInstance = NewGlobalFactory()
	})
	return globalFactoryInstance
}

// RegisterCollectorFactory is a convenience function to register a factory globally
func RegisterCollectorFactory(collectorType string, factory Factory) error {
	return GetGlobalFactory().Register(collectorType, factory)
}

// CreateCollector is a convenience function to create a collector globally
func CreateCollector(config CollectorConfig) (Collector, error) {
	return GetGlobalFactory().Create(config)
}

// GetSupportedCollectorTypes returns all globally supported collector types
func GetSupportedCollectorTypes() []string {
	return GetGlobalFactory().SupportedTypes()
}

// ValidateCollectorConfig validates a collector configuration globally
func ValidateCollectorConfig(config CollectorConfig) error {
	return GetGlobalFactory().ValidateConfig(config)
}

// GetDefaultCollectorConfig returns default configuration for a collector type
func GetDefaultCollectorConfig(collectorType string) (CollectorConfig, error) {
	return GetGlobalFactory().DefaultConfig(collectorType)
}
