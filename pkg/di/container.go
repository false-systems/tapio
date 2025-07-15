package di

import (
	"context"
	"fmt"
	"reflect"
	"sync"
)

// Container is a dependency injection container
type Container struct {
	services map[string]*ServiceDefinition
	instances map[string]interface{}
	mutex     sync.RWMutex
}

// ServiceDefinition defines how to create a service
type ServiceDefinition struct {
	Name      string
	Type      reflect.Type
	Factory   interface{}
	Singleton bool
	Instance  interface{}
	Tags      []string
}

// Lifecycle defines service lifecycle hooks
type Lifecycle interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// HealthChecker defines health check capability
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

// NewContainer creates a new DI container
func NewContainer() *Container {
	return &Container{
		services:  make(map[string]*ServiceDefinition),
		instances: make(map[string]interface{}),
	}
}

// Register registers a service factory
func (c *Container) Register(name string, factory interface{}, singleton bool, tags ...string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	factoryType := reflect.TypeOf(factory)
	if factoryType.Kind() != reflect.Func {
		return fmt.Errorf("factory must be a function")
	}

	// Get the return type (service type)
	if factoryType.NumOut() == 0 {
		return fmt.Errorf("factory must return at least one value")
	}

	serviceType := factoryType.Out(0)
	
	c.services[name] = &ServiceDefinition{
		Name:      name,
		Type:      serviceType,
		Factory:   factory,
		Singleton: singleton,
		Tags:      tags,
	}

	return nil
}

// RegisterInstance registers a service instance
func (c *Container) RegisterInstance(name string, instance interface{}, tags ...string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	instanceType := reflect.TypeOf(instance)
	
	c.services[name] = &ServiceDefinition{
		Name:      name,
		Type:      instanceType,
		Instance:  instance,
		Singleton: true,
		Tags:      tags,
	}
	
	c.instances[name] = instance
}

// Get retrieves a service instance
func (c *Container) Get(name string) (interface{}, error) {
	c.mutex.RLock()
	serviceDef, exists := c.services[name]
	c.mutex.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("service %s not found", name)
	}

	// If it's a singleton and already instantiated, return the instance
	if serviceDef.Singleton {
		c.mutex.RLock()
		if instance, exists := c.instances[name]; exists {
			c.mutex.RUnlock()
			return instance, nil
		}
		c.mutex.RUnlock()
	}

	// If it's a pre-registered instance, return it
	if serviceDef.Instance != nil {
		return serviceDef.Instance, nil
	}

	// Create new instance using factory
	instance, err := c.createInstance(serviceDef)
	if err != nil {
		return nil, err
	}

	// Store singleton instances
	if serviceDef.Singleton {
		c.mutex.Lock()
		c.instances[name] = instance
		c.mutex.Unlock()
	}

	return instance, nil
}

// GetByType retrieves a service by type
func (c *Container) GetByType(serviceType reflect.Type) (interface{}, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	for _, serviceDef := range c.services {
		if serviceDef.Type == serviceType || 
		   (serviceDef.Type.Kind() == reflect.Ptr && serviceDef.Type.Elem() == serviceType) ||
		   serviceDef.Type.Implements(serviceType) {
			c.mutex.RUnlock()
			return c.Get(serviceDef.Name)
		}
	}

	return nil, fmt.Errorf("service of type %s not found", serviceType.String())
}

// GetByTag retrieves all services with a specific tag
func (c *Container) GetByTag(tag string) ([]interface{}, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var services []interface{}
	for _, serviceDef := range c.services {
		for _, serviceTag := range serviceDef.Tags {
			if serviceTag == tag {
				c.mutex.RUnlock()
				service, err := c.Get(serviceDef.Name)
				c.mutex.RLock()
				if err != nil {
					return nil, err
				}
				services = append(services, service)
				break
			}
		}
	}

	return services, nil
}

