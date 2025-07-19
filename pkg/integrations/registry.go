package integrations

import (
	"context"
	"fmt"
	"sync"

	"github.com/falseyair/tapio/pkg/integrations/core"
)

// Registry manages all active integrations
type Registry struct {
	mu           sync.RWMutex
	integrations map[string]core.Integration
}

// NewRegistry creates a new integration registry
func NewRegistry() *Registry {
	return &Registry{
		integrations: make(map[string]core.Integration),
	}
}

// Register adds an integration to the registry
func (r *Registry) Register(integration core.Integration) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := integration.Name()
	if _, exists := r.integrations[name]; exists {
		return fmt.Errorf("integration %s already registered", name)
	}

	r.integrations[name] = integration
	return nil
}

// Get retrieves an integration by name
func (r *Registry) Get(name string) (core.Integration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	integration, exists := r.integrations[name]
	return integration, exists
}

// List returns all registered integration names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.integrations))
	for name := range r.integrations {
		names = append(names, name)
	}
	return names
}

// HealthCheck checks health of all integrations
func (r *Registry) HealthCheck(ctx context.Context) map[string]*core.HealthStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make(map[string]*core.HealthStatus)
	for name, integration := range r.integrations {
		health, err := integration.Health(ctx)
		if err != nil {
			health = &core.HealthStatus{
				Healthy: false,
				Message: fmt.Sprintf("health check failed: %v", err),
			}
		}
		results[name] = health
	}
	return results
}

// CloseAll closes all registered integrations
func (r *Registry) CloseAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for name, integration := range r.integrations {
		if err := integration.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close %s: %w", name, err))
		}
	}

	// Clear registry
	r.integrations = make(map[string]core.Integration)

	if len(errs) > 0 {
		return fmt.Errorf("errors closing integrations: %v", errs)
	}
	return nil
}
