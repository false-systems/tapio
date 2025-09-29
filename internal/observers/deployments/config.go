package deployments

import (
	"fmt"
	"time"
)

// Config holds configuration for the deployments observer
type Config struct {
	// Observer configuration
	Name       string `json:"name"`
	BufferSize int    `json:"buffer_size"`

	// Kubernetes client configuration
	KubeConfig string `json:"kubeconfig,omitempty"`
	MockMode   bool   `json:"mock_mode,omitempty"`

	// Informer configuration
	ResyncPeriod time.Duration `json:"resync_period"`

	// What to track
	TrackConfigMaps bool `json:"track_configmaps"`
	TrackSecrets    bool `json:"track_secrets"`

	// Filtering
	Namespaces              []string      `json:"namespaces,omitempty"`
	AnnotationFilter        string        `json:"annotation_filter,omitempty"`
	IgnoreSystemDeployments bool          `json:"ignore_system_deployments"`
	DeduplicationWindow     time.Duration `json:"deduplication_window"`
}

// DefaultConfig returns sensible defaults for the deployments observer
func DefaultConfig() *Config {
	return &Config{
		Name:                    "deployments",
		BufferSize:              1000,
		ResyncPeriod:            30 * time.Second,
		TrackConfigMaps:         true,
		TrackSecrets:            true,
		IgnoreSystemDeployments: true,
		DeduplicationWindow:     5 * time.Minute,
		Namespaces:              []string{}, // Empty means all namespaces
		AnnotationFilter:        "",         // Empty means no filter
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be positive, got: %d", c.BufferSize)
	}

	if c.ResyncPeriod <= 0 {
		return fmt.Errorf("resync_period must be positive, got: %s", c.ResyncPeriod)
	}

	if c.DeduplicationWindow <= 0 {
		return fmt.Errorf("deduplication_window must be positive, got: %s", c.DeduplicationWindow)
	}

	if c.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}

	return nil
}
