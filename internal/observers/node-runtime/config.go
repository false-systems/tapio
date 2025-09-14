package noderuntime

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

// Config holds configuration for node-runtime observer
type Config struct {
	// Node name to collect from (defaults to current node)
	NodeName string

	// Kubelet address (defaults to localhost:10250)
	Address string

	// Use insecure connection (for testing)
	Insecure bool

	// Client certificate for authentication
	ClientCert string
	ClientKey  string

	// Collection intervals
	MetricsInterval time.Duration
	StatsInterval   time.Duration

	// Logger (will be set by observer if nil)
	Logger *zap.Logger

	// Additional settings
	RequestTimeout time.Duration
	MaxRetries     int
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Basic validation
	if c.Address == "" {
		return fmt.Errorf("node-runtime address cannot be empty")
	}

	// Interval validation
	if c.MetricsInterval < 5*time.Second {
		return fmt.Errorf("metrics interval must be at least 5 seconds")
	}
	if c.StatsInterval < 5*time.Second {
		return fmt.Errorf("stats interval must be at least 5 seconds")
	}

	// Timeout validation
	if c.RequestTimeout > 0 && c.RequestTimeout < time.Second {
		return fmt.Errorf("request timeout must be at least 1 second")
	}

	// Retry validation
	if c.MaxRetries < 0 {
		return fmt.Errorf("max retries cannot be negative")
	}
	if c.MaxRetries > 10 {
		return fmt.Errorf("max retries must not exceed 10")
	}

	return nil
}

// DefaultConfig returns default configuration for node-runtime observer
func DefaultConfig() *Config {
	return &Config{
		Address:         "localhost:10250",
		MetricsInterval: 30 * time.Second,
		StatsInterval:   10 * time.Second,
		Insecure:        false,
		RequestTimeout:  10 * time.Second,
		MaxRetries:      3,
	}
}

// ProductionConfig returns a production-ready configuration
func ProductionConfig() *Config {
	config := DefaultConfig()

	// Production-specific settings
	config.Insecure = false
	config.RequestTimeout = 5 * time.Second
	config.MaxRetries = 2
	config.MetricsInterval = 60 * time.Second // Less frequent in production

	return config
}

// DevelopmentConfig returns a development-friendly configuration
func DevelopmentConfig() *Config {
	config := DefaultConfig()

	// Development-specific settings
	config.Insecure = true                    // More permissive for dev
	config.MetricsInterval = 10 * time.Second // More frequent for debugging
	config.StatsInterval = 5 * time.Second
	config.RequestTimeout = 30 * time.Second // Longer timeout for debugging

	return config
}
