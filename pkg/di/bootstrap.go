package di

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Application represents the main application with DI container
type Application struct {
	container *Container
	modules   []Module
	started   bool
}

// NewApplication creates a new application with DI container
func NewApplication() *Application {
	return &Application{
		container: NewContainer(),
		modules:   []Module{},
	}
}

// AddModule adds a DI module to the application
func (app *Application) AddModule(module Module) *Application {
	app.modules = append(app.modules, module)
	return app
}

// Configure configures all modules
func (app *Application) Configure() error {
	for _, module := range app.modules {
		if err := module.Configure(app.container); err != nil {
			return fmt.Errorf("failed to configure module %s: %w", module.Name(), err)
		}
	}
	return nil
}

// Start starts the application and all services
func (app *Application) Start(ctx context.Context) error {
	if app.started {
		return fmt.Errorf("application already started")
	}

	// Configure all modules
	if err := app.Configure(); err != nil {
		return fmt.Errorf("failed to configure application: %w", err)
	}

	// Start all services
	if err := app.container.StartAll(ctx); err != nil {
		return fmt.Errorf("failed to start services: %w", err)
	}

	app.started = true
	return nil
}

// Stop stops the application and all services
func (app *Application) Stop(ctx context.Context) error {
	if !app.started {
		return nil
	}

	// Stop all services
	if err := app.container.StopAll(ctx); err != nil {
		return fmt.Errorf("failed to stop services: %w", err)
	}

	app.started = false
	return nil
}

// Container returns the DI container
func (app *Application) Container() *Container {
	return app.container
}

// Run runs the application with signal handling
func (app *Application) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start application
	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("failed to start application: %w", err)
	}

	fmt.Println("🚀 Application started successfully")
	fmt.Println("📋 Registered services:")
	for _, name := range app.container.ListServices() {
		info, _ := app.container.GetServiceInfo(name)
		fmt.Printf("   - %s (%s) [%v]\n", info.Name, info.Type, info.Tags)
	}

	// Wait for shutdown signal
	<-sigCh
	fmt.Println("\n🛑 Shutdown signal received, stopping application...")

	// Stop application with timeout
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopCancel()

	if err := app.Stop(stopCtx); err != nil {
		fmt.Printf("⚠️  Error during shutdown: %v\n", err)
		return err
	}

	fmt.Println("🏁 Application stopped successfully")
	return nil
}

// HealthCheck performs health checks on all services
func (app *Application) HealthCheck(ctx context.Context) map[string]error {
	return app.container.HealthCheckAll(ctx)
}

// GetService retrieves a service from the container
func (app *Application) GetService(name string) (interface{}, error) {
	return app.container.Get(name)
}

// MustGetService retrieves a service and panics if not found
func (app *Application) MustGetService(name string) interface{} {
	return app.container.MustGet(name)
}

// GetTypedService retrieves a typed service from the container
func GetTypedService[T any](app *Application, name string) (T, error) {
	return GetTyped[T](app.container, name)
}

// MustGetTypedService retrieves a typed service and panics if not found
func MustGetTypedService[T any](app *Application, name string) T {
	return MustGetTyped[T](app.container, name)
}

// Builder provides a fluent interface for building applications
type Builder struct {
	app *Application
}

// NewBuilder creates a new application builder
func NewBuilder() *Builder {
	return &Builder{
		app: NewApplication(),
	}
}

// WithModule adds a module to the application
func (b *Builder) WithModule(module Module) *Builder {
	b.app.AddModule(module)
	return b
}

// WithCoreModule adds the core module
func (b *Builder) WithCoreModule() *Builder {
	return b.WithModule(&CoreModule{})
}

// WithEngineModule adds the engine module
func (b *Builder) WithEngineModule() *Builder {
	return b.WithModule(&EngineModule{})
}

// WithClientModule adds the client module
func (b *Builder) WithClientModule() *Builder {
	return b.WithModule(&ClientModule{})
}

// WithPluginModule adds the plugin module
func (b *Builder) WithPluginModule() *Builder {
	return b.WithModule(&PluginModule{})
}

// WithAPIModule adds the API module
func (b *Builder) WithAPIModule() *Builder {
	return b.WithModule(&APIModule{})
}

// WithAllModules adds all standard modules
func (b *Builder) WithAllModules() *Builder {
	return b.
		WithCoreModule().
		WithEngineModule().
		WithClientModule().
		WithPluginModule().
		WithAPIModule()
}

// Build builds the application
func (b *Builder) Build() *Application {
	return b.app
}

// BuildAndRun builds and runs the application
func (b *Builder) BuildAndRun() error {
	return b.Build().Run()
}

// Predefined application builders for different components

// NewCLIApplication creates a CLI application
func NewCLIApplication() *Application {
	return NewBuilder().
		WithCoreModule().
		WithClientModule().
		Build()
}

// NewEngineApplication creates an engine application
func NewEngineApplication() *Application {
	return NewBuilder().
		WithCoreModule().
		WithEngineModule().
		WithAPIModule().
		WithPluginModule().
		Build()
}

// NewCollectorApplication creates a collector application
func NewCollectorApplication() *Application {
	return NewBuilder().
		WithCoreModule().
		WithClientModule().
		Build()
}

// NewGUIApplication creates a GUI application
func NewGUIApplication() *Application {
	return NewBuilder().
		WithCoreModule().
		WithClientModule().
		Build()
}

// NewPluginApplication creates a plugin application
func NewPluginApplication() *Application {
	return NewBuilder().
		WithCoreModule().
		WithPluginModule().
		Build()
}

// ServiceLocator provides global access to services (use sparingly)
var globalContainer *Container

// SetGlobalContainer sets the global container (for legacy code)
func SetGlobalContainer(container *Container) {
	globalContainer = container
}

// GetGlobalService retrieves a service from the global container
func GetGlobalService(name string) (interface{}, error) {
	if globalContainer == nil {
		return nil, fmt.Errorf("global container not set")
	}
	return globalContainer.Get(name)
}

// MustGetGlobalService retrieves a service from the global container and panics if not found
func MustGetGlobalService(name string) interface{} {
	service, err := GetGlobalService(name)
	if err != nil {
		panic(err)
	}
	return service
}

// Utility functions for common DI patterns

// Singleton creates a singleton factory function
func Singleton[T any](factory func() (T, error)) func() (T, error) {
	var instance T
	var once sync.Once
	var err error

	return func() (T, error) {
		once.Do(func() {
			instance, err = factory()
		})
		return instance, err
	}
}

// Scoped creates a scoped factory function (new instance per scope)
type Scope struct {
	instances map[string]interface{}
	mutex     sync.RWMutex
}

func NewScope() *Scope {
	return &Scope{
		instances: make(map[string]interface{}),
	}
}

func (s *Scope) GetOrCreate(key string, factory func() (interface{}, error)) (interface{}, error) {
	s.mutex.RLock()
	if instance, exists := s.instances[key]; exists {
		s.mutex.RUnlock()
		return instance, nil
	}
	s.mutex.RUnlock()

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Double-check locking
	if instance, exists := s.instances[key]; exists {
		return instance, nil
	}

	instance, err := factory()
	if err != nil {
		return nil, err
	}

	s.instances[key] = instance
	return instance, nil
}

// ConfigurationProvider interface for configuration injection
type ConfigurationProvider interface {
	GetString(key string) string
	GetInt(key string) int
	GetBool(key string) bool
	GetDuration(key string) time.Duration
}