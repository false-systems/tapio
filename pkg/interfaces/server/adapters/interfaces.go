package adapters

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ConnectionManagerAdapter implements connection management
type ConnectionManagerAdapter interface {
	domain.ConnectionManager

	// Adapter-specific methods
	StartMonitoring(ctx context.Context) error
	StopMonitoring(ctx context.Context) error
	GetStatistics(ctx context.Context) (map[string]interface{}, error)
}

// EndpointManagerAdapter implements endpoint management
type EndpointManagerAdapter interface {
	domain.EndpointManager

	// Adapter-specific methods
	StartHealthChecking(ctx context.Context) error
	StopHealthChecking(ctx context.Context) error
	GetEndpointStatistics(ctx context.Context) (map[string]interface{}, error)
}

// HealthCheckerAdapter implements health checking
type HealthCheckerAdapter interface {
	domain.HealthChecker

	// Adapter-specific methods
	RegisterHealthCheck(ctx context.Context, name string, check HealthCheckFunc) error
	UnregisterHealthCheck(ctx context.Context, name string) error
	GetHealthCheckResults(ctx context.Context) (map[string]*domain.HealthCheck, error)
}

// HealthCheckFunc is a function that performs a health check
type HealthCheckFunc func(ctx context.Context) (*domain.HealthCheck, error)

// MetricsCollectorAdapter implements metrics collection
type MetricsCollectorAdapter interface {
	domain.MetricsCollector

	// Adapter-specific methods
	RegisterMetric(ctx context.Context, name string, collector MetricCollectorFunc) error
	UnregisterMetric(ctx context.Context, name string) error
	GetMetricRegistry(ctx context.Context) (map[string]MetricCollectorFunc, error)
}

// MetricCollectorFunc is a function that collects a specific metric
type MetricCollectorFunc func(ctx context.Context) (interface{}, error)

// EventPublisherAdapter implements event publishing
type EventPublisherAdapter interface {
	domain.EventPublisher

	// Adapter-specific methods
	StartPublishing(ctx context.Context) error
	StopPublishing(ctx context.Context) error
	GetSubscribers(ctx context.Context) (map[domain.EventType][]domain.EventHandler, error)
}

// ConfigurationManagerAdapter implements configuration management
type ConfigurationManagerAdapter interface {
	domain.ConfigurationManager

	// Adapter-specific methods
	StartWatching(ctx context.Context) error
	StopWatching(ctx context.Context) error
	GetConfigurationSource(ctx context.Context) (string, error)
}

// TransportAdapter implements transport layer functionality
type TransportAdapter interface {
	domain.Transport

	// Adapter-specific methods
	GetTransportInfo(ctx context.Context) (*TransportInfo, error)
	UpdateConfiguration(ctx context.Context, config *domain.EndpointConfig) error
}

// TransportInfo contains transport-specific information
type TransportInfo struct {
	Name              string
	Protocol          string
	Version           string
	SupportedFeatures []string
	Statistics        map[string]interface{}
}

// StreamHandlerAdapter implements stream handling
type StreamHandlerAdapter interface {
	domain.StreamHandler

	// Adapter-specific methods
	GetActiveStreams(ctx context.Context) ([]domain.Stream, error)
	GetStreamStatistics(ctx context.Context) (map[string]interface{}, error)
}

// RepositoryAdapter implements data storage
type RepositoryAdapter interface {
	domain.Repository

	// Adapter-specific methods
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	GetConnectionInfo(ctx context.Context) (*ConnectionInfo, error)
}

// ConnectionInfo contains repository connection information
type ConnectionInfo struct {
	Type     string
	Address  string
	Status   string
	Metadata map[string]interface{}
}

// CacheAdapter implements caching
type CacheAdapter interface {
	domain.Cache

	// Adapter-specific methods
	GetCacheInfo(ctx context.Context) (*CacheInfo, error)
	FlushCache(ctx context.Context) error
}

// CacheInfo contains cache information
type CacheInfo struct {
	Type       string
	Size       int
	MaxSize    int
	HitRate    float64
	Statistics map[string]interface{}
}

// ValidatorAdapter implements validation
type ValidatorAdapter interface {
	domain.Validator

	// Adapter-specific methods
	RegisterValidationRule(ctx context.Context, name string, rule ValidationRule) error
	UnregisterValidationRule(ctx context.Context, name string) error
	GetValidationRules(ctx context.Context) (map[string]ValidationRule, error)
}

