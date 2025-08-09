package registry

import (
	"fmt"
	"sort"
	"sync"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// CollectorFactory is a function that creates a new collector instance
// TODO: Migrate to typed config when all collectors support ConfigValidator interface
type CollectorFactory func(config map[string]interface{}) (collectors.Collector, error)

// registry holds all registered collector factories
var (
	mu        sync.RWMutex
	factories = make(map[string]CollectorFactory)
)

// Register registers a collector factory with error handling
func Register(name string, factory CollectorFactory) error {
	if name == "" {
		return fmt.Errorf("collector name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("factory cannot be nil")
	}

	mu.Lock()
	defer mu.Unlock()

	if _, exists := factories[name]; exists {
		return fmt.Errorf("collector %s already registered", name)
	}

	factories[name] = factory
	return nil
}

// CreateCollector creates a collector instance by name with configuration
func CreateCollector(name string, config map[string]interface{}) (collectors.Collector, error) {
	if name == "" {
		return nil, fmt.Errorf("collector name cannot be empty")
	}
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	mu.RLock()
	factory, exists := factories[name]
	mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown collector type: %s", name)
	}

	return factory(config)
}

// ListCollectors returns a sorted list of registered collector names
func ListCollectors() []string {
	mu.RLock()
	defer mu.RUnlock()

	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}

	sort.Strings(names)
	return names
}

// IsRegistered checks if a collector type is registered
func IsRegistered(name string) bool {
	mu.RLock()
	defer mu.RUnlock()

	_, exists := factories[name]
	return exists
}
