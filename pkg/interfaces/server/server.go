package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/server/adapters"
	"github.com/yairfalse/tapio/pkg/server/config"
	"github.com/yairfalse/tapio/pkg/server/core"
	"github.com/yairfalse/tapio/pkg/server/domain"
)

// Server represents the main server instance
type Server struct {
	// Core components
	serverService     domain.ServerService
	requestHandler    domain.RequestHandler
	responseHandler   domain.ResponseHandler
	middlewareManager domain.MiddlewareManager

	// Configuration
	config        *domain.Configuration
	configManager domain.ConfigurationManager

	// Adapters
	adapterRegistry *adapters.AdapterRegistry

	// Dependencies
	connectionManager domain.ConnectionManager
	endpointManager   domain.EndpointManager
	healthChecker     domain.HealthChecker
	metricsCollector  domain.MetricsCollector
	eventPublisher    domain.EventPublisher
	logger            domain.Logger

	// State management
	mu        sync.RWMutex
	started   bool
	stopped   bool
	startTime time.Time
}

// NewServer creates a new server instance
func NewServer(options ...ServerOption) (*Server, error) {
	server := &Server{
		adapterRegistry: adapters.NewAdapterRegistry(),
	}

	// Apply options
	for _, option := range options {
		if err := option(server); err != nil {
			return nil, fmt.Errorf("failed to apply server option: %w", err)
		}
	}

	// Load configuration if not provided
	if server.config == nil {
		ctx := context.Background()
		cfg, err := config.LoadConfiguration(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load configuration: %w", err)
		}
		server.config = cfg
	}

	// Initialize configuration manager if not provided
	if server.configManager == nil {
		server.configManager = config.NewConfigurationManager(server.logger)
	}

	// Initialize components
	if err := server.initializeComponents(); err != nil {
		return nil, fmt.Errorf("failed to initialize components: %w", err)
	}

	return server, nil
}

// ServerOption represents a server configuration option
type ServerOption func(*Server) error

// WithConfiguration sets the server configuration
func WithConfiguration(config *domain.Configuration) ServerOption {
	return func(s *Server) error {
		if config == nil {
			return domain.ErrInvalidRequest("configuration cannot be nil")
		}
		s.config = config
		return nil
	}
}

// WithConfigurationManager sets the configuration manager
func WithConfigurationManager(manager domain.ConfigurationManager) ServerOption {
	return func(s *Server) error {
		if manager == nil {
			return domain.ErrInvalidRequest("configuration manager cannot be nil")
		}
		s.configManager = manager
		return nil
	}
}

// WithLogger sets the logger
func WithLogger(logger domain.Logger) ServerOption {
	return func(s *Server) error {
		if logger == nil {
			return domain.ErrInvalidRequest("logger cannot be nil")
		}
		s.logger = logger
		return nil
	}
}

// WithAdapterRegistry sets the adapter registry
func WithAdapterRegistry(registry *adapters.AdapterRegistry) ServerOption {
	return func(s *Server) error {
		if registry == nil {
			return domain.ErrInvalidRequest("adapter registry cannot be nil")
		}
		s.adapterRegistry = registry
		return nil
	}
}

// WithConnectionManager sets the connection manager
func WithConnectionManager(manager domain.ConnectionManager) ServerOption {
	return func(s *Server) error {
		if manager == nil {
			return domain.ErrInvalidRequest("connection manager cannot be nil")
		}
		s.connectionManager = manager
		return nil
	}
}

// WithEndpointManager sets the endpoint manager
func WithEndpointManager(manager domain.EndpointManager) ServerOption {
	return func(s *Server) error {
		if manager == nil {
			return domain.ErrInvalidRequest("endpoint manager cannot be nil")
		}
		s.endpointManager = manager
		return nil
	}
}

// WithHealthChecker sets the health checker
func WithHealthChecker(checker domain.HealthChecker) ServerOption {
	return func(s *Server) error {
		if checker == nil {
			return domain.ErrInvalidRequest("health checker cannot be nil")
		}
		s.healthChecker = checker
		return nil
	}
}