// ValidationRule defines a validation rule
type ValidationRule interface {
	Validate(ctx context.Context, data interface{}) error
	Name() string
	Description() string
}

// SerializerAdapter implements serialization
type SerializerAdapter interface {
	domain.Serializer

	// Adapter-specific methods
	GetSerializerInfo(ctx context.Context) (*SerializerInfo, error)
	SetCompressionLevel(level int) error
}

// SerializerInfo contains serializer information
type SerializerInfo struct {
	Type                string
	ContentType         string
	SupportsCompression bool
	CompressionLevel    int
}

// LoggerAdapter implements logging
type LoggerAdapter interface {
	domain.Logger

	// Adapter-specific methods
	SetLogLevel(level string) error
	GetLogLevel() string
	Flush(ctx context.Context) error
}

// SecurityManagerAdapter implements security management
type SecurityManagerAdapter interface {
	domain.SecurityManager

	// Adapter-specific methods
	LoadSecurityConfig(ctx context.Context, config *domain.SecurityConfig) error
	GetSecurityStatus(ctx context.Context) (*SecurityStatus, error)
}

// SecurityStatus contains security status information
type SecurityStatus struct {
	AuthenticationEnabled bool
	AuthorizationEnabled  bool
	TLSEnabled            bool
	RateLimitingEnabled   bool
	LastSecurityCheck     time.Time
	SecurityLevel         string
}

// Factory interfaces for creating adapters

// ConnectionManagerFactory creates connection manager adapters
type ConnectionManagerFactory interface {
	CreateConnectionManager(ctx context.Context, config *domain.Configuration) (ConnectionManagerAdapter, error)
}

// EndpointManagerFactory creates endpoint manager adapters
type EndpointManagerFactory interface {
	CreateEndpointManager(ctx context.Context, config *domain.Configuration) (EndpointManagerAdapter, error)
}

// HealthCheckerFactory creates health checker adapters
type HealthCheckerFactory interface {
	CreateHealthChecker(ctx context.Context, config *domain.Configuration) (HealthCheckerAdapter, error)
}

// MetricsCollectorFactory creates metrics collector adapters
type MetricsCollectorFactory interface {
	CreateMetricsCollector(ctx context.Context, config *domain.Configuration) (MetricsCollectorAdapter, error)
}

// EventPublisherFactory creates event publisher adapters
type EventPublisherFactory interface {
	CreateEventPublisher(ctx context.Context, config *domain.Configuration) (EventPublisherAdapter, error)
}

// ConfigurationManagerFactory creates configuration manager adapters
type ConfigurationManagerFactory interface {
	CreateConfigurationManager(ctx context.Context, config *domain.Configuration) (ConfigurationManagerAdapter, error)
}

// TransportFactory creates transport adapters
type TransportFactory interface {
	CreateTransport(ctx context.Context, config *domain.EndpointConfig) (TransportAdapter, error)
}

// StreamHandlerFactory creates stream handler adapters
type StreamHandlerFactory interface {
	CreateStreamHandler(ctx context.Context, config *domain.Configuration) (StreamHandlerAdapter, error)
}

// RepositoryFactory creates repository adapters
type RepositoryFactory interface {
	CreateRepository(ctx context.Context, config *domain.Configuration) (RepositoryAdapter, error)
}

// CacheFactory creates cache adapters
type CacheFactory interface {
	CreateCache(ctx context.Context, config *domain.Configuration) (CacheAdapter, error)
}

// ValidatorFactory creates validator adapters
type ValidatorFactory interface {
	CreateValidator(ctx context.Context, config *domain.Configuration) (ValidatorAdapter, error)
}

// SerializerFactory creates serializer adapters
type SerializerFactory interface {
	CreateSerializer(ctx context.Context, format string) (SerializerAdapter, error)
}

// LoggerFactory creates logger adapters
type LoggerFactory interface {
	CreateLogger(ctx context.Context, config *domain.LoggingConfig) (LoggerAdapter, error)
}

// SecurityManagerFactory creates security manager adapters
type SecurityManagerFactory interface {
	CreateSecurityManager(ctx context.Context, config *domain.SecurityConfig) (SecurityManagerAdapter, error)
}

