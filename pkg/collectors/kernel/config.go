package kernel

import "time"

// eBPFBufferConfig defines eBPF ring buffer configuration
type eBPFBufferConfig struct {
	// Ring buffer sizes in KB
	KernelEventsBuffer    int `json:"kernel_events_buffer" yaml:"kernel_events_buffer"`
	ProcessEventsBuffer   int `json:"process_events_buffer" yaml:"process_events_buffer"`
	NetworkEventsBuffer   int `json:"network_events_buffer" yaml:"network_events_buffer"`
	SecurityEventsBuffer  int `json:"security_events_buffer" yaml:"security_events_buffer"`
}

// ResourceLimits defines resource usage limits
type ResourceLimits struct {
	MaxMemoryMB      int           `json:"max_memory_mb" yaml:"max_memory_mb"`
	MaxCPUPercent    int           `json:"max_cpu_percent" yaml:"max_cpu_percent"`
	EventQueueSize   int           `json:"event_queue_size" yaml:"event_queue_size"`
	BatchTimeout     time.Duration `json:"batch_timeout" yaml:"batch_timeout"`
	MaxEventsPerSec  int           `json:"max_events_per_sec" yaml:"max_events_per_sec"`
}

// BackpressureConfig defines backpressure handling
type BackpressureConfig struct {
	Enabled              bool          `json:"enabled" yaml:"enabled"`
	HighWatermark        float64       `json:"high_watermark" yaml:"high_watermark"`
	LowWatermark         float64       `json:"low_watermark" yaml:"low_watermark"`
	DropThreshold        float64       `json:"drop_threshold" yaml:"drop_threshold"`
	RecoveryDelay        time.Duration `json:"recovery_delay" yaml:"recovery_delay"`
	SamplingReduction    float64       `json:"sampling_reduction" yaml:"sampling_reduction"`
}

// HealthConfig defines health check configuration
type HealthConfig struct {
	Enabled             bool          `json:"enabled" yaml:"enabled"`
	Interval            time.Duration `json:"interval" yaml:"interval"`
	MaxFailures         int           `json:"max_failures" yaml:"max_failures"`
	RestartOnFailure    bool          `json:"restart_on_failure" yaml:"restart_on_failure"`
	MemoryCheckInterval time.Duration `json:"memory_check_interval" yaml:"memory_check_interval"`
}

// Config holds kernel collector configuration
type Config struct {
	Name            string               `json:"name" yaml:"name"`
	Enabled         bool                 `json:"enabled" yaml:"enabled"`
	BufferConfig    eBPFBufferConfig     `json:"buffer_config" yaml:"buffer_config"`
	ResourceLimits  ResourceLimits       `json:"resource_limits" yaml:"resource_limits"`
	Backpressure    BackpressureConfig   `json:"backpressure" yaml:"backpressure"`
	Health          HealthConfig         `json:"health" yaml:"health"`
	SamplingEnabled bool                 `json:"sampling_enabled" yaml:"sampling_enabled"`
	SamplingRate    int                  `json:"sampling_rate" yaml:"sampling_rate"`
	DebugMode       bool                 `json:"debug_mode" yaml:"debug_mode"`
}

// DefaultConfig returns production-ready default configuration
func DefaultConfig() *Config {
	return &Config{
		Name:    "kernel-collector",
		Enabled: true,
		BufferConfig: eBPFBufferConfig{
			KernelEventsBuffer:   512, // 512KB - production optimized
			ProcessEventsBuffer:  256, // 256KB - production optimized
			NetworkEventsBuffer:  512, // 512KB - production optimized
			SecurityEventsBuffer: 256, // 256KB - production optimized
		},
		ResourceLimits: ResourceLimits{
			MaxMemoryMB:     100,              // 100MB memory limit
			MaxCPUPercent:   25,               // 25% CPU limit
			EventQueueSize:  10000,            // 10K events in queue
			BatchTimeout:    100 * time.Millisecond,
			MaxEventsPerSec: 10000,            // 10K events/sec max
		},
		Backpressure: BackpressureConfig{
			Enabled:           true,
			HighWatermark:     0.8,            // 80% buffer usage
			LowWatermark:      0.6,            // 60% buffer usage
			DropThreshold:     0.95,           // 95% - start dropping
			RecoveryDelay:     5 * time.Second,
			SamplingReduction: 0.5,            // Reduce to 50% sampling
		},
		Health: HealthConfig{
			Enabled:             true,
			Interval:            30 * time.Second,
			MaxFailures:         3,
			RestartOnFailure:    true,
			MemoryCheckInterval: 10 * time.Second,
		},
		SamplingEnabled: true,
		SamplingRate:    100, // Sample 1 in 100 events
		DebugMode:       false,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferConfig.KernelEventsBuffer <= 0 {
		c.BufferConfig.KernelEventsBuffer = 512
	}
	if c.BufferConfig.ProcessEventsBuffer <= 0 {
		c.BufferConfig.ProcessEventsBuffer = 256
	}
	if c.BufferConfig.NetworkEventsBuffer <= 0 {
		c.BufferConfig.NetworkEventsBuffer = 512
	}
	if c.BufferConfig.SecurityEventsBuffer <= 0 {
		c.BufferConfig.SecurityEventsBuffer = 256
	}

	if c.ResourceLimits.MaxMemoryMB <= 0 {
		c.ResourceLimits.MaxMemoryMB = 100
	}
	if c.ResourceLimits.MaxCPUPercent <= 0 {
		c.ResourceLimits.MaxCPUPercent = 25
	}
	if c.ResourceLimits.EventQueueSize <= 0 {
		c.ResourceLimits.EventQueueSize = 10000
	}

	return nil
}

// GetBufferSize returns the buffer size in bytes for the given buffer type
func (c *Config) GetBufferSize(bufferType string) int {
	switch bufferType {
	case "kernel":
		return c.BufferConfig.KernelEventsBuffer * 1024
	case "process":
		return c.BufferConfig.ProcessEventsBuffer * 1024
	case "network":
		return c.BufferConfig.NetworkEventsBuffer * 1024
	case "security":
		return c.BufferConfig.SecurityEventsBuffer * 1024
	default:
		return 256 * 1024 // Default 256KB
	}
}