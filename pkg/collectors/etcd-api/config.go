package etcdapi

import "fmt"

// TLSConfig holds TLS configuration for etcd API connections
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

// Config holds configuration for etcd API collector
type Config struct {
	// Buffer size for events channel
	BufferSize int `json:"buffer_size"`

	// etcd endpoints for API monitoring
	Endpoints []string `json:"endpoints"`

	// Authentication
	Username string `json:"username"`
	Password string `json:"password"`

	// TLS configuration
	TLS *TLSConfig `json:"tls"`

	// Watch prefix - defaults to "/registry/" for K8s
	WatchPrefix string `json:"watch_prefix"`

	// Connection timeout in seconds
	DialTimeout int `json:"dial_timeout"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be greater than 0")
	}
	if c.BufferSize > 1000000 {
		return fmt.Errorf("buffer size must not exceed 1,000,000")
	}

	if len(c.Endpoints) == 0 {
		return fmt.Errorf("at least one etcd endpoint must be specified")
	}

	// Validate TLS configuration if present
	if c.TLS != nil {
		if err := c.TLS.Validate(); err != nil {
			return fmt.Errorf("TLS config validation failed: %w", err)
		}
	}

	if c.DialTimeout <= 0 {
		c.DialTimeout = 5 // Default 5 seconds
	}

	if c.WatchPrefix == "" {
		c.WatchPrefix = "/registry/" // Default to K8s registry
	}

	return nil
}

// DefaultConfig returns default configuration for API monitoring
func DefaultConfig() Config {
	return Config{
		BufferSize:  10000,
		Endpoints:   []string{"localhost:2379"},
		WatchPrefix: "/registry/",
		DialTimeout: 5,
	}
}
