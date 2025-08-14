package kernel

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors/config"
	"go.uber.org/zap"
)

// KernelFactory creates Kernel collectors from type-safe configuration
type KernelFactory struct {
	*config.BaseCollectorFactory
}

// NewKernelFactory creates a new Kernel collector factory
func NewKernelFactory() *KernelFactory {
	return &KernelFactory{
		BaseCollectorFactory: config.NewBaseCollectorFactory("Kernel", "kernel"),
	}
}

// CreateCollector creates a new Kernel collector from configuration
func (f *KernelFactory) CreateCollector(ctx context.Context, cfg config.CollectorConfig) (config.Collector, error) {
	kernelConfig, ok := cfg.(*config.KernelConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type for Kernel collector, expected *config.KernelConfig, got %T", cfg)
	}

	// Convert from typed config to internal Kernel Config
	internalConfig := &Config{
		Name: kernelConfig.GetName(),
	}

	// Use the appropriate logger (in production, this would be injected)
	logger, err := zap.NewProduction()
	if err != nil {
		// Fallback to no-op logger if production logger fails
		logger = zap.NewNop()
	}

	collector, err := NewModularCollectorWithConfig(internalConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kernel collector: %w", err)
	}

	return collector, nil
}

// ValidateConfig validates that the provided config is compatible with this factory
func (f *KernelFactory) ValidateConfig(cfg config.CollectorConfig) error {
	// First run base validation
	if err := f.BaseCollectorFactory.ValidateConfig(cfg); err != nil {
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
