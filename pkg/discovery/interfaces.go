// Package discovery provides enterprise-grade auto-discovery functionality
// showcasing Go best practices and advanced concurrency patterns.
package discovery

import (
	"context"
	"time"
)

// Discovery provides type-safe discovery results using Go generics
type Discovery[T ServiceType] interface {
	// Discover performs discovery and returns strongly-typed results
	Discover(ctx context.Context, opts DiscoveryOptions) ([]T, error)

	// DiscoverStream provides real-time discovery updates via channels
	DiscoverStream(ctx context.Context, opts DiscoveryOptions) (<-chan DiscoveryResult[T], error)

	// Validate ensures discovered services are reachable and healthy
	Validate(ctx context.Context, services []T) ValidationResults

	// Health returns the current health status of the discovery system
	Health() HealthStatus
}

// Scanner defines the interface for different discovery backends
type Scanner interface {
	// Scan performs a single discovery scan
	Scan(ctx context.Context, target ScanTarget) ([]ServiceInfo, error)

	// StreamScan provides continuous scanning with configurable intervals
	StreamScan(ctx context.Context, target ScanTarget, interval time.Duration) (<-chan ScanResult, error)

	// Name returns the scanner's identifier
	Name() string

	// Capabilities returns what this scanner can discover
	Capabilities() ScannerCapabilities
}

// Validator interface for connection validation with circuit breaker support
type Validator interface {
	// ValidateConnection tests if a service is reachable
	ValidateConnection(ctx context.Context, service ServiceInfo) ValidationResult

	// ValidateBatch performs parallel validation of multiple services
	ValidateBatch(ctx context.Context, services []ServiceInfo) ValidationResults

	// HealthCheck performs comprehensive health validation
	HealthCheck(ctx context.Context, service ServiceInfo) HealthCheckResult
}

// Cache interface for discovery result caching with TTL support
type Cache interface {
	// Get retrieves cached discovery results
	Get(ctx context.Context, key CacheKey) (interface{}, bool)

	// Set stores discovery results with TTL
	Set(ctx context.Context, key CacheKey, value interface{}, ttl time.Duration) error

	// Invalidate removes specific cache entries
	Invalidate(ctx context.Context, pattern string) error

	// Clear removes all cache entries
	Clear(ctx context.Context) error

	// Stats returns cache performance metrics
	Stats() CacheStats
}

// ServiceType represents different types of discoverable services
type ServiceType interface {
	// GetID returns unique service identifier
	GetID() string

	// GetType returns the service type
	GetType() string

	// GetEndpoints returns service endpoints
	GetEndpoints() []Endpoint

	// GetMetadata returns service metadata
	GetMetadata() map[string]string
}

// WorkerPool interface for concurrent discovery operations
type WorkerPool interface {
	// Submit submits work to the pool
	Submit(ctx context.Context, work WorkFunc) error

	// SubmitWithResult submits work and returns a result channel
	SubmitWithResult(ctx context.Context, work WorkFuncWithResult) <-chan WorkResult

	// Resize dynamically adjusts pool size
	Resize(size int) error

	// Stats returns pool performance metrics
	Stats() PoolStats

	// Shutdown gracefully shuts down the worker pool
	Shutdown(ctx context.Context) error
}

// CircuitBreaker interface for resilient service discovery
type CircuitBreaker interface {
	// Execute runs the function with circuit breaker protection
	Execute(ctx context.Context, fn func() error) error

	// ExecuteWithFallback runs with fallback on failure
	ExecuteWithFallback(ctx context.Context, fn func() error, fallback func() error) error

	// State returns current circuit breaker state
	State() CircuitState

	// Reset manually resets the circuit breaker
	Reset()
}

// DiscoveryOptions configures discovery behavior
type DiscoveryOptions struct {
	// Timeout for discovery operations
	Timeout time.Duration

	// Concurrency level for parallel discovery
	Concurrency int

	// EnableCache whether to use caching
	EnableCache bool

	// CacheTTL cache time-to-live
	CacheTTL time.Duration

	// EnableValidation whether to validate discovered services
	EnableValidation bool

	// Filters to apply during discovery
	Filters []DiscoveryFilter

	// Labels for service selection
	Labels map[string]string

	// Namespaces to search (Kubernetes-specific)
	Namespaces []string
}

// DiscoveryResult represents a discovery operation result
type DiscoveryResult[T ServiceType] struct {
	// Services contains discovered services
	Services []T

	// Error if discovery failed
	Error error

	// Timestamp when discovery was performed
	Timestamp time.Time

	// Duration how long discovery took
	Duration time.Duration

	// Source which scanner produced this result
	Source string

	// Metadata additional result information
	Metadata map[string]interface{}
}

