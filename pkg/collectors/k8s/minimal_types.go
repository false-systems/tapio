package k8s

import "time"

// MinimalRawEvent represents a raw event from K8s
type MinimalRawEvent struct {
	Timestamp time.Time
	Type      string
	Data      []byte
	Metadata  map[string]string
}

// MinimalConfig represents configuration for the minimal collector
type MinimalConfig struct {
	BufferSize int
	Labels     map[string]string
}

// DefaultMinimalConfig returns default configuration
func DefaultMinimalConfig() MinimalConfig {
	return MinimalConfig{
		BufferSize: 1000,
		Labels:     make(map[string]string),
	}
}
