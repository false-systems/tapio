package etcd

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors/config"
)

// ETCDFactory creates ETCD collectors from type-safe configuration
type ETCDFactory struct {
	*config.BaseCollectorFactory
}

// NewETCDFactory creates a new ETCD collector factory
func NewETCDFactory() *ETCDFactory {
	return &ETCDFactory{
		BaseCollectorFactory: config.NewBaseCollectorFactory("ETCD", "etcd"),
	}
}

// CreateCollector creates a new ETCD collector from configuration
func (f *ETCDFactory) CreateCollector(ctx context.Context, cfg config.CollectorConfig) (config.Collector, error) {
	etcdConfig, ok := cfg.(*config.ETCDConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type for ETCD collector, expected *config.ETCDConfig, got %T", cfg)
	}

	// Convert from typed config to internal ETCD Config
	internalConfig := Config{
		BufferSize: etcdConfig.GetBufferSize(),
		EnableEBPF: etcdConfig.EnableEBPF,
		Endpoints:  etcdConfig.Endpoints,
		Username:   etcdConfig.Username,
		Password:   etcdConfig.Password,
	}

	// Convert TLS config if present
	if etcdConfig.TLS != nil {
		internalConfig.TLS = &TLSConfig{
			CertFile: etcdConfig.TLS.CertFile,
			KeyFile:  etcdConfig.TLS.KeyFile,
			CAFile:   etcdConfig.TLS.CAFile,
		}
	}

	collector, err := NewCollector(etcdConfig.GetName(), internalConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create ETCD collector: %w", err)
	}

	return collector, nil
}

// ValidateConfig validates that the provided config is compatible with this factory
func (f *ETCDFactory) ValidateConfig(cfg config.CollectorConfig) error {
	// First run base validation
	if err := f.BaseCollectorFactory.ValidateConfig(cfg); err != nil {
		return err
	}

	// Check type
	etcdConfig, ok := cfg.(*config.ETCDConfig)
	if !ok {
		return fmt.Errorf("invalid config type for ETCD collector, expected *config.ETCDConfig, got %T", cfg)
	}

	// ETCD-specific validation already handled by the config's Validate method
	// which is called by the base validation
	_ = etcdConfig // Use the variable to avoid unused warning

	return nil
}
