package kernel

import (
	"fmt"
	"time"
)

// eBPFBufferConfig defines eBPF ring buffer configuration
type eBPFBufferConfig struct {
	// Ring buffer sizes in KB
	KernelEventsBuffer   int `json:"kernel_events_buffer" yaml:"kernel_events_buffer"`
	ProcessEventsBuffer  int `json:"process_events_buffer" yaml:"process_events_buffer"`
	NetworkEventsBuffer  int `json:"network_events_buffer" yaml:"network_events_buffer"`
	SecurityEventsBuffer int `json:"security_events_buffer" yaml:"security_events_buffer"`
}

// ResourceLimits defines resource usage limits
type ResourceLimits struct {
	MaxMemoryMB     int           `json:"max_memory_mb" yaml:"max_memory_mb"`
	MaxCPUPercent   int           `json:"max_cpu_percent" yaml:"max_cpu_percent"`
	EventQueueSize  int           `json:"event_queue_size" yaml:"event_queue_size"`
	BatchTimeout    time.Duration `json:"batch_timeout" yaml:"batch_timeout"`
	MaxEventsPerSec int           `json:"max_events_per_sec" yaml:"max_events_per_sec"`
}

// BackpressureConfig defines backpressure handling
type BackpressureConfig struct {
	Enabled           bool          `json:"enabled" yaml:"enabled"`
	HighWatermark     float64       `json:"high_watermark" yaml:"high_watermark"`
	LowWatermark      float64       `json:"low_watermark" yaml:"low_watermark"`
	DropThreshold     float64       `json:"drop_threshold" yaml:"drop_threshold"`
	RecoveryDelay     time.Duration `json:"recovery_delay" yaml:"recovery_delay"`
	SamplingReduction float64       `json:"sampling_reduction" yaml:"sampling_reduction"`
	MaxEventsPerSec   int           `json:"max_events_per_sec" yaml:"max_events_per_sec"`
	MemoryThresholdMB int           `json:"memory_threshold_mb" yaml:"memory_threshold_mb"`
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
	Name            string             `json:"name" yaml:"name"`
	Enabled         bool               `json:"enabled" yaml:"enabled"`
	BufferConfig    eBPFBufferConfig   `json:"buffer_config" yaml:"buffer_config"`
	ResourceLimits  ResourceLimits     `json:"resource_limits" yaml:"resource_limits"`
	Backpressure    BackpressureConfig `json:"backpressure" yaml:"backpressure"`
	Health          HealthConfig       `json:"health" yaml:"health"`
	SamplingEnabled bool               `json:"sampling_enabled" yaml:"sampling_enabled"`
	SamplingRate    int                `json:"sampling_rate" yaml:"sampling_rate"`
	DebugMode       bool               `json:"debug_mode" yaml:"debug_mode"`
}

