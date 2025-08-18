package factory

import (
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/config"
)

// RegisterTypedFactory is a wrapper around the main collectors.RegisterCollectorFactory
// This provides backward compatibility for collectors that use the factory package import pattern
func RegisterTypedFactory(collectorType string, factory collectors.CollectorFactory) error {
	return collectors.RegisterCollectorFactory(collectorType, factory)
}

// CreateCollector creates a collector using the global registry
func CreateCollector(collectorType string, cfg config.CollectorConfig) (collectors.Collector, error) {
	return collectors.CreateCollectorFromConfig(collectorType, cfg)
}