// WithMetricsCollector sets the metrics collector
func WithMetricsCollector(collector domain.MetricsCollector) ServerOption {
	return func(s *Server) error {
		if collector == nil {
			return domain.ErrInvalidRequest("metrics collector cannot be nil")
		}
		s.metricsCollector = collector
		return nil
	}
}

// WithEventPublisher sets the event publisher
func WithEventPublisher(publisher domain.EventPublisher) ServerOption {
	return func(s *Server) error {
		if publisher == nil {
			return domain.ErrInvalidRequest("event publisher cannot be nil")
		}
		s.eventPublisher = publisher
		return nil
	}
}

// initializeComponents initializes server components
func (s *Server) initializeComponents() error {
	ctx := context.Background()

	// Initialize adapters if not provided
	if err := s.initializeAdapters(ctx); err != nil {
		return fmt.Errorf("failed to initialize adapters: %w", err)
	}

	// Initialize core components
	if err := s.initializeCoreComponents(ctx); err != nil {
		return fmt.Errorf("failed to initialize core components: %w", err)
	}

	return nil
}

// initializeAdapters initializes adapters using the registry
func (s *Server) initializeAdapters(ctx context.Context) error {
	// Initialize connection manager if not provided
	if s.connectionManager == nil {
		if manager, err := s.adapterRegistry.CreateConnectionManager(ctx, s.config); err == nil {
			s.connectionManager = manager
		}
	}

	// Initialize endpoint manager if not provided
	if s.endpointManager == nil {
		if manager, err := s.adapterRegistry.CreateEndpointManager(ctx, s.config); err == nil {
			s.endpointManager = manager
		}
	}

	// Initialize health checker if not provided
	if s.healthChecker == nil {
		if checker, err := s.adapterRegistry.CreateHealthChecker(ctx, s.config); err == nil {
			s.healthChecker = checker
		}
	}

	// Initialize metrics collector if not provided
	if s.metricsCollector == nil {
		if collector, err := s.adapterRegistry.CreateMetricsCollector(ctx, s.config); err == nil {
			s.metricsCollector = collector
		}
	}

	// Initialize event publisher if not provided
	if s.eventPublisher == nil {
		if publisher, err := s.adapterRegistry.CreateEventPublisher(ctx, s.config); err == nil {
			s.eventPublisher = publisher
		}
	}

	// Initialize logger if not provided
	if s.logger == nil {
		if logger, err := s.adapterRegistry.CreateLogger(ctx, &s.config.Logging); err == nil {
			s.logger = logger
		}
	}

	return nil
}

// initializeCoreComponents initializes core components
func (s *Server) initializeCoreComponents(ctx context.Context) error {
	// Initialize server service
	s.serverService = core.NewServerService(
		s.config,
		s.connectionManager,
		s.endpointManager,
		s.healthChecker,
		s.metricsCollector,
		s.eventPublisher,
		s.configManager,
	)

	// Initialize request handler
	validator, _ := s.adapterRegistry.CreateValidator(ctx, s.config)
	serializer, _ := s.adapterRegistry.CreateSerializer(ctx, "json")

	s.requestHandler = core.NewRequestHandler(
		s.serverService,
		validator,
		serializer,
		s.metricsCollector,
		s.eventPublisher,
		s.logger,
	)

	// Initialize response handler
	s.responseHandler = core.NewResponseHandler(
		serializer,
		validator,
		s.metricsCollector,
		s.eventPublisher,
		s.logger,
	)

	// Initialize middleware manager
	s.middlewareManager = core.NewMiddlewareManager(s.logger, s.eventPublisher)

	// Register default middleware
	if err := s.registerDefaultMiddleware(ctx); err != nil {
		return fmt.Errorf("failed to register default middleware: %w", err)
	}

	return nil
}

