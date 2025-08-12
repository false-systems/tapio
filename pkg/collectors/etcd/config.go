package etcd

import "fmt"

// TLSConfig holds TLS configuration
type TLSConfig struct {
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	CAFile   string `json:"ca_file"`
}

// Validate validates TLS configuration
func (t *TLSConfig) Validate() error {
	if t.CertFile != "" && t.KeyFile == "" {
		return fmt.Errorf("cert file specified but key file is missing")
	}
	if t.KeyFile != "" && t.CertFile == "" {
		return fmt.Errorf("key file specified but cert file is missing")
	}
	return nil
}

// Config holds configuration for etcd collector
type Config struct {
	// Buffer size for events channel
	BufferSize int

	// Enable eBPF monitoring
	EnableEBPF bool

	// etcd endpoints for API monitoring
	Endpoints []string `json:"endpoints"`

	// Authentication
	Username string `json:"username"`
	Password string `json:"password"`

	// TLS configuration
	TLS *TLSConfig `json:"tls"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be greater than 0")
	}
	if c.BufferSize > 1000000 {
		return fmt.Errorf("buffer size must not exceed 1,000,000")
	}

	// Validate TLS configuration if present
	if c.TLS != nil {
		if err := c.TLS.Validate(); err != nil {
			return fmt.Errorf("TLS config validation failed: %w", err)
		}
	}

	return nil
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		BufferSize: 10000,
		EnableEBPF: true,
		Endpoints:  []string{}, // No API monitoring by default
	}
}
