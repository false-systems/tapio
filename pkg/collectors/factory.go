package collectors

import (
	"fmt"
	"sync"

	"github.com/yairfalse/tapio/pkg/collectors/cni"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf"
	"github.com/yairfalse/tapio/pkg/collectors/journald"
	"github.com/yairfalse/tapio/pkg/collectors/k8s"
	"github.com/yairfalse/tapio/pkg/collectors/systemd"
	"github.com/yairfalse/tapio/pkg/collectors/unified"
)

// CollectorFactory manages the creation of different collector types
type CollectorFactory struct {
	factories map[string]CollectorFactoryFunc
	mu        sync.RWMutex
}

// CollectorFactoryFunc creates a collector with the given configuration
type CollectorFactoryFunc func(config unified.CollectorConfig) (unified.Collector, error)

// globalFactory is the default factory instance
var globalFactory = NewCollectorFactory()

// NewCollectorFactory creates a new collector factory
func NewCollectorFactory() *CollectorFactory {
	factory := &CollectorFactory{
		factories: make(map[string]CollectorFactoryFunc),
	}

	// Register built-in collector factories
	factory.RegisterFactory("ebpf", func(config unified.CollectorConfig) (unified.Collector, error) {
		return ebpf.NewCollector(config)
	})

	factory.RegisterFactory("k8s", func(config unified.CollectorConfig) (unified.Collector, error) {
		return k8s.NewCollector(config)
	})

	factory.RegisterFactory("kubernetes", func(config unified.CollectorConfig) (unified.Collector, error) {
		return k8s.NewCollector(config)
	})

	factory.RegisterFactory("systemd", func(config unified.CollectorConfig) (unified.Collector, error) {
		return systemd.NewCollector(config)
	})

	factory.RegisterFactory("journald", func(config unified.CollectorConfig) (unified.Collector, error) {
		return journald.NewCollector(config)
	})

	factory.RegisterFactory("cni", func(config unified.CollectorConfig) (unified.Collector, error) {
		return cni.NewCNICollector(config)
	})

	return factory
}

// RegisterFactory registers a factory for a specific collector type
func (cf *CollectorFactory) RegisterFactory(collectorType string, factory CollectorFactoryFunc) {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	cf.factories[collectorType] = factory
}

// CreateCollector creates a collector of the specified type
func (cf *CollectorFactory) CreateCollector(collectorType string, config unified.CollectorConfig) (unified.Collector, error) {
	cf.mu.RLock()
	factory, exists := cf.factories[collectorType]
	cf.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown collector type: %s", collectorType)
	}

	return factory(config)
}

// GetSupportedTypes returns all supported collector types
func (cf *CollectorFactory) GetSupportedTypes() []string {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	types := make([]string, 0, len(cf.factories))
	for collectorType := range cf.factories {
		types = append(types, collectorType)
	}

	return types
}

// Global factory functions for convenience
func RegisterFactory(collectorType string, factory CollectorFactoryFunc) {
	globalFactory.RegisterFactory(collectorType, factory)
}

func CreateCollector(collectorType string, config unified.CollectorConfig) (unified.Collector, error) {
	return globalFactory.CreateCollector(collectorType, config)
}

func GetSupportedTypes() []string {
	return globalFactory.GetSupportedTypes()
}
