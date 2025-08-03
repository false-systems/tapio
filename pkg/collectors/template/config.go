package template

import (
	"fmt"
	"time"
)

// Config holds collector configuration
type Config struct {
	// Buffer size for events channel
	BufferSize int

	// Number of worker goroutines
	Workers int

	// How often to poll for events
	PollInterval time.Duration

	// Collector-specific settings go here
	// Example: Endpoints, Filters, etc.
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		BufferSize:   10000,
		Workers:      2,
		PollInterval: 5 * time.Second,
	}
}

// Validate checks if the configuration is valid
func (c Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be positive")
	}

	if c.Workers <= 0 {
		return fmt.Errorf("workers must be positive")
	}

	if c.PollInterval <= 0 {
		return fmt.Errorf("poll interval must be positive")
	}

	return nil
}
