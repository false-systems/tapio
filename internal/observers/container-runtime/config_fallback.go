//go:build !linux
// +build !linux

package containerruntime

import (
	"fmt"
	"time"
)

// NewDefaultConfig returns a default configuration
func NewDefaultConfig(name string) *Config {
	return &Config{
		Name:                 name,
		BufferSize:           10000,
		EnableOOMKill:        true,
		EnableMemoryPressure: true,
		EnableProcessExit:    true,
		EnableProcessFork:    false, // Disabled by default for performance
		BPFProgramPinPath:    "/sys/fs/bpf",
		BPFLogLevel:          0,
		MetricsInterval:      30 * time.Second,
		MetadataCacheSize:    1000,
		MetadataCacheTTL:     5 * time.Minute,
		RingBufferSize:       65536, // 64KB default
		WakeupEvents:         1,
		EnableSymbolCache:    false,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("observer name is required")
	}

	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be greater than 0")
	}

	if c.RingBufferSize <= 0 {
		return fmt.Errorf("ring_buffer_size must be greater than 0")
	}

	if c.MetadataCacheSize <= 0 {
		return fmt.Errorf("metadata_cache_size must be greater than 0")
	}

	if c.MetricsInterval <= 0 {
		return fmt.Errorf("metrics_interval must be greater than 0")
	}

	return nil
}
