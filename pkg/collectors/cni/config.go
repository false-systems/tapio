package cni

import "fmt"

// Config holds configuration for CNI collector
type Config struct {
	// Buffer size for events channel
	BufferSize int

	// Enable eBPF monitoring
	EnableEBPF bool
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be greater than 0")
	}
	if c.BufferSize > 1000000 {
		return fmt.Errorf("buffer size must not exceed 1,000,000")
	}
	return nil
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		BufferSize: 10000,
		EnableEBPF: true,
	}
}
