package services

import (
	"fmt"
	"time"
)

// Config holds configuration for the services observer
type Config struct {
	// connection tracking: Connection tracking config
	ConnectionTableSize int           `json:"connection_table_size"`
	ConnectionTimeout   time.Duration `json:"connection_timeout"`
	BufferSize          int           `json:"buffer_size"`
	CleanupInterval     time.Duration `json:"cleanup_interval"`

	// K8s enrichment: K8s context config
	EnableK8sMapping   bool          `json:"enable_k8s_mapping"`
	K8sRefreshInterval time.Duration `json:"k8s_refresh_interval"`
	PodMappingTimeout  time.Duration `json:"pod_mapping_timeout"`

	// General observer config
	Name        string `json:"name"`
	HealthCheck bool   `json:"health_check"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		// connection tracking defaults
		ConnectionTableSize: 10000,
		ConnectionTimeout:   5 * time.Minute,
		BufferSize:          1000,
		CleanupInterval:     30 * time.Second,

		// K8s enrichment defaults
		EnableK8sMapping:   true,
		K8sRefreshInterval: 30 * time.Second,
		PodMappingTimeout:  10 * time.Second,

		// General defaults
		Name:        "services",
		HealthCheck: true,
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.ConnectionTableSize <= 0 {
		return fmt.Errorf("connection_table_size must be positive")
	}
	if c.ConnectionTimeout <= 0 {
		return fmt.Errorf("connection_timeout must be positive")
	}
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be positive")
	}
	if c.CleanupInterval <= 0 {
		return fmt.Errorf("cleanup_interval must be positive")
	}

	if c.EnableK8sMapping {
		if c.K8sRefreshInterval <= 0 {
			return fmt.Errorf("k8s_refresh_interval must be positive when K8s mapping enabled")
		}
		if c.PodMappingTimeout <= 0 {
			return fmt.Errorf("pod_mapping_timeout must be positive when K8s mapping enabled")
		}
	}

	return nil
}

// GetEnabledLevels returns which levels are enabled
func (c *Config) GetEnabledLevels() []int {
	levels := []int{1} // connection tracking always enabled

	if c.EnableK8sMapping {
		levels = append(levels, 2)
	}

	return levels
}
