package registry

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/config"
)

// TypedCollectorFactory creates collectors from typed configurations
type TypedCollectorFactory = config.CollectorFactory

// registry holds all registered collector factories
var (
	mu             sync.RWMutex
	typedFactories = make(map[string]TypedCollectorFactory)
	configParser   = config.NewConfigParser()
)


// RegisterTypedFactory registers a typed collector factory
func RegisterTypedFactory(name string, factory TypedCollectorFactory) error {
	if name == "" {
		return fmt.Errorf("collector name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("factory cannot be nil")
	}

	mu.Lock()
	defer mu.Unlock()

	// Check if already registered
	if _, exists := typedFactories[name]; exists {
		return fmt.Errorf("collector %s already registered", name)
	}

	typedFactories[name] = factory
	return nil
}

// CreateCollector creates a collector instance by name with map[string]interface{} configuration
// This method parses the map into typed configurations and uses typed factories
func CreateCollector(name string, config map[string]interface{}) (collectors.Collector, error) {
	if name == "" {
		return nil, fmt.Errorf("collector name cannot be empty")
	}
	if config == nil {
		config = make(map[string]interface{})
	}

	mu.RLock()
	typedFactory, exists := typedFactories[name]
	mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown collector type: %s", name)
	}

	// Parse map config to typed config
	typedConfig, err := configParser.ParseFromMap(name, config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config for collector %s: %w", name, err)
	}

	// Validate the parsed config
	if err := typedFactory.ValidateConfig(typedConfig); err != nil {
		return nil, fmt.Errorf("config validation failed for collector %s: %w", name, err)
	}

	// Create collector and convert to proper type
	collectorInterface, err := typedFactory.CreateCollector(context.Background(), typedConfig)
	if err != nil {
		return nil, err
	}

	// Type assert to collectors.Collector
	collector, ok := collectorInterface.(collectors.Collector)
	if !ok {
		return nil, fmt.Errorf("factory returned invalid collector type for %s", name)
	}

	return collector, nil
}

// CreateTypedCollector creates a collector instance using typed configuration
func CreateTypedCollector(ctx context.Context, collectorType string, config config.CollectorConfig) (collectors.Collector, error) {
	if collectorType == "" {
		return nil, fmt.Errorf("collector type cannot be empty")
	}
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	mu.RLock()
	factory, exists := typedFactories[collectorType]
	mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no typed factory registered for collector type: %s", collectorType)
	}

	// Validate config
	if err := factory.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	// Create collector and convert to proper type
	collectorInterface, err := factory.CreateCollector(ctx, config)
	if err != nil {
		return nil, err
	}

	// Type assert to collectors.Collector
	collector, ok := collectorInterface.(collectors.Collector)
	if !ok {
		return nil, fmt.Errorf("factory returned invalid collector type")
	}

	return collector, nil
}

// ListCollectors returns a sorted list of registered collector names
func ListCollectors() []string {
	mu.RLock()
	defer mu.RUnlock()

	names := make([]string, 0, len(typedFactories))
	for name := range typedFactories {
		names = append(names, name)
	}

	sort.Strings(names)
	return names
}

// ListTypedCollectors returns a sorted list of collectors that support typed configuration
func ListTypedCollectors() []string {
	mu.RLock()
	defer mu.RUnlock()

	names := make([]string, 0, len(typedFactories))
	for name := range typedFactories {
		names = append(names, name)
	}

	sort.Strings(names)
	return names
}

// IsRegistered checks if a collector type is registered
func IsRegistered(name string) bool {
	mu.RLock()
	defer mu.RUnlock()

	_, exists := typedFactories[name]
	return exists
}

// IsTypedRegistered checks if a collector type is registered with typed factory
func IsTypedRegistered(name string) bool {
	mu.RLock()
	defer mu.RUnlock()

	_, exists := typedFactories[name]
	return exists
}

// GetTypedFactory returns the typed factory for a collector type
func GetTypedFactory(name string) (TypedCollectorFactory, error) {
	mu.RLock()
	defer mu.RUnlock()

	factory, exists := typedFactories[name]
	if !exists {
		return nil, fmt.Errorf("no typed factory registered for collector type: %s", name)
	}

	return factory, nil
}