// registerDefaultMiddleware registers default middleware components
func (s *Server) registerDefaultMiddleware(ctx context.Context) error {
	// Register logging middleware
	if s.logger != nil {
		loggingMiddleware := core.NewLoggingMiddleware(s.logger)
		if err := s.middlewareManager.RegisterMiddleware(ctx, loggingMiddleware); err != nil {
			return fmt.Errorf("failed to register logging middleware: %w", err)
		}
	}

	// Register metrics middleware
	if s.metricsCollector != nil {
		metricsMiddleware := core.NewMetricsMiddleware(s.metricsCollector)
		if err := s.middlewareManager.RegisterMiddleware(ctx, metricsMiddleware); err != nil {
			return fmt.Errorf("failed to register metrics middleware: %w", err)
		}
	}

	// Register validation middleware
	if validator, err := s.adapterRegistry.CreateValidator(ctx, s.config); err == nil {
		validationMiddleware := core.NewValidationMiddleware(validator)
		if err := s.middlewareManager.RegisterMiddleware(ctx, validationMiddleware); err != nil {
			return fmt.Errorf("failed to register validation middleware: %w", err)
		}
	}

	// Register security middleware if security is enabled
	if s.config.Security.Auth.Enabled {
		if securityManager, err := s.adapterRegistry.CreateSecurityManager(ctx, &s.config.Security); err == nil {
			securityMiddleware := core.NewSecurityMiddleware(securityManager)
			if err := s.middlewareManager.RegisterMiddleware(ctx, securityMiddleware); err != nil {
				return fmt.Errorf("failed to register security middleware: %w", err)
			}
		}
	}

	return nil
}

// Start starts the server
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return domain.ErrServerAlreadyRunning()
	}

	s.startTime = time.Now()
	s.started = true
	s.stopped = false

	// Start the server service
	if err := s.serverService.Start(ctx); err != nil {
		s.started = false
		return fmt.Errorf("failed to start server service: %w", err)
	}

	// Log server start
	if s.logger != nil {
		s.logger.Info(ctx, fmt.Sprintf("server started: %s version %s", s.config.Server.Name, s.config.Server.Version))
	}

	return nil
}

// Stop stops the server
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started || s.stopped {
		return domain.NewServerError(domain.ErrorCodeServerNotStarted, "server is not running")
	}

	s.stopped = true

	// Stop the server service
	if err := s.serverService.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop server service: %w", err)
	}

	// Log server stop
	if s.logger != nil {
		uptime := time.Since(s.startTime)
		s.logger.Info(ctx, fmt.Sprintf("server stopped after %v", uptime))
	}

	return nil
}

// Restart restarts the server
func (s *Server) Restart(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}

	// Reset state
	s.mu.Lock()
	s.started = false
	s.stopped = false
	s.mu.Unlock()

	return s.Start(ctx)
}

// GetHealth returns the server health status
func (s *Server) GetHealth(ctx context.Context) (*domain.HealthCheck, error) {
	return s.serverService.GetHealth(ctx)
}

// GetStatus returns the server status
func (s *Server) GetStatus(ctx context.Context) (*domain.Server, error) {
	return s.serverService.GetStatus(ctx)
}

// GetMetrics returns server metrics
func (s *Server) GetMetrics(ctx context.Context) (*domain.Metrics, error) {
	return s.serverService.GetMetrics(ctx)
}

// GetConfiguration returns the server configuration
func (s *Server) GetConfiguration(ctx context.Context) (*domain.Configuration, error) {
	return s.serverService.GetConfig(ctx)
}

// UpdateConfiguration updates the server configuration
func (s *Server) UpdateConfiguration(ctx context.Context, config *domain.Configuration) error {
	return s.serverService.UpdateConfig(ctx, config)
}

// HandleRequest handles a server request
func (s *Server) HandleRequest(ctx context.Context, request *domain.Request) (*domain.Response, error) {
	// Execute middleware
	response := &domain.Response{
		ID:        s.generateResponseID(),
		RequestID: request.ID,
		Type:      domain.ResponseTypeSuccess,
		Status:    domain.ResponseStatusOK,
		Timestamp: time.Now(),
	}

	if err := s.middlewareManager.ExecuteMiddleware(ctx, request, response); err != nil {
		return nil, err
	}

	// Handle the request
	return s.requestHandler.HandleRequest(ctx, request)
}

