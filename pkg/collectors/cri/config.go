package cri

import "time"

// Config holds simple CRI collector configuration
type Config struct {
	Name         string        `json:"name" yaml:"name"`
	SocketPath   string        `json:"socket_path" yaml:"socket_path"`
	BufferSize   int           `json:"buffer_size" yaml:"buffer_size"`
	PollInterval time.Duration `json:"poll_interval" yaml:"poll_interval"`
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig(name string) *Config {
	return &Config{
		Name:         name,
		SocketPath:   "", // Auto-detect
		BufferSize:   10000,
		PollInterval: 5 * time.Second,
	}
}