// DefaultConfig returns production-ready default configuration
func DefaultConfig() *Config {
	return &Config{
		Name:    "kernel-collector",
		Enabled: true,
		BufferConfig: eBPFBufferConfig{
			KernelEventsBuffer:   DefaultKernelBufferKB,
			ProcessEventsBuffer:  DefaultProcessBufferKB,
			NetworkEventsBuffer:  DefaultNetworkBufferKB,
			SecurityEventsBuffer: DefaultSecurityBufferKB,
		},
		ResourceLimits: ResourceLimits{
			MaxMemoryMB:     DefaultMaxMemoryMB,
			MaxCPUPercent:   DefaultMaxCPUPercent,
			EventQueueSize:  DefaultEventQueueSize,
			BatchTimeout:    DefaultBatchTimeout,
			MaxEventsPerSec: DefaultMaxEventsPerSec,
		},
		Backpressure: BackpressureConfig{
			Enabled:           true,
			HighWatermark:     DefaultHighWatermark,
			LowWatermark:      DefaultLowWatermark,
			DropThreshold:     DefaultDropThreshold,
			RecoveryDelay:     DefaultRecoveryDelay,
			SamplingReduction: DefaultSamplingReduction,
			MaxEventsPerSec:   DefaultMaxEventsPerSec,
			MemoryThresholdMB: DefaultBackpressureMemoryMB,
		},
		Health: HealthConfig{
			Enabled:             true,
			Interval:            DefaultHealthCheckInterval,
			MaxFailures:         DefaultMaxHealthFailures,
			RestartOnFailure:    true,
			MemoryCheckInterval: DefaultMemoryCheckInterval,
		},
		SamplingEnabled: true,
		SamplingRate:    DefaultSamplingRate,
		DebugMode:       false,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate name
	if c.Name == "" {
		return fmt.Errorf("collector name cannot be empty")
	}

	// Validate buffer sizes
	if c.BufferConfig.KernelEventsBuffer <= 0 {
		return fmt.Errorf("kernel events buffer size must be positive, got %d", c.BufferConfig.KernelEventsBuffer)
	}
	if c.BufferConfig.ProcessEventsBuffer <= 0 {
		return fmt.Errorf("process events buffer size must be positive, got %d", c.BufferConfig.ProcessEventsBuffer)
	}
	if c.BufferConfig.NetworkEventsBuffer <= 0 {
		return fmt.Errorf("network events buffer size must be positive, got %d", c.BufferConfig.NetworkEventsBuffer)
	}
	if c.BufferConfig.SecurityEventsBuffer <= 0 {
		return fmt.Errorf("security events buffer size must be positive, got %d", c.BufferConfig.SecurityEventsBuffer)
	}

	// Validate resource limits
	if c.ResourceLimits.MaxMemoryMB <= 0 {
		return fmt.Errorf("max memory must be positive, got %d MB", c.ResourceLimits.MaxMemoryMB)
	}
	if c.ResourceLimits.MaxCPUPercent <= 0 || c.ResourceLimits.MaxCPUPercent > 100 {
		return fmt.Errorf("max CPU percent must be between 1-100, got %d", c.ResourceLimits.MaxCPUPercent)
	}
	if c.ResourceLimits.EventQueueSize <= 0 {
		return fmt.Errorf("event queue size must be positive, got %d", c.ResourceLimits.EventQueueSize)
	}
	if c.ResourceLimits.BatchTimeout <= 0 {
		return fmt.Errorf("batch timeout must be positive, got %v", c.ResourceLimits.BatchTimeout)
	}
	if c.ResourceLimits.MaxEventsPerSec <= 0 {
		return fmt.Errorf("max events per second must be positive, got %d", c.ResourceLimits.MaxEventsPerSec)
	}

	// Validate backpressure config if enabled
	if c.Backpressure.Enabled {
		if c.Backpressure.HighWatermark <= 0 || c.Backpressure.HighWatermark > 1 {
			return fmt.Errorf("high watermark must be between 0-1, got %f", c.Backpressure.HighWatermark)
		}
		if c.Backpressure.LowWatermark <= 0 || c.Backpressure.LowWatermark > 1 {
			return fmt.Errorf("low watermark must be between 0-1, got %f", c.Backpressure.LowWatermark)
		}
		if c.Backpressure.LowWatermark >= c.Backpressure.HighWatermark {
			return fmt.Errorf("low watermark (%f) must be less than high watermark (%f)",
				c.Backpressure.LowWatermark, c.Backpressure.HighWatermark)
		}
		if c.Backpressure.DropThreshold <= 0 || c.Backpressure.DropThreshold > 1 {
			return fmt.Errorf("drop threshold must be between 0-1, got %f", c.Backpressure.DropThreshold)
		}
		if c.Backpressure.SamplingReduction <= 0 || c.Backpressure.SamplingReduction > 1 {
			return fmt.Errorf("sampling reduction must be between 0-1, got %f", c.Backpressure.SamplingReduction)
		}
	}

	// Validate health config if enabled
	if c.Health.Enabled {
		if c.Health.Interval <= 0 {
			return fmt.Errorf("health check interval must be positive, got %v", c.Health.Interval)
		}
		if c.Health.MaxFailures <= 0 {
			return fmt.Errorf("max failures must be positive, got %d", c.Health.MaxFailures)
		}
	}

	// Validate sampling rate
	if c.SamplingEnabled && c.SamplingRate <= 0 {
		return fmt.Errorf("sampling rate must be positive when sampling is enabled, got %d", c.SamplingRate)
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