// HandleResponse handles a server response
func (s *Server) HandleResponse(ctx context.Context, response *domain.Response) error {
	return s.responseHandler.HandleResponse(ctx, response)
}

// PublishEvent publishes an event
func (s *Server) PublishEvent(ctx context.Context, event *domain.Event) error {
	return s.serverService.PublishEvent(ctx, event)
}

// GetConnections returns all active connections
func (s *Server) GetConnections(ctx context.Context) ([]*domain.Connection, error) {
	return s.serverService.GetConnections(ctx)
}

// CloseConnection closes a specific connection
func (s *Server) CloseConnection(ctx context.Context, connectionID string) error {
	return s.serverService.CloseConnection(ctx, connectionID)
}

// IsStarted returns whether the server is started
func (s *Server) IsStarted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started
}

// IsStopped returns whether the server is stopped
func (s *Server) IsStopped() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stopped
}

// GetStartTime returns the server start time
func (s *Server) GetStartTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.startTime
}

// GetUptime returns the server uptime
func (s *Server) GetUptime() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.started {
		return 0
	}

	return time.Since(s.startTime)
}

// generateResponseID generates a unique response ID
func (s *Server) generateResponseID() string {
	return fmt.Sprintf("response-%d", time.Now().UnixNano())
}

// Convenience functions for creating servers

// NewDefaultServer creates a server with default configuration
func NewDefaultServer() (*Server, error) {
	return NewServer()
}

// NewProductionServer creates a server with production configuration
func NewProductionServer() (*Server, error) {
	config := config.ProductionConfiguration()
	return NewServer(WithConfiguration(config))
}

// NewTestServer creates a server for testing
func NewTestServer() (*Server, error) {
	config, err := config.GetEnvironmentSpecificConfig("testing")
	if err != nil {
		return nil, err
	}

	return NewServer(WithConfiguration(config))
}

// NewServerWithConfig creates a server with the specified configuration
func NewServerWithConfig(config *domain.Configuration) (*Server, error) {
	return NewServer(WithConfiguration(config))
}

// Builder pattern for server construction

// ServerBuilder provides a builder pattern for server construction
type ServerBuilder struct {
	server *Server
	err    error
}

// NewServerBuilder creates a new server builder
func NewServerBuilder() *ServerBuilder {
	return &ServerBuilder{
		server: &Server{
			adapterRegistry: adapters.NewAdapterRegistry(),
		},
	}
}

// WithConfig sets the configuration
func (b *ServerBuilder) WithConfig(config *domain.Configuration) *ServerBuilder {
	if b.err != nil {
		return b
	}

	if config == nil {
		b.err = domain.ErrInvalidRequest("configuration cannot be nil")
		return b
	}

	b.server.config = config
	return b
}

// WithConfigManager sets the configuration manager
func (b *ServerBuilder) WithConfigManager(manager domain.ConfigurationManager) *ServerBuilder {
	if b.err != nil {
		return b
	}

	if manager == nil {
		b.err = domain.ErrInvalidRequest("configuration manager cannot be nil")
		return b
	}

	b.server.configManager = manager
	return b
}

// WithLogger sets the logger
func (b *ServerBuilder) WithLogger(logger domain.Logger) *ServerBuilder {
	if b.err != nil {
		return b
	}

	if logger == nil {
		b.err = domain.ErrInvalidRequest("logger cannot be nil")
		return b
	}

	b.server.logger = logger
	return b
}

// Build builds the server
func (b *ServerBuilder) Build() (*Server, error) {
	if b.err != nil {
		return nil, b.err
	}

	// Load configuration if not provided
	if b.server.config == nil {
		ctx := context.Background()
		cfg, err := config.LoadConfiguration(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load configuration: %w", err)
		}
		b.server.config = cfg
	}

	// Initialize configuration manager if not provided
	if b.server.configManager == nil {
		b.server.configManager = config.NewConfigurationManager(b.server.logger)
	}

	// Initialize components
	if err := b.server.initializeComponents(); err != nil {
		return nil, fmt.Errorf("failed to initialize components: %w", err)
	}

	return b.server, nil
}
