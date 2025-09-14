package loader

import (
	"time"

	"github.com/yairfalse/tapio/internal/integrations/neo4j"
	"github.com/yairfalse/tapio/pkg/config"
)

// Config holds configuration for the Neo4j loader service
type Config struct {
	// NATS configuration
	NATS *config.NATSConfig `json:"nats"`

	// Neo4j configuration
	Neo4j neo4j.Config `json:"neo4j"`

	// Processing configuration
	BatchSize       int           `json:"batch_size"`
	BatchTimeout    time.Duration `json:"batch_timeout"`
	MaxConcurrency  int           `json:"max_concurrency"`
	ProcessTimeout  time.Duration `json:"process_timeout"`
	ShutdownTimeout time.Duration `json:"shutdown_timeout"`

	// Health check configuration
	HealthCheckInterval time.Duration `json:"health_check_interval"`

	// Retry configuration
	MaxRetries      int           `json:"max_retries"`
	RetryBackoff    time.Duration `json:"retry_backoff"`
	MaxRetryBackoff time.Duration `json:"max_retry_backoff"`
}

// DefaultConfig returns default configuration for the loader
func DefaultConfig() *Config {
	return &Config{
		NATS: config.DefaultNATSConfig(),
		Neo4j: neo4j.Config{
			URI:      "neo4j://localhost:7687",
			Username: "neo4j",
			Password: "password",
			Database: "neo4j",
		},

		BatchSize:       100,
		BatchTimeout:    5 * time.Second,
		MaxConcurrency:  4,
		ProcessTimeout:  30 * time.Second,
		ShutdownTimeout: 30 * time.Second,

		HealthCheckInterval: 30 * time.Second,

		MaxRetries:      3,
		RetryBackoff:    1 * time.Second,
		MaxRetryBackoff: 30 * time.Second,
	}
}

// Validate ensures the configuration is valid
func (c *Config) Validate() error {
	if c.NATS == nil {
		return NewValidationError("NATS", nil, "configuration is required")
	}

	if c.Neo4j.URI == "" {
		return NewValidationError("Neo4j.URI", c.Neo4j.URI, "cannot be empty")
	}

	if c.BatchSize <= 0 {
		return NewValidationError("BatchSize", c.BatchSize, "must be positive")
	}

	if c.BatchTimeout <= 0 {
		return NewValidationError("BatchTimeout", c.BatchTimeout, "must be positive")
	}

	if c.MaxConcurrency <= 0 {
		return NewValidationError("MaxConcurrency", c.MaxConcurrency, "must be positive")
	}

	if c.ProcessTimeout <= 0 {
		return NewValidationError("ProcessTimeout", c.ProcessTimeout, "must be positive")
	}

	if c.ShutdownTimeout <= 0 {
		return NewValidationError("ShutdownTimeout", c.ShutdownTimeout, "must be positive")
	}

	return nil
}

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Value   interface{}
	Rule    string
	wrapped error
}

// NewValidationError creates a new validation error
func NewValidationError(field string, value interface{}, rule string) *ValidationError {
	return &ValidationError{
		Field: field,
		Value: value,
		Rule:  rule,
	}
}

func (e *ValidationError) Error() string {
	return "validation failed for field " + e.Field + ": " + e.Rule
}

func (e *ValidationError) Unwrap() error {
	return e.wrapped
}
