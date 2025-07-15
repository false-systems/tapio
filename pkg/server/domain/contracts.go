package domain

import (
	"context"
	"time"
)

// ServerService defines the core server service interface
type ServerService interface {
	// Server lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error

	// Health monitoring
	GetHealth(ctx context.Context) (*HealthCheck, error)
	GetStatus(ctx context.Context) (*Server, error)

	// Metrics
	GetMetrics(ctx context.Context) (*Metrics, error)

	// Configuration
	GetConfig(ctx context.Context) (*Configuration, error)
	UpdateConfig(ctx context.Context, config *Configuration) error

	// Events
	PublishEvent(ctx context.Context, event *Event) error

	// Connections
	GetConnections(ctx context.Context) ([]*Connection, error)
	CloseConnection(ctx context.Context, connectionID string) error
}

// RequestHandler defines the interface for handling server requests
type RequestHandler interface {
	// Request processing
	HandleRequest(ctx context.Context, request *Request) (*Response, error)

	// Request validation
	ValidateRequest(ctx context.Context, request *Request) error

	// Request routing
	RouteRequest(ctx context.Context, request *Request) (string, error)
}

// ResponseHandler defines the interface for handling server responses
type ResponseHandler interface {
	// Response processing
	HandleResponse(ctx context.Context, response *Response) error

	// Response formatting
	FormatResponse(ctx context.Context, response *Response) ([]byte, error)

	// Response validation
	ValidateResponse(ctx context.Context, response *Response) error
}

// ConnectionManager defines the interface for managing client connections
type ConnectionManager interface {
	// Connection lifecycle
	AcceptConnection(ctx context.Context, connection *Connection) error
	CloseConnection(ctx context.Context, connectionID string) error

	// Connection tracking
	GetConnection(ctx context.Context, connectionID string) (*Connection, error)
	GetConnections(ctx context.Context) ([]*Connection, error)

	// Connection metrics
	GetConnectionMetrics(ctx context.Context, connectionID string) (*ConnectionMetrics, error)

	// Connection cleanup
	CleanupIdleConnections(ctx context.Context, maxIdle time.Duration) error
}

// EndpointManager defines the interface for managing server endpoints
type EndpointManager interface {
	// Endpoint lifecycle
	RegisterEndpoint(ctx context.Context, endpoint *Endpoint) error
	UnregisterEndpoint(ctx context.Context, endpointName string) error

	// Endpoint status
	GetEndpoint(ctx context.Context, endpointName string) (*Endpoint, error)
	GetEndpoints(ctx context.Context) ([]*Endpoint, error)

	// Endpoint health
	CheckEndpointHealth(ctx context.Context, endpointName string) (*HealthCheck, error)

	// Endpoint metrics
	GetEndpointMetrics(ctx context.Context, endpointName string) (*EndpointMetrics, error)
}

// HealthChecker defines the interface for health checking
type HealthChecker interface {
	// Health checks
	CheckHealth(ctx context.Context) (*HealthCheck, error)
	CheckComponentHealth(ctx context.Context, component string) (*HealthCheck, error)

	// Health status
	IsHealthy(ctx context.Context) (bool, error)
	GetHealthStatus(ctx context.Context) (HealthStatus, error)
}

// MetricsCollector defines the interface for metrics collection
type MetricsCollector interface {
	// Metrics collection
	CollectMetrics(ctx context.Context) (*Metrics, error)
	CollectServerMetrics(ctx context.Context) (*ServerMetrics, error)
	CollectEndpointMetrics(ctx context.Context, endpointName string) (*EndpointMetrics, error)
	CollectConnectionMetrics(ctx context.Context, connectionID string) (*ConnectionMetrics, error)

	// Metrics recording
	RecordRequest(ctx context.Context, request *Request) error
	RecordResponse(ctx context.Context, response *Response) error
	RecordError(ctx context.Context, err error) error
}

// EventPublisher defines the interface for publishing server events
type EventPublisher interface {
	// Event publishing
	PublishEvent(ctx context.Context, event *Event) error
	PublishEvents(ctx context.Context, events []*Event) error

	// Event subscriptions
	Subscribe(ctx context.Context, eventType EventType, handler EventHandler) error
	Unsubscribe(ctx context.Context, eventType EventType, handler EventHandler) error
}

// EventHandler defines the interface for handling server events
type EventHandler interface {
	HandleEvent(ctx context.Context, event *Event) error
}

