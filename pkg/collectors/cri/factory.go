package cri

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/config"
)

// CRIFactory implements the TypedCollectorFactory interface for CRI collector
type CRIFactory struct {
	*collectors.BaseCollectorFactory
}

// NewCRIFactory creates a new CRI factory
func NewCRIFactory() *CRIFactory {
	return &CRIFactory{
		BaseCollectorFactory: collectors.NewBaseCollectorFactory("CRI", "cri"),
	}
}

// CreateCollector creates a new CRI collector from configuration
func (f *CRIFactory) CreateCollector(cfg config.CollectorConfig) (collectors.Collector, error) {
	criConfig, ok := cfg.(*config.CRIConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type for CRI collector, expected *config.CRIConfig, got %T", cfg)
	}

	// Validate configuration
	if err := f.ValidateConfig(criConfig); err != nil {
		return nil, fmt.Errorf("CRI config validation failed: %w", err)
	}

	// Create collector based on eBPF availability
	name := criConfig.Name
	if name == "" {
		name = "cri"
	}

	if criConfig.EnableMemoryTracking || criConfig.EnableCPUTracking {
		// Try to create eBPF-enhanced collector
		collector, err := NewEBPFCollector(name, convertToLocalConfig(criConfig))
		if err != nil {
			// Fall back to regular collector if eBPF fails
			return NewCollector(name, convertToLocalConfig(criConfig))
		}
		return collector, nil
	}

	// Create regular CRI collector
	return NewCollector(name, convertToLocalConfig(criConfig))
}

// convertToLocalConfig converts the global CRIConfig to the local Config struct
func convertToLocalConfig(globalConfig *config.CRIConfig) Config {
	return Config{
		Name:                globalConfig.Name,
		SocketPath:          globalConfig.RuntimeEndpoint,
		EventBufferSize:     globalConfig.BufferSize,
		EnableEBPF:          globalConfig.EnableMemoryTracking || globalConfig.EnableCPUTracking,
		EnableMetrics:       globalConfig.MetricsEnabled,
		EnableTracing:       true, // Default
		HealthCheckInterval: globalConfig.HealthCheckInterval,
		// Set other defaults
		PollInterval:          DefaultConfig().PollInterval,
		BatchSize:             DefaultConfig().BatchSize,
		FlushInterval:         DefaultConfig().FlushInterval,
		RingBufferSize:        DefaultConfig().RingBufferSize,
		KubernetesOnly:        DefaultConfig().KubernetesOnly,
		MaxMemoryMB:           DefaultConfig().MaxMemoryMB,
		MaxCPUPercent:         DefaultConfig().MaxCPUPercent,
		HealthCheckTimeout:    DefaultConfig().HealthCheckTimeout,
		TracingEnabled:        DefaultConfig().TracingEnabled,
		TracingSampleRate:     DefaultConfig().TracingSampleRate,
		MetricsEnabled:        globalConfig.MetricsEnabled,
		MetricsInterval:       DefaultConfig().MetricsInterval,
		OTLPEndpoint:          DefaultConfig().OTLPEndpoint,
		OTLPInsecure:          DefaultConfig().OTLPInsecure,
		SpanBufferSize:        DefaultConfig().SpanBufferSize,
		SpanBatchTimeout:      DefaultConfig().SpanBatchTimeout,
		ServiceName:           DefaultConfig().ServiceName,
		ServiceVersion:        DefaultConfig().ServiceVersion,
		DeploymentEnvironment: DefaultConfig().DeploymentEnvironment,
	}
}

// SupportedTypes returns the collector types this factory supports
func (f *CRIFactory) SupportedTypes() []string {
	return f.BaseCollectorFactory.SupportedTypes()
}

// ValidateConfig validates that the provided config is compatible with this factory
func (f *CRIFactory) ValidateConfig(cfg config.CollectorConfig) error {
	// Check type
	_, ok := cfg.(*config.CRIConfig)
	if !ok {
		return fmt.Errorf("invalid config type for CRI collector, expected *config.CRIConfig, got %T", cfg)
	}

	return cfg.Validate()
}

// Ensure CRIFactory implements the CollectorFactory interface
var _ collectors.CollectorFactory = (*CRIFactory)(nil)
