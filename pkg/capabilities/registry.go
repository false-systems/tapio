package capabilities

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

// CapabilityRegistry manages available system capabilities
type CapabilityRegistry struct {
	capabilities map[string]Capability
	mu           sync.RWMutex
	platform     string
}

// NewRegistry creates a new capability registry
func NewRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{
		capabilities: make(map[string]Capability),
		platform:     runtime.GOOS,
	}
}

// Register registers a capability with the registry
func (r *CapabilityRegistry) Register(cap Capability) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := cap.Name()
	if _, exists := r.capabilities[name]; exists {
		return fmt.Errorf("capability '%s' already registered", name)
	}

	r.capabilities[name] = cap
	return nil
}

// Get retrieves a capability by name
func (r *CapabilityRegistry) Get(name string) (Capability, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cap, exists := r.capabilities[name]
	if !exists {
		return nil, NewCapabilityError(name, "not registered", r.platform)
	}

	return cap, nil
}

// GetMemoryCapability retrieves a memory capability
func (r *CapabilityRegistry) GetMemoryCapability(name string) (MemoryCapability, error) {
	cap, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	memCap, ok := cap.(MemoryCapability)
	if !ok {
		return nil, NewCapabilityError(name, "does not implement MemoryCapability", r.platform)
	}

	return memCap, nil
}

// GetNetworkCapability retrieves a network capability
func (r *CapabilityRegistry) GetNetworkCapability(name string) (NetworkCapability, error) {
	cap, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	netCap, ok := cap.(NetworkCapability)
	if !ok {
		return nil, NewCapabilityError(name, "does not implement NetworkCapability", r.platform)
	}

	return netCap, nil
}

// GetSystemCapability retrieves a system capability
func (r *CapabilityRegistry) GetSystemCapability(name string) (SystemCapability, error) {
	cap, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	sysCap, ok := cap.(SystemCapability)
	if !ok {
		return nil, NewCapabilityError(name, "does not implement SystemCapability", r.platform)
	}

	return sysCap, nil
}

// List returns all registered capabilities
func (r *CapabilityRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.capabilities))
	for name := range r.capabilities {
		names = append(names, name)
	}

	return names
}

// ListByType returns capabilities that implement a specific interface
func (r *CapabilityRegistry) ListByType(capType string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var names []string
	for name, cap := range r.capabilities {
		switch capType {
		case "memory":
			if _, ok := cap.(MemoryCapability); ok {
				names = append(names, name)
			}
		case "network":
			if _, ok := cap.(NetworkCapability); ok {
				names = append(names, name)
			}
		case "system":
			if _, ok := cap.(SystemCapability); ok {
				names = append(names, name)
			}
		}
	}

	return names
}

// GetStatus returns the status of all capabilities
func (r *CapabilityRegistry) GetStatus() map[string]*CapabilityInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	status := make(map[string]*CapabilityInfo)
	for name, cap := range r.capabilities {
		status[name] = cap.Info()
	}

	return status
}

// StartAll starts all available capabilities
func (r *CapabilityRegistry) StartAll(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var errors []error
	for name, cap := range r.capabilities {
		if cap.IsAvailable() {
			if err := cap.Start(ctx); err != nil {
				errors = append(errors, fmt.Errorf("failed to start capability '%s': %w", name, err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors starting capabilities: %v", errors)
	}

	return nil
}

// StopAll stops all capabilities
func (r *CapabilityRegistry) StopAll() error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var errors []error
	for name, cap := range r.capabilities {
		if err := cap.Stop(); err != nil {
			errors = append(errors, fmt.Errorf("failed to stop capability '%s': %w", name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors stopping capabilities: %v", errors)
	}

	return nil
}

// GetAvailableCapabilities returns only capabilities that are available on this platform
func (r *CapabilityRegistry) GetAvailableCapabilities() map[string]Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()

	available := make(map[string]Capability)
	for name, cap := range r.capabilities {
		if cap.IsAvailable() {
			available[name] = cap
		}
	}

	return available
}

// GetHealthStatus returns health status for all capabilities
func (r *CapabilityRegistry) GetHealthStatus() map[string]*HealthStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	health := make(map[string]*HealthStatus)
	for name, cap := range r.capabilities {
		health[name] = cap.Health()
	}

	return health
}

// Global registry instance
var globalRegistry = NewRegistry()

// Global registry functions for convenience

// Register registers a capability with the global registry
func Register(cap Capability) error {
	return globalRegistry.Register(cap)
}

// Get retrieves a capability from the global registry
func Get(name string) (Capability, error) {
	return globalRegistry.Get(name)
}

// GetMemory retrieves a memory capability from the global registry
func GetMemory(name string) (MemoryCapability, error) {
	return globalRegistry.GetMemoryCapability(name)
}

// GetNetwork retrieves a network capability from the global registry
func GetNetwork(name string) (NetworkCapability, error) {
	return globalRegistry.GetNetworkCapability(name)
}

// GetSystem retrieves a system capability from the global registry
func GetSystem(name string) (SystemCapability, error) {
	return globalRegistry.GetSystemCapability(name)
}

// List returns all registered capabilities
func List() []string {
	return globalRegistry.List()
}

// GetStatus returns the status of all capabilities
func GetStatus() map[string]*CapabilityInfo {
	return globalRegistry.GetStatus()
}

// StartAll starts all available capabilities
func StartAll(ctx context.Context) error {
	return globalRegistry.StartAll(ctx)
}

// StopAll stops all capabilities
func StopAll() error {
	return globalRegistry.StopAll()
}