// ConfigurationManager defines the interface for configuration management
type ConfigurationManager interface {
	// Configuration loading
	LoadConfiguration(ctx context.Context) (*Configuration, error)
	ReloadConfiguration(ctx context.Context) error

	// Configuration updates
	UpdateConfiguration(ctx context.Context, config *Configuration) error
	UpdateServerConfig(ctx context.Context, config *ServerConfig) error
	UpdateEndpointConfig(ctx context.Context, endpointName string, config *EndpointConfig) error

	// Configuration validation
	ValidateConfiguration(ctx context.Context, config *Configuration) error
}

// SecurityManager defines the interface for security management
type SecurityManager interface {
	// Authentication
	Authenticate(ctx context.Context, credentials interface{}) (bool, error)

	// Authorization
	Authorize(ctx context.Context, request *Request) (bool, error)

	// Rate limiting
	CheckRateLimit(ctx context.Context, clientID string) (bool, error)

	// Security validation
	ValidateRequest(ctx context.Context, request *Request) error
}

// MiddlewareManager defines the interface for middleware management
type MiddlewareManager interface {
	// Middleware registration
	RegisterMiddleware(ctx context.Context, middleware Middleware) error
	UnregisterMiddleware(ctx context.Context, name string) error

	// Middleware execution
	ExecuteMiddleware(ctx context.Context, request *Request, response *Response) error

	// Middleware configuration
	ConfigureMiddleware(ctx context.Context, name string, config map[string]interface{}) error
}

// Middleware defines the interface for middleware components
type Middleware interface {
	// Middleware execution
	Execute(ctx context.Context, request *Request, response *Response, next func() error) error

	// Middleware info
	Name() string
	Priority() int

	// Middleware configuration
	Configure(ctx context.Context, config map[string]interface{}) error
}

// Logger defines the interface for server logging
type Logger interface {
	// Logging levels
	Debug(ctx context.Context, message string, fields ...interface{})
	Info(ctx context.Context, message string, fields ...interface{})
	Warn(ctx context.Context, message string, fields ...interface{})
	Error(ctx context.Context, message string, fields ...interface{})

	// Structured logging
	WithFields(fields map[string]interface{}) Logger
	WithError(err error) Logger
	WithRequest(request *Request) Logger
	WithResponse(response *Response) Logger
}

// Transport defines the interface for server transports
type Transport interface {
	// Transport lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// Transport info
	Name() string
	Protocol() string
	Address() string

	// Request handling
	HandleRequest(ctx context.Context, request *Request) (*Response, error)

	// Configuration
	Configure(ctx context.Context, config *EndpointConfig) error
}

// StreamHandler defines the interface for handling streaming requests
type StreamHandler interface {
	// Stream handling
	HandleStream(ctx context.Context, stream Stream) error

	// Stream validation
	ValidateStream(ctx context.Context, stream Stream) error
}

// Stream defines the interface for server streams
type Stream interface {
	// Stream info
	ID() string
	Type() string

	// Stream data
	Send(ctx context.Context, data interface{}) error
	Receive(ctx context.Context) (interface{}, error)

	// Stream lifecycle
	Close(ctx context.Context) error
	IsClosed() bool
}

// Repository defines the interface for data storage
type Repository interface {
	// Data operations
	Save(ctx context.Context, key string, value interface{}) error
	Load(ctx context.Context, key string) (interface{}, error)
	Delete(ctx context.Context, key string) error

	// Query operations
	Query(ctx context.Context, query string, params ...interface{}) ([]interface{}, error)

	// Batch operations
	SaveBatch(ctx context.Context, items map[string]interface{}) error
	LoadBatch(ctx context.Context, keys []string) (map[string]interface{}, error)
}

// Cache defines the interface for caching
type Cache interface {
	// Cache operations
	Get(ctx context.Context, key string) (interface{}, error)
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Delete(ctx context.Context, key string) error

	// Cache management
	Clear(ctx context.Context) error
	Size(ctx context.Context) (int, error)

	// Cache statistics
	Stats(ctx context.Context) (map[string]interface{}, error)
}

// Validator defines the interface for data validation
type Validator interface {
	// Request validation
	ValidateRequest(ctx context.Context, request *Request) error

	// Response validation
	ValidateResponse(ctx context.Context, response *Response) error

	// Configuration validation
	ValidateConfiguration(ctx context.Context, config *Configuration) error

	// Data validation
	ValidateData(ctx context.Context, data interface{}, schema string) error
}

// Serializer defines the interface for data serialization
type Serializer interface {
	// Serialization
	Serialize(ctx context.Context, data interface{}) ([]byte, error)
	Deserialize(ctx context.Context, data []byte, target interface{}) error

	// Content type
	ContentType() string

	// Compression
	SupportsCompression() bool
	Compress(ctx context.Context, data []byte) ([]byte, error)
	Decompress(ctx context.Context, data []byte) ([]byte, error)
}
