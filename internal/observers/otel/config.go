package otel

import "time"

// Config holds OTEL observer configuration
type Config struct {
	// Basic settings
	Name       string
	BufferSize int

	// Sampling configuration
	SamplingRate       float64 // 0.0 to 1.0 (1.0 = 100%)
	AlwaysSampleErrors bool    // Keep all error traces

	// Processing settings
	ServiceMapInterval  time.Duration // How often to emit service dependency events
	EnableDependencies  bool          // Extract service dependencies from spans
	EnableResourceAttrs bool          // Include K8s resource attributes
}

// DefaultConfig returns sensible defaults for OTEL observer
func DefaultConfig() *Config {
	return &Config{
		Name:                "otel-observer",
		BufferSize:          10000,
		SamplingRate:        1.0, // Keep all traces initially
		AlwaysSampleErrors:  true,
		ServiceMapInterval:  30 * time.Second,
		EnableDependencies:  true,
		EnableResourceAttrs: true,
	}
}

// Validate ensures configuration is valid
func (c *Config) Validate() error {
	if c.SamplingRate < 0 || c.SamplingRate > 1 {
		c.SamplingRate = 1.0
	}
	if c.BufferSize <= 0 {
		c.BufferSize = 10000
	}
	if c.ServiceMapInterval <= 0 {
		c.ServiceMapInterval = 30 * time.Second
	}
	return nil
}
