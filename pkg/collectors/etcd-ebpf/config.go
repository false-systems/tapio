//go:build linux

package etcdebpf

import "fmt"

// Config holds configuration for etcd eBPF collector
type Config struct {
	// Buffer size for events channel
	BufferSize int `json:"buffer_size"`

	// Process discovery interval in seconds
	ProcessDiscoveryInterval int `json:"process_discovery_interval"`

	// PID verification timeout in seconds (prevents stale PIDs)
	PIDVerificationTimeout int `json:"pid_verification_timeout"`

	// Enable detailed syscall data capture
	CaptureDataPayload bool `json:"capture_data_payload"`

	// Maximum data payload size to capture (bytes)
	MaxDataCaptureSize int `json:"max_data_capture_size"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be greater than 0")
	}
	if c.BufferSize > 1000000 {
		return fmt.Errorf("buffer size must not exceed 1,000,000")
	}

	if c.ProcessDiscoveryInterval <= 0 {
		c.ProcessDiscoveryInterval = 30 // Default 30 seconds
	}
	if c.ProcessDiscoveryInterval > 3600 {
		return fmt.Errorf("process discovery interval must not exceed 3600 seconds")
	}

	if c.PIDVerificationTimeout <= 0 {
		c.PIDVerificationTimeout = 300 // Default 5 minutes
	}
	if c.PIDVerificationTimeout > 86400 {
		return fmt.Errorf("PID verification timeout must not exceed 86400 seconds (24 hours)")
	}

	if c.MaxDataCaptureSize <= 0 {
		c.MaxDataCaptureSize = 256 // Default 256 bytes
	}
	if c.MaxDataCaptureSize > 4096 {
		return fmt.Errorf("max data capture size must not exceed 4096 bytes")
	}

	return nil
}

// DefaultConfig returns default configuration for eBPF monitoring
func DefaultConfig() Config {
	return Config{
		BufferSize:               10000,
		ProcessDiscoveryInterval: 30,
		PIDVerificationTimeout:   300,
		CaptureDataPayload:       false, // Disabled by default for performance
		MaxDataCaptureSize:       256,
	}
}
