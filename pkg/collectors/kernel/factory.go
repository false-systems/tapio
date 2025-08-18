package kernel

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/config"
	"go.uber.org/zap"
)

// KernelFactory creates Kernel collectors from type-safe configuration
type KernelFactory struct {
	*collectors.BaseCollectorFactory
	logger *zap.Logger
}

// NewKernelFactory creates a new Kernel collector factory
func NewKernelFactory(logger *zap.Logger) *KernelFactory {
	return &KernelFactory{
		BaseCollectorFactory: collectors.NewBaseCollectorFactory("Kernel", "kernel"),
		logger:               logger,
	}
}

// CreateCollector creates a new Kernel collector from configuration
func (f *KernelFactory) CreateCollector(cfg config.CollectorConfig) (collectors.Collector, error) {
	kernelConfig, ok := cfg.(*config.KernelConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type for Kernel collector, expected *config.KernelConfig, got %T", cfg)
	}

	// Convert from typed config to internal Kernel Config
	internalConfig := &Config{
		Name: kernelConfig.GetName(),
	}

	collector, err := NewModularCollectorWithConfig(internalConfig, f.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kernel collector: %w", err)
	}

	return collector, nil
}

// SupportedTypes returns the collector types this factory supports
func (f *KernelFactory) SupportedTypes() []string {
	return f.BaseCollectorFactory.SupportedTypes()
}

// ValidateConfig validates that the provided config is compatible with this factory
func (f *KernelFactory) ValidateConfig(cfg config.CollectorConfig) error {
	// First run base validation
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Check type
	kernelConfig, ok := cfg.(*config.KernelConfig)
	if !ok {
		return fmt.Errorf("invalid config type for Kernel collector, expected *config.KernelConfig, got %T", cfg)
	}

	// Kernel-specific validation already handled by the config's Validate method
	// which is called by the base validation
	if !kernelConfig.EnableNetworking && !kernelConfig.EnableProcess && !kernelConfig.EnableSecurity {
		return fmt.Errorf("at least one monitoring component must be enabled for Kernel collector")
	}

	return nil
}