// AdapterRegistry manages adapter factories
type AdapterRegistry struct {
	connectionManagerFactory ConnectionManagerFactory
	endpointManagerFactory   EndpointManagerFactory
	healthCheckerFactory     HealthCheckerFactory
	metricsCollectorFactory  MetricsCollectorFactory
	eventPublisherFactory    EventPublisherFactory
	configManagerFactory     ConfigurationManagerFactory
	transportFactory         TransportFactory
	streamHandlerFactory     StreamHandlerFactory
	repositoryFactory        RepositoryFactory
	cacheFactory             CacheFactory
	validatorFactory         ValidatorFactory
	serializerFactory        SerializerFactory
	loggerFactory            LoggerFactory
	securityManagerFactory   SecurityManagerFactory
}

// NewAdapterRegistry creates a new adapter registry
func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{}
}

// RegisterConnectionManagerFactory registers a connection manager factory
func (r *AdapterRegistry) RegisterConnectionManagerFactory(factory ConnectionManagerFactory) {
	r.connectionManagerFactory = factory
}

// RegisterEndpointManagerFactory registers an endpoint manager factory
func (r *AdapterRegistry) RegisterEndpointManagerFactory(factory EndpointManagerFactory) {
	r.endpointManagerFactory = factory
}

// RegisterHealthCheckerFactory registers a health checker factory
func (r *AdapterRegistry) RegisterHealthCheckerFactory(factory HealthCheckerFactory) {
	r.healthCheckerFactory = factory
}

// RegisterMetricsCollectorFactory registers a metrics collector factory
func (r *AdapterRegistry) RegisterMetricsCollectorFactory(factory MetricsCollectorFactory) {
	r.metricsCollectorFactory = factory
}

// RegisterEventPublisherFactory registers an event publisher factory
func (r *AdapterRegistry) RegisterEventPublisherFactory(factory EventPublisherFactory) {
	r.eventPublisherFactory = factory
}

// RegisterConfigurationManagerFactory registers a configuration manager factory
func (r *AdapterRegistry) RegisterConfigurationManagerFactory(factory ConfigurationManagerFactory) {
	r.configManagerFactory = factory
}

// RegisterTransportFactory registers a transport factory
func (r *AdapterRegistry) RegisterTransportFactory(factory TransportFactory) {
	r.transportFactory = factory
}

// RegisterStreamHandlerFactory registers a stream handler factory
func (r *AdapterRegistry) RegisterStreamHandlerFactory(factory StreamHandlerFactory) {
	r.streamHandlerFactory = factory
}

// RegisterRepositoryFactory registers a repository factory
func (r *AdapterRegistry) RegisterRepositoryFactory(factory RepositoryFactory) {
	r.repositoryFactory = factory
}

// RegisterCacheFactory registers a cache factory
func (r *AdapterRegistry) RegisterCacheFactory(factory CacheFactory) {
	r.cacheFactory = factory
}

// RegisterValidatorFactory registers a validator factory
func (r *AdapterRegistry) RegisterValidatorFactory(factory ValidatorFactory) {
	r.validatorFactory = factory
}

// RegisterSerializerFactory registers a serializer factory
func (r *AdapterRegistry) RegisterSerializerFactory(factory SerializerFactory) {
	r.serializerFactory = factory
}

// RegisterLoggerFactory registers a logger factory
func (r *AdapterRegistry) RegisterLoggerFactory(factory LoggerFactory) {
	r.loggerFactory = factory
}

// RegisterSecurityManagerFactory registers a security manager factory
func (r *AdapterRegistry) RegisterSecurityManagerFactory(factory SecurityManagerFactory) {
	r.securityManagerFactory = factory
}

// Factory methods

// CreateConnectionManager creates a connection manager
func (r *AdapterRegistry) CreateConnectionManager(ctx context.Context, config *domain.Configuration) (ConnectionManagerAdapter, error) {
	if r.connectionManagerFactory == nil {
		return nil, domain.ErrNotImplemented("connection manager factory not registered")
	}
	return r.connectionManagerFactory.CreateConnectionManager(ctx, config)
}

// CreateEndpointManager creates an endpoint manager
func (r *AdapterRegistry) CreateEndpointManager(ctx context.Context, config *domain.Configuration) (EndpointManagerAdapter, error) {
	if r.endpointManagerFactory == nil {
		return nil, domain.ErrNotImplemented("endpoint manager factory not registered")
	}
	return r.endpointManagerFactory.CreateEndpointManager(ctx, config)
}

