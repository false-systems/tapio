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

	// OTLP exporter configuration
	OTLP OTLPConfig
}

// OTLPConfig configures OTLP/gRPC trace export
type OTLPConfig struct {
	// Enabled controls whether OTLP export is active
	Enabled bool

	// Endpoint is the gRPC endpoint (e.g., "localhost:4317")
	Endpoint string

	// Headers are additional gRPC headers (e.g., authentication)
	Headers map[string]string

	// Timeout for export operations
	Timeout time.Duration

	// Insecure disables TLS (for local development)
	Insecure bool
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
		OTLP: OTLPConfig{
			Enabled:  false,
			Endpoint: "localhost:4317",
			Headers:  make(map[string]string),
			Timeout:  10 * time.Second,
			Insecure: true, // Default to insecure for local dev
		},
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

	// Validate OTLP config
	if c.OTLP.Enabled {
		if c.OTLP.Endpoint == "" {
			c.OTLP.Endpoint = "localhost:4317"
		}
		if c.OTLP.Timeout <= 0 {
			c.OTLP.Timeout = 10 * time.Second
		}
		if c.OTLP.Headers == nil {
			c.OTLP.Headers = make(map[string]string)
		}
	}

	return nil
}
