package otel

import "time"

// Config holds OTEL collector configuration
type Config struct {
	// Basic settings
	Name         string
	GRPCEndpoint string // Default :4317
	HTTPEndpoint string // Default :4318
	BufferSize   int
	MaxBatchSize int

	// Sampling configuration
	SamplingRate       float64 // 0.0 to 1.0 (1.0 = 100%)
	AlwaysSampleErrors bool    // Keep all error traces
	MaxTracesPerSecond int     // Rate limiting

	// Processing settings
	ProcessingTimeout   time.Duration
	ServiceMapInterval  time.Duration // How often to emit service dependency events
	EnableDependencies  bool          // Extract service dependencies from spans
	EnableResourceAttrs bool          // Include K8s resource attributes
}

// DefaultConfig returns sensible defaults for OTEL collector
func DefaultConfig() *Config {
	return &Config{
		Name:                "otel-collector",
		GRPCEndpoint:        ":4317",
		HTTPEndpoint:        ":4318",
		BufferSize:          10000,
		MaxBatchSize:        100,
		SamplingRate:        1.0, // Keep all traces initially
		AlwaysSampleErrors:  true,
		MaxTracesPerSecond:  1000,
		ProcessingTimeout:   5 * time.Second,
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
	if c.MaxTracesPerSecond <= 0 {
		c.MaxTracesPerSecond = 1000
	}
	return nil
}