// ScanTarget defines what to scan for
type ScanTarget struct {
	// Type of scan (kubernetes, docker, process, network)
	Type ScanType

	// Scope defines the scan boundary
	Scope ScanScope

	// Filters to apply during scanning
	Filters []ScanFilter

	// Options scanner-specific options
	Options map[string]interface{}
}

// ServiceInfo represents discovered service information
type ServiceInfo struct {
	// ID unique service identifier
	ID string

	// Name service name
	Name string

	// Type service type
	Type string

	// Endpoints service endpoints
	Endpoints []Endpoint

	// Metadata service metadata
	Metadata map[string]string

	// Labels service labels
	Labels map[string]string

	// Namespace service namespace (if applicable)
	Namespace string

	// DiscoveredAt when this service was discovered
	DiscoveredAt time.Time

	// LastSeen when this service was last seen
	LastSeen time.Time

	// Health current health status
	Health HealthStatus
}

// Endpoint represents a service endpoint
type Endpoint struct {
	// Address endpoint address
	Address string

	// Port endpoint port
	Port int

	// Protocol endpoint protocol (http, https, tcp, udp)
	Protocol string

	// Path endpoint path (for HTTP endpoints)
	Path string

	// Secure whether endpoint uses TLS
	Secure bool

	// Metadata endpoint-specific metadata
	Metadata map[string]string
}

// ValidationResult represents service validation outcome
type ValidationResult struct {
	// ServiceID which service was validated
	ServiceID string

	// Valid whether the service is reachable
	Valid bool

	// Error validation error if any
	Error error

	// ResponseTime how long validation took
	ResponseTime time.Duration

	// Timestamp when validation was performed
	Timestamp time.Time

	// Details additional validation details
	Details map[string]interface{}
}

// ValidationResults contains batch validation results
type ValidationResults struct {
	// Results individual validation results
	Results []ValidationResult

	// Summary validation summary statistics
	Summary ValidationSummary

	// Duration total validation duration
	Duration time.Duration
}

// HealthCheckResult represents comprehensive health check
type HealthCheckResult struct {
	// ServiceID which service was checked
	ServiceID string

	// Healthy overall health status
	Healthy bool

	// Checks individual health check results
	Checks map[string]CheckResult

	// Score overall health score (0-100)
	Score int

	// Timestamp when health check was performed
	Timestamp time.Time
}

// WorkFunc represents work to be executed by worker pool
type WorkFunc func(ctx context.Context) error

// WorkFuncWithResult represents work that returns a result
type WorkFuncWithResult func(ctx context.Context) interface{}

// WorkResult represents work execution result
type WorkResult struct {
	// Result work result
	Result interface{}

	// Error work error if any
	Error error

	// Duration work execution duration
	Duration time.Duration
}

// CacheKey represents cache key
type CacheKey struct {
	// Namespace cache namespace
	Namespace string

	// Key cache key
	Key string

	// Version cache version (for invalidation)
	Version string
}

// Supporting types and enums
type (
	ScanType        string
	ScanScope       string
	CircuitState    string
	HealthStatus    string
	DiscoveryFilter interface{}
	ScanFilter      interface{}
	CheckResult     struct {
		Name    string
		Passed  bool
		Message string
		Data    map[string]interface{}
	}
	ValidationSummary struct {
		Total   int
		Valid   int
		Invalid int
		Errors  int
		AvgTime time.Duration
	}
	ScannerCapabilities struct {
		SupportedTypes    []string
		SupportedScopes   []string
		MaxConcurrency    int
		SupportsStreaming bool
	}
	ScanResult struct {
		Services  []ServiceInfo
		Error     error
		Timestamp time.Time
		Source    string
	}
	CacheStats struct {
		HitRate     float64
		MissRate    float64
		Size        int64
		Entries     int
		Evictions   int64
		LastCleanup time.Time
	}
	PoolStats struct {
		ActiveWorkers    int
		QueuedTasks      int
		CompletedTasks   int64
		FailedTasks      int64
		AvgTaskTime      time.Duration
		ThroughputPerSec float64
	}
)

// Constants for scan types
const (
	ScanTypeKubernetes ScanType = "kubernetes"
	ScanTypeDocker     ScanType = "docker"
	ScanTypeProcess    ScanType = "process"
	ScanTypeNetwork    ScanType = "network"
	ScanTypeLocal      ScanType = "local"
)

// Constants for scan scopes
const (
	ScopeCluster   ScanScope = "cluster"
	ScopeNamespace ScanScope = "namespace"
	ScopeNode      ScanScope = "node"
	ScopePod       ScanScope = "pod"
	ScopeHost      ScanScope = "host"
)

// Constants for circuit states
const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half-open"
)

// Constants for health status
const (
	HealthHealthy   HealthStatus = "healthy"
	HealthUnhealthy HealthStatus = "unhealthy"
	HealthUnknown   HealthStatus = "unknown"
	HealthDegraded  HealthStatus = "degraded"
)
