//go:build linux

package criebpf

import (
	"fmt"
	"time"
)

// Config holds CRI eBPF collector configuration
type Config struct {
	Name                 string `json:"name" yaml:"name"`
	BufferSize           int    `json:"buffer_size" yaml:"buffer_size"`
	EnableOOMKill        bool   `json:"enable_oom_kill" yaml:"enable_oom_kill"`
	EnableMemoryPressure bool   `json:"enable_memory_pressure" yaml:"enable_memory_pressure"`
	EnableProcessExit    bool   `json:"enable_process_exit" yaml:"enable_process_exit"`
	EnableProcessFork    bool   `json:"enable_process_fork" yaml:"enable_process_fork"`

	// eBPF-specific configuration
	BPFProgramPinPath string        `json:"bpf_program_pin_path" yaml:"bpf_program_pin_path"`
	BPFLogLevel       int           `json:"bpf_log_level" yaml:"bpf_log_level"`
	MetricsInterval   time.Duration `json:"metrics_interval" yaml:"metrics_interval"`

	// Container metadata cache configuration
	MetadataCacheSize int           `json:"metadata_cache_size" yaml:"metadata_cache_size"`
	MetadataCacheTTL  time.Duration `json:"metadata_cache_ttl" yaml:"metadata_cache_ttl"`

	// Performance tuning
	RingBufferSize   int  `json:"ring_buffer_size" yaml:"ring_buffer_size"`
	WakeupEvents     int  `json:"wakeup_events" yaml:"wakeup_events"`
	DisableJIT       bool `json:"disable_jit" yaml:"disable_jit"`
	VerifierLogLevel int  `json:"verifier_log_level" yaml:"verifier_log_level"`
}

// NewDefaultConfig returns default configuration for CRI eBPF collector
func NewDefaultConfig(name string) *Config {
	return &Config{
		Name:                 name,
		BufferSize:           10000,
		EnableOOMKill:        true,
		EnableMemoryPressure: true,
		EnableProcessExit:    true,
		EnableProcessFork:    false, // Can be noisy

		// eBPF defaults
		BPFProgramPinPath: "/sys/fs/bpf/tapio/cri-ebpf",
		BPFLogLevel:       0, // 0=disabled, 1=info, 2=debug
		MetricsInterval:   30 * time.Second,

		// Cache defaults
		MetadataCacheSize: 10000,
		MetadataCacheTTL:  5 * time.Minute,

		// Performance defaults (256KB ring buffer)
		RingBufferSize:   256 * 1024,
		WakeupEvents:     64, // Wake userspace every 64 events
		DisableJIT:       false,
		VerifierLogLevel: 0,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("collector name is required")
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
