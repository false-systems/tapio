package correlation

import (
	"context"
	"fmt"
	"sync"
)

// ExtensibleFramework provides a clean way to handle optional/future components
// Instead of stubs, we use explicit capability registration and graceful degradation
type ExtensibleFramework struct {
	capabilities map[string]Capability
	mutex       sync.RWMutex
}

// Capability represents an optional system capability
type Capability interface {
	Name() string
	IsAvailable() bool
	Execute(ctx context.Context, params map[string]interface{}) (interface{}, error)
	GetRequiredParams() []string
	GetDescription() string
}

// CapabilityRegistry manages available capabilities
type CapabilityRegistry struct {
	framework *ExtensibleFramework
}

// NewExtensibleFramework creates a framework with graceful capability handling
func NewExtensibleFramework() *ExtensibleFramework {
	return &ExtensibleFramework{
		capabilities: make(map[string]Capability),
	}
}

// RegisterCapability registers a capability if it's available
func (ef *ExtensibleFramework) RegisterCapability(capability Capability) {
	ef.mutex.Lock()
	defer ef.mutex.Unlock()
	
	if capability.IsAvailable() {
		ef.capabilities[capability.Name()] = capability
	}
}

// HasCapability checks if a capability is available
func (ef *ExtensibleFramework) HasCapability(name string) bool {
	ef.mutex.RLock()
	defer ef.mutex.RUnlock()
	
	capability, exists := ef.capabilities[name]
	return exists && capability.IsAvailable()
}

// ExecuteCapability executes a capability if available, returns nil if not
func (ef *ExtensibleFramework) ExecuteCapability(ctx context.Context, name string, params map[string]interface{}) (interface{}, error) {
	ef.mutex.RLock()
	capability, exists := ef.capabilities[name]
	ef.mutex.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("capability %s not available", name)
	}
	
	if !capability.IsAvailable() {
		return nil, fmt.Errorf("capability %s is currently unavailable", name)
	}
	
	return capability.Execute(ctx, params)
}

// GetAvailableCapabilities returns list of available capabilities
func (ef *ExtensibleFramework) GetAvailableCapabilities() []string {
	ef.mutex.RLock()
	defer ef.mutex.RUnlock()
	
	capabilities := make([]string, 0, len(ef.capabilities))
	for name, capability := range ef.capabilities {
		if capability.IsAvailable() {
			capabilities = append(capabilities, name)
		}
	}
	return capabilities
}

// TryExecuteCapability executes capability if available, gracefully handles missing capabilities
func (ef *ExtensibleFramework) TryExecuteCapability(ctx context.Context, name string, params map[string]interface{}) (interface{}, bool) {
	result, err := ef.ExecuteCapability(ctx, name, params)
	return result, err == nil
}

// BasicHealingCapability implements basic healing actions that are always available
type BasicHealingCapability struct {
	name        string
	description string
	handler     func(ctx context.Context, params map[string]interface{}) (interface{}, error)
}

func NewBasicHealingCapability(name, description string, handler func(ctx context.Context, params map[string]interface{}) (interface{}, error)) *BasicHealingCapability {
	return &BasicHealingCapability{
		name:        name,
		description: description,
		handler:     handler,
	}
}

func (bhc *BasicHealingCapability) Name() string { return bhc.name }
func (bhc *BasicHealingCapability) IsAvailable() bool { return true }
func (bhc *BasicHealingCapability) GetDescription() string { return bhc.description }
func (bhc *BasicHealingCapability) GetRequiredParams() []string { return []string{} }

func (bhc *BasicHealingCapability) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	if bhc.handler != nil {
		return bhc.handler(ctx, params)
	}
	return nil, fmt.Errorf("no handler configured for capability %s", bhc.name)
}

// Advanced capabilities that may not be available
type AdvancedHealingCapability struct {
	name         string
	description  string
	available    bool
	requirements []string
	handler      func(ctx context.Context, params map[string]interface{}) (interface{}, error)
}

func NewAdvancedHealingCapability(name, description string, requirements []string) *AdvancedHealingCapability {
	return &AdvancedHealingCapability{
		name:         name,
		description:  description,
		available:    false, // Must be explicitly enabled
		requirements: requirements,
	}
}

func (ahc *AdvancedHealingCapability) Name() string { return ahc.name }
func (ahc *AdvancedHealingCapability) IsAvailable() bool { return ahc.available }
func (ahc *AdvancedHealingCapability) GetDescription() string { return ahc.description }
func (ahc *AdvancedHealingCapability) GetRequiredParams() []string { return ahc.requirements }

func (ahc *AdvancedHealingCapability) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	if !ahc.available {
		return nil, fmt.Errorf("capability %s is not enabled", ahc.name)
	}
	if ahc.handler != nil {
		return ahc.handler(ctx, params)
	}
	return nil, fmt.Errorf("no handler configured for advanced capability %s", ahc.name)
}

func (ahc *AdvancedHealingCapability) Enable(handler func(ctx context.Context, params map[string]interface{}) (interface{}, error)) {
	ahc.available = true
	ahc.handler = handler
}

// ConfigurableCapability allows runtime configuration
type ConfigurableCapability struct {
	name        string
	description string
	config      map[string]interface{}
	available   bool
	handler     func(ctx context.Context, params map[string]interface{}, config map[string]interface{}) (interface{}, error)
}

func NewConfigurableCapability(name, description string) *ConfigurableCapability {
	return &ConfigurableCapability{
		name:        name,
		description: description,
		config:      make(map[string]interface{}),
		available:   false,
	}
}

func (cc *ConfigurableCapability) Name() string { return cc.name }
func (cc *ConfigurableCapability) IsAvailable() bool { return cc.available }
func (cc *ConfigurableCapability) GetDescription() string { return cc.description }
func (cc *ConfigurableCapability) GetRequiredParams() []string { return []string{} }

func (cc *ConfigurableCapability) Configure(config map[string]interface{}, handler func(ctx context.Context, params map[string]interface{}, config map[string]interface{}) (interface{}, error)) {
	cc.config = config
	cc.handler = handler
	cc.available = true
}

func (cc *ConfigurableCapability) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	if !cc.available {
		return nil, fmt.Errorf("capability %s is not configured", cc.name)
	}
	if cc.handler != nil {
		return cc.handler(ctx, params, cc.config)
	}
	return nil, fmt.Errorf("no handler configured for capability %s", cc.name)
}