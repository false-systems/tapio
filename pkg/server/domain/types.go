package domain

import (
	"context"
	"time"
)

// ServerStatus represents the server's health status
type ServerStatus string

const (
	StatusHealthy     ServerStatus = "healthy"
	StatusDegraded    ServerStatus = "degraded"
	StatusUnhealthy   ServerStatus = "unhealthy"
	StatusMaintenance ServerStatus = "maintenance"
)

// Server represents a server instance
type Server struct {
	ID        string
	Name      string
	Version   string
	StartTime time.Time
	Status    ServerStatus
	Endpoints []Endpoint
	Metrics   ServerMetrics
	Config    ServerConfig
}

// Endpoint represents a server endpoint
type Endpoint struct {
	Name     string
	Protocol string
	Address  string
	Port     int
	Path     string
	Status   EndpointStatus
	Metrics  EndpointMetrics
}

// EndpointStatus represents endpoint health
type EndpointStatus string

const (
	EndpointActive   EndpointStatus = "active"
	EndpointInactive EndpointStatus = "inactive"
	EndpointFailed   EndpointStatus = "failed"
)

// ServerMetrics contains server performance metrics
type ServerMetrics struct {
	RequestsTotal       uint64
	RequestsPerSecond   float64
	ErrorsTotal         uint64
	ErrorRate           float64
	ActiveConnections   uint64
	AverageResponseTime time.Duration
	MemoryUsage         uint64
	CPUUsage            float64
	LastUpdated         time.Time
}

// EndpointMetrics contains endpoint-specific metrics
type EndpointMetrics struct {
	RequestsTotal       uint64
	RequestsPerSecond   float64
	ErrorsTotal         uint64
	ErrorRate           float64
	AverageResponseTime time.Duration
	P95ResponseTime     time.Duration
	P99ResponseTime     time.Duration
	LastRequest         time.Time
}

// ServerConfig contains server configuration
type ServerConfig struct {
	Name            string
	Version         string
	Environment     string
	LogLevel        string
	MaxConnections  int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	Features        []string
}

// Request represents a generic server request
type Request struct {
	ID        string
	Type      RequestType
	Timestamp time.Time
	Source    string
	Data      interface{}
	Context   context.Context
}

// RequestType categorizes server requests
type RequestType string

const (
	RequestTypeHealth  RequestType = "health"
	RequestTypeMetrics RequestType = "metrics"
	RequestTypeEvent   RequestType = "event"
	RequestTypeStream  RequestType = "stream"
	RequestTypeQuery   RequestType = "query"
	RequestTypeCommand RequestType = "command"
)

// Response represents a generic server response
type Response struct {
	ID        string
	RequestID string
	Type      ResponseType
	Status    ResponseStatus
	Timestamp time.Time
	Data      interface{}
	Error     error
}

// ResponseType categorizes server responses
type ResponseType string

const (
	ResponseTypeSuccess ResponseType = "success"
	ResponseTypeError   ResponseType = "error"
	ResponseTypeData    ResponseType = "data"
	ResponseTypeStream  ResponseType = "stream"
)

// ResponseStatus represents response status
type ResponseStatus string

const (
	ResponseStatusOK          ResponseStatus = "ok"
	ResponseStatusError       ResponseStatus = "error"
	ResponseStatusPartial     ResponseStatus = "partial"
	ResponseStatusUnavailable ResponseStatus = "unavailable"
)

// Connection represents a client connection
type Connection struct {
	ID            string
	RemoteAddress string
	Protocol      string
	StartTime     time.Time
	LastActivity  time.Time
	Status        ConnectionStatus
	Metrics       ConnectionMetrics
}

// ConnectionStatus represents connection state
type ConnectionStatus string

const (
	ConnectionActive ConnectionStatus = "active"
	ConnectionIdle   ConnectionStatus = "idle"
	ConnectionClosed ConnectionStatus = "closed"
	ConnectionError  ConnectionStatus = "error"
)

// ConnectionMetrics contains connection-specific metrics
type ConnectionMetrics struct {
	RequestsTotal   uint64
	ErrorsTotal     uint64
	BytesReceived   uint64
	BytesSent       uint64
	LastRequestTime time.Time
	AverageLatency  time.Duration
}

// Event represents a server event
type Event struct {
	ID        string
	Type      EventType
	Severity  EventSeverity
	Source    string
	Message   string
	Timestamp time.Time
	Data      map[string]interface{}
	Context   context.Context
}

// EventType categorizes server events
type EventType string

const (
	EventTypeStartup    EventType = "startup"
	EventTypeShutdown   EventType = "shutdown"
	EventTypeRequest    EventType = "request"
	EventTypeResponse   EventType = "response"
	EventTypeError      EventType = "error"
	EventTypeMetrics    EventType = "metrics"
	EventTypeConnection EventType = "connection"
	EventTypeHealth     EventType = "health"
)

// EventSeverity represents event severity levels
type EventSeverity string

const (
	SeverityInfo     EventSeverity = "info"
	SeverityWarning  EventSeverity = "warning"
	SeverityError    EventSeverity = "error"
	SeverityCritical EventSeverity = "critical"
)

// HealthCheck represents a health check result
type HealthCheck struct {
	Name      string
	Status    HealthStatus
	Message   string
	Timestamp time.Time
	Duration  time.Duration
	Details   map[string]interface{}
}

// HealthStatus represents health check status
type HealthStatus string

const (
	HealthStatusPass HealthStatus = "pass"
	HealthStatusWarn HealthStatus = "warn"
	HealthStatusFail HealthStatus = "fail"
)

// Metrics represents server metrics data
type Metrics struct {
	Server      ServerMetrics
	Endpoints   map[string]EndpointMetrics
	Connections map[string]ConnectionMetrics
	Timestamp   time.Time
}

// Configuration represents server configuration
type Configuration struct {
	Server     ServerConfig
	Endpoints  []EndpointConfig
	Middleware []MiddlewareConfig
	Logging    LoggingConfig
	Metrics    MetricsConfig
	Security   SecurityConfig
}

// EndpointConfig represents endpoint configuration
type EndpointConfig struct {
	Name       string
	Protocol   string
	Address    string
	Port       int
	Path       string
	Enabled    bool
	Middleware []string
	Timeout    time.Duration
	RateLimit  int
	Auth       AuthConfig
}

// MiddlewareConfig represents middleware configuration
type MiddlewareConfig struct {
	Name    string
	Type    string
	Enabled bool
	Config  map[string]interface{}
}

// LoggingConfig represents logging configuration
type LoggingConfig struct {
	Level    string
	Format   string
	Output   string
	Rotation bool
	MaxSize  int
	MaxAge   int
	Compress bool
}

// MetricsConfig represents metrics configuration
type MetricsConfig struct {
	Enabled    bool
	Endpoint   string
	Interval   time.Duration
	Collectors []string
	Exporters  []string
}

// SecurityConfig represents security configuration
type SecurityConfig struct {
	TLS       TLSConfig
	Auth      AuthConfig
	RateLimit RateLimitConfig
	CORS      CORSConfig
}

// TLSConfig represents TLS configuration
type TLSConfig struct {
	Enabled  bool
	CertFile string
	KeyFile  string
	CAFile   string
}

// AuthConfig represents authentication configuration
type AuthConfig struct {
	Type    string
	Enabled bool
	Config  map[string]interface{}
}

// RateLimitConfig represents rate limiting configuration
type RateLimitConfig struct {
	Enabled    bool
	Requests   int
	Window     time.Duration
	BurstLimit int
}

// CORSConfig represents CORS configuration
type CORSConfig struct {
	Enabled        bool
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
}
