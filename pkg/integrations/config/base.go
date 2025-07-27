package config

import (
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// BaseConfig provides common configuration fields for all integrations
type BaseConfig struct {
	// Integration metadata
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type" json:"type"`
	Version     string `yaml:"version" json:"version"`
	Environment string `yaml:"environment" json:"environment"`

	// Common operational settings
	Enabled bool          `yaml:"enabled" json:"enabled"`
	Timeout time.Duration `yaml:"timeout" json:"timeout"`

	// Retry configuration
	Retry RetryConfig `yaml:"retry" json:"retry"`

	// Observability
	Observability ObservabilityConfig `yaml:"observability" json:"observability"`

	// Resource limits
	Limits ResourceLimits `yaml:"limits" json:"limits"`

	// Labels and metadata
	Labels   map[string]string `yaml:"labels" json:"labels"`
	Metadata map[string]string `yaml:"metadata" json:"metadata"`
}

// RetryConfig defines common retry settings
type RetryConfig struct {
	Enabled     bool          `yaml:"enabled" json:"enabled"`
	MaxAttempts int           `yaml:"max_attempts" json:"max_attempts"`
	InitialWait time.Duration `yaml:"initial_wait" json:"initial_wait"`
	MaxWait     time.Duration `yaml:"max_wait" json:"max_wait"`
	Multiplier  float64       `yaml:"multiplier" json:"multiplier"`
}

// ObservabilityConfig defines observability settings
type ObservabilityConfig struct {
	// Tracing
	TracingEnabled  bool                 `yaml:"tracing_enabled" json:"tracing_enabled"`
	TracingSampling float64              `yaml:"tracing_sampling" json:"tracing_sampling"`
	TracingEndpoint string               `yaml:"tracing_endpoint" json:"tracing_endpoint"`
	TracingHeaders  map[string]string    `yaml:"tracing_headers" json:"tracing_headers"`
	ResourceAttrs   []attribute.KeyValue `yaml:"-" json:"-"`

	// Metrics
	MetricsEnabled  bool          `yaml:"metrics_enabled" json:"metrics_enabled"`
	MetricsInterval time.Duration `yaml:"metrics_interval" json:"metrics_interval"`
	MetricsEndpoint string        `yaml:"metrics_endpoint" json:"metrics_endpoint"`

	// Logging
	LogLevel      string   `yaml:"log_level" json:"log_level"`
	LogFormat     string   `yaml:"log_format" json:"log_format"`
	LogSampling   bool     `yaml:"log_sampling" json:"log_sampling"`
	LogRateLimit  int      `yaml:"log_rate_limit" json:"log_rate_limit"`
	SensitiveKeys []string `yaml:"sensitive_keys" json:"sensitive_keys"`
}

// ResourceLimits defines resource usage limits
type ResourceLimits struct {
	MaxConnections    int           `yaml:"max_connections" json:"max_connections"`
	MaxConcurrency    int           `yaml:"max_concurrency" json:"max_concurrency"`
	MaxMemoryMB       int           `yaml:"max_memory_mb" json:"max_memory_mb"`
	MaxCPUPercent     float64       `yaml:"max_cpu_percent" json:"max_cpu_percent"`
	MaxRequestsPerSec int           `yaml:"max_requests_per_sec" json:"max_requests_per_sec"`
	ConnectionTimeout time.Duration `yaml:"connection_timeout" json:"connection_timeout"`
	IdleTimeout       time.Duration `yaml:"idle_timeout" json:"idle_timeout"`
	ShutdownTimeout   time.Duration `yaml:"shutdown_timeout" json:"shutdown_timeout"`
}

// Integration is the base interface all integrations must implement
type Integration interface {
	// Lifecycle
	Start() error
	Stop() error
	Reload(config interface{}) error

	// Health and status
	Health() HealthStatus
	Statistics() Statistics

	// Configuration
	GetConfig() interface{}
	ValidateConfig() error
}

// HealthStatus represents the health of an integration
type HealthStatus struct {
	Healthy   bool                   `json:"healthy"`
	Status    string                 `json:"status"`
	Message   string                 `json:"message,omitempty"`
	LastCheck time.Time              `json:"last_check"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// Statistics represents runtime statistics
type Statistics struct {
	StartTime      time.Time              `json:"start_time"`
	Uptime         time.Duration          `json:"uptime"`
	ProcessedCount uint64                 `json:"processed_count"`
	ErrorCount     uint64                 `json:"error_count"`
	LastActivity   time.Time              `json:"last_activity"`
	Custom         map[string]interface{} `json:"custom,omitempty"`
}

// DefaultBaseConfig returns a default base configuration
func DefaultBaseConfig() BaseConfig {
	return BaseConfig{
		Enabled: true,
		Timeout: 30 * time.Second,
		Retry: RetryConfig{
			Enabled:     true,
			MaxAttempts: 3,
			InitialWait: 100 * time.Millisecond,
			MaxWait:     10 * time.Second,
			Multiplier:  2.0,
		},
		Observability: ObservabilityConfig{
			TracingEnabled:  true,
			TracingSampling: 0.1,
			MetricsEnabled:  true,
			MetricsInterval: 60 * time.Second,
			LogLevel:        "info",
			LogFormat:       "json",
			LogRateLimit:    100,
		},
		Limits: ResourceLimits{
			MaxConnections:    100,
			MaxConcurrency:    10,
			MaxMemoryMB:       512,
			MaxCPUPercent:     80.0,
			MaxRequestsPerSec: 1000,
			ConnectionTimeout: 10 * time.Second,
			IdleTimeout:       60 * time.Second,
			ShutdownTimeout:   30 * time.Second,
		},
		Labels:   make(map[string]string),
		Metadata: make(map[string]string),
	}
}

// MergeWithBase merges a specific config with base config
func MergeWithBase(base BaseConfig, specific interface{}) interface{} {
	// This will be implemented using reflection to merge configs
	// For now, integrations will embed BaseConfig directly
	return specific
}