// CreateHealthChecker creates a health checker
func (r *AdapterRegistry) CreateHealthChecker(ctx context.Context, config *domain.Configuration) (HealthCheckerAdapter, error) {
	if r.healthCheckerFactory == nil {
		return nil, domain.ErrNotImplemented("health checker factory not registered")
	}
	return r.healthCheckerFactory.CreateHealthChecker(ctx, config)
}

// CreateMetricsCollector creates a metrics collector
func (r *AdapterRegistry) CreateMetricsCollector(ctx context.Context, config *domain.Configuration) (MetricsCollectorAdapter, error) {
	if r.metricsCollectorFactory == nil {
		return nil, domain.ErrNotImplemented("metrics collector factory not registered")
	}
	return r.metricsCollectorFactory.CreateMetricsCollector(ctx, config)
}

// CreateEventPublisher creates an event publisher
func (r *AdapterRegistry) CreateEventPublisher(ctx context.Context, config *domain.Configuration) (EventPublisherAdapter, error) {
	if r.eventPublisherFactory == nil {
		return nil, domain.ErrNotImplemented("event publisher factory not registered")
	}
	return r.eventPublisherFactory.CreateEventPublisher(ctx, config)
}

// CreateConfigurationManager creates a configuration manager
func (r *AdapterRegistry) CreateConfigurationManager(ctx context.Context, config *domain.Configuration) (ConfigurationManagerAdapter, error) {
	if r.configManagerFactory == nil {
		return nil, domain.ErrNotImplemented("configuration manager factory not registered")
	}
	return r.configManagerFactory.CreateConfigurationManager(ctx, config)
}

// CreateTransport creates a transport
func (r *AdapterRegistry) CreateTransport(ctx context.Context, config *domain.EndpointConfig) (TransportAdapter, error) {
	if r.transportFactory == nil {
		return nil, domain.ErrNotImplemented("transport factory not registered")
	}
	return r.transportFactory.CreateTransport(ctx, config)
}

// CreateStreamHandler creates a stream handler
func (r *AdapterRegistry) CreateStreamHandler(ctx context.Context, config *domain.Configuration) (StreamHandlerAdapter, error) {
	if r.streamHandlerFactory == nil {
		return nil, domain.ErrNotImplemented("stream handler factory not registered")
	}
	return r.streamHandlerFactory.CreateStreamHandler(ctx, config)
}

// CreateRepository creates a repository
func (r *AdapterRegistry) CreateRepository(ctx context.Context, config *domain.Configuration) (RepositoryAdapter, error) {
	if r.repositoryFactory == nil {
		return nil, domain.ErrNotImplemented("repository factory not registered")
	}
	return r.repositoryFactory.CreateRepository(ctx, config)
}

// CreateCache creates a cache
func (r *AdapterRegistry) CreateCache(ctx context.Context, config *domain.Configuration) (CacheAdapter, error) {
	if r.cacheFactory == nil {
		return nil, domain.ErrNotImplemented("cache factory not registered")
	}
	return r.cacheFactory.CreateCache(ctx, config)
}

// CreateValidator creates a validator
func (r *AdapterRegistry) CreateValidator(ctx context.Context, config *domain.Configuration) (ValidatorAdapter, error) {
	if r.validatorFactory == nil {
		return nil, domain.ErrNotImplemented("validator factory not registered")
	}
	return r.validatorFactory.CreateValidator(ctx, config)
}

// CreateSerializer creates a serializer
func (r *AdapterRegistry) CreateSerializer(ctx context.Context, format string) (SerializerAdapter, error) {
	if r.serializerFactory == nil {
		return nil, domain.ErrNotImplemented("serializer factory not registered")
	}
	return r.serializerFactory.CreateSerializer(ctx, format)
}

// CreateLogger creates a logger
func (r *AdapterRegistry) CreateLogger(ctx context.Context, config *domain.LoggingConfig) (LoggerAdapter, error) {
	if r.loggerFactory == nil {
		return nil, domain.ErrNotImplemented("logger factory not registered")
	}
	return r.loggerFactory.CreateLogger(ctx, config)
}

// CreateSecurityManager creates a security manager
func (r *AdapterRegistry) CreateSecurityManager(ctx context.Context, config *domain.SecurityConfig) (SecurityManagerAdapter, error) {
	if r.securityManagerFactory == nil {
		return nil, domain.ErrNotImplemented("security manager factory not registered")
	}
	return r.securityManagerFactory.CreateSecurityManager(ctx, config)
}