// createInstance creates a new service instance using its factory
func (c *Container) createInstance(serviceDef *ServiceDefinition) (interface{}, error) {
	factoryValue := reflect.ValueOf(serviceDef.Factory)
	factoryType := factoryValue.Type()

	// Prepare arguments for factory function
	args := make([]reflect.Value, factoryType.NumIn())
	for i := 0; i < factoryType.NumIn(); i++ {
		argType := factoryType.In(i)
		
		// Try to resolve dependency
		dep, err := c.GetByType(argType)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve dependency %s for service %s: %w", 
				argType.String(), serviceDef.Name, err)
		}
		
		args[i] = reflect.ValueOf(dep)
	}

	// Call factory function
	results := factoryValue.Call(args)
	
	// Check for error return
	if len(results) > 1 {
		if err, ok := results[1].Interface().(error); ok && err != nil {
			return nil, fmt.Errorf("factory for service %s returned error: %w", serviceDef.Name, err)
		}
	}

	return results[0].Interface(), nil
}

// StartAll starts all services that implement Lifecycle
func (c *Container) StartAll(ctx context.Context) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	for name, serviceDef := range c.services {
		// Get the service instance
		service, err := c.Get(name)
		if err != nil {
			return fmt.Errorf("failed to get service %s: %w", name, err)
		}

		// Start if it implements Lifecycle
		if lifecycle, ok := service.(Lifecycle); ok {
			if err := lifecycle.Start(ctx); err != nil {
				return fmt.Errorf("failed to start service %s: %w", name, err)
			}
		}
	}

	return nil
}

// StopAll stops all services that implement Lifecycle
func (c *Container) StopAll(ctx context.Context) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var errors []error
	
	// Stop in reverse order of starting
	for name := range c.services {
		if instance, exists := c.instances[name]; exists {
			if lifecycle, ok := instance.(Lifecycle); ok {
				if err := lifecycle.Stop(ctx); err != nil {
					errors = append(errors, fmt.Errorf("failed to stop service %s: %w", name, err))
				}
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors stopping services: %v", errors)
	}

	return nil
}

// HealthCheckAll performs health checks on all services
func (c *Container) HealthCheckAll(ctx context.Context) map[string]error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	results := make(map[string]error)
	
	for name := range c.services {
		if instance, exists := c.instances[name]; exists {
			if checker, ok := instance.(HealthChecker); ok {
				results[name] = checker.HealthCheck(ctx)
			}
		}
	}

	return results
}

// ListServices returns all registered service names
func (c *Container) ListServices() []string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var names []string
	for name := range c.services {
		names = append(names, name)
	}

	return names
}

// GetServiceInfo returns information about a service
func (c *Container) GetServiceInfo(name string) (*ServiceInfo, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	serviceDef, exists := c.services[name]
	if !exists {
		return nil, fmt.Errorf("service %s not found", name)
	}

	_, instantiated := c.instances[name]

	return &ServiceInfo{
		Name:         serviceDef.Name,
		Type:         serviceDef.Type.String(),
		Singleton:    serviceDef.Singleton,
		Instantiated: instantiated,
		Tags:         serviceDef.Tags,
	}, nil
}

// ServiceInfo provides information about a registered service
type ServiceInfo struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Singleton    bool     `json:"singleton"`
	Instantiated bool     `json:"instantiated"`
	Tags         []string `json:"tags"`
}

// Helper functions for type-safe service registration and retrieval

// RegisterSingleton registers a singleton service
func (c *Container) RegisterSingleton(name string, factory interface{}, tags ...string) error {
	return c.Register(name, factory, true, tags...)
}

// RegisterTransient registers a transient service
func (c *Container) RegisterTransient(name string, factory interface{}, tags ...string) error {
	return c.Register(name, factory, false, tags...)
}

// MustGet retrieves a service and panics if not found
func (c *Container) MustGet(name string) interface{} {
	service, err := c.Get(name)
	if err != nil {
		panic(err)
	}
	return service
}

// GetTyped retrieves a service with type assertion
func GetTyped[T any](c *Container, name string) (T, error) {
	var zero T
	service, err := c.Get(name)
	if err != nil {
		return zero, err
	}
	
	typed, ok := service.(T)
	if !ok {
		return zero, fmt.Errorf("service %s is not of type %T", name, zero)
	}
	
	return typed, nil
}

// MustGetTyped retrieves a service with type assertion and panics if not found
func MustGetTyped[T any](c *Container, name string) T {
	service, err := GetTyped[T](c, name)
	if err != nil {
		panic(err)
	}
	return service
}