package cni

import (
	"fmt"
	"log"

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
		return nil, fmt.Errorf("invalid config type for CNI collector, expected *config.CNIConfig")
	}

	collector, err := NewCollectorWithConfig(cniConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create CNI collector: %w", err)
	}

	return collector, nil
}

func init() {
	// Register the CNI collector factory with proper error handling
	factory := NewCNIFactory()
	if err := collectors.RegisterCollectorFactory("cni", factory); err != nil {
		// Log error but don't panic - this allows the application to continue
		// In production, this would use structured logging
		log.Printf("WARNING: failed to register CNI collector factory: %v", err)
		log.Printf("CNI collector will not be available")
	}
}
