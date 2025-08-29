package resourcestarvation

import (
	"fmt"
	"os"
	"time"
)

// Config defines configuration for the resource starvation collector
// Designed with extreme emphasis on stability and crash prevention
type Config struct {
	// Basic configuration
	Name    string `json:"name" yaml:"name"`
	Enabled bool   `json:"enabled" yaml:"enabled"`

	// Detection thresholds (in milliseconds)
	StarvationThresholdMS int `json:"starvation_threshold_ms" yaml:"starvation_threshold_ms"`
	SevereThresholdMS     int `json:"severe_threshold_ms" yaml:"severe_threshold_ms"`
	CriticalThresholdMS   int `json:"critical_threshold_ms" yaml:"critical_threshold_ms"`

	// Sampling and performance controls
	SampleRate       float64 `json:"sample_rate" yaml:"sample_rate"`
	RingBufferSizeKB int     `json:"ring_buffer_size_kb" yaml:"ring_buffer_size_kb"`
	MaxEventsPerSec  int     `json:"max_events_per_sec" yaml:"max_events_per_sec"`

	// Pattern detection settings
	EnablePatternDetection bool          `json:"enable_pattern_detection" yaml:"enable_pattern_detection"`
	PatternWindowSec       int           `json:"pattern_window_sec" yaml:"pattern_window_sec"`
	MinPatternConfidence   float64       `json:"min_pattern_confidence" yaml:"min_pattern_confidence"`
	PatternCacheSize       int           `json:"pattern_cache_size" yaml:"pattern_cache_size"`
	PatternCleanupInterval time.Duration `json:"pattern_cleanup_interval" yaml:"pattern_cleanup_interval"`

	// Kubernetes enrichment
	EnableK8sEnrichment bool          `json:"enable_k8s_enrichment" yaml:"enable_k8s_enrichment"`
	NodeName            string        `json:"node_name" yaml:"node_name"`
	KubeletTimeout      time.Duration `json:"kubelet_timeout" yaml:"kubelet_timeout"`

	// Resource management and crash prevention
	MaxMemoryMB         int           `json:"max_memory_mb" yaml:"max_memory_mb"`
	ProcessingTimeout   time.Duration `json:"processing_timeout" yaml:"processing_timeout"`
	HealthCheckInterval time.Duration `json:"health_check_interval" yaml:"health_check_interval"`
	GracefulShutdownSec int           `json:"graceful_shutdown_sec" yaml:"graceful_shutdown_sec"`

	// Error handling and resilience
	MaxConsecutiveErrors    int           `json:"max_consecutive_errors" yaml:"max_consecutive_errors"`
	ErrorBackoffDuration    time.Duration `json:"error_backoff_duration" yaml:"error_backoff_duration"`
	CircuitBreakerEnabled   bool          `json:"circuit_breaker_enabled" yaml:"circuit_breaker_enabled"`
	CircuitBreakerThreshold int           `json:"circuit_breaker_threshold" yaml:"circuit_breaker_threshold"`

	// eBPF-specific settings
	EBPFProgramTimeout  time.Duration `json:"ebpf_program_timeout" yaml:"ebpf_program_timeout"`
	MaxTrackedProcesses int           `json:"max_tracked_processes" yaml:"max_tracked_processes"`
	ProcessTrackingTTL  time.Duration `json:"process_tracking_ttl" yaml:"process_tracking_ttl"`
	EnableVerifierLogs  bool          `json:"enable_verifier_logs" yaml:"enable_verifier_logs"`

	// Event processing
	EventChannelSize int           `json:"event_channel_size" yaml:"event_channel_size"`
	BatchSize        int           `json:"batch_size" yaml:"batch_size"`
	BatchTimeout     time.Duration `json:"batch_timeout" yaml:"batch_timeout"`
	FlushTimeout     time.Duration `json:"flush_timeout" yaml:"flush_timeout"`

	// Debugging and monitoring
	VerboseLogging  bool `json:"verbose_logging" yaml:"verbose_logging"`
	EnableProfiling bool `json:"enable_profiling" yaml:"enable_profiling"`
	MetricsPort     int  `json:"metrics_port" yaml:"metrics_port"`
	DebugMode       bool `json:"debug_mode" yaml:"debug_mode"`
}

// NewDefaultConfig creates a production-ready configuration with conservative defaults
// Optimized for stability and crash prevention
func NewDefaultConfig() *Config {
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		if hostname, err := os.Hostname(); err == nil {
			nodeName = hostname
		} else {
			nodeName = "unknown"
		}
	}

	return &Config{
		// Basic settings
		Name:    "resource-starvation",
		Enabled: true,

		// Conservative detection thresholds
		StarvationThresholdMS: 100,  // 100ms wait = starvation
		SevereThresholdMS:     500,  // 500ms wait = severe
		CriticalThresholdMS:   2000, // 2s wait = critical

		// Performance controls - conservative for stability
		SampleRate:       0.1,  // Sample 10% to reduce load
		RingBufferSizeKB: 1024, // 1MB ring buffer
		MaxEventsPerSec:  1000, // Rate limit to prevent overload

		// Pattern detection - enabled but limited
		EnablePatternDetection: true,
		PatternWindowSec:       60,
		MinPatternConfidence:   0.7,
		PatternCacheSize:       1000,
		PatternCleanupInterval: 5 * time.Minute,

		// Kubernetes enrichment
		EnableK8sEnrichment: true,
		NodeName:            nodeName,
		KubeletTimeout:      5 * time.Second,

		// Resource limits for crash prevention
		MaxMemoryMB:         512,              // Limit memory usage
		ProcessingTimeout:   30 * time.Second, // Timeout long operations
		HealthCheckInterval: 10 * time.Second, // Regular health checks
		GracefulShutdownSec: 30,               // Graceful shutdown window

		// Error handling
		MaxConsecutiveErrors:    10,
		ErrorBackoffDuration:    time.Second,
		CircuitBreakerEnabled:   true,
		CircuitBreakerThreshold: 5,

		// eBPF settings - conservative
		EBPFProgramTimeout:  10 * time.Second,
		MaxTrackedProcesses: 10000,
		ProcessTrackingTTL:  5 * time.Minute,
		EnableVerifierLogs:  false, // Disable in production

		// Event processing
		EventChannelSize: 10000,
		BatchSize:        100,
		BatchTimeout:     time.Second,
		FlushTimeout:     5 * time.Second,

		// Monitoring
		VerboseLogging:  false,
		EnableProfiling: false,
		MetricsPort:     0, // Disabled by default
		DebugMode:       false,
	}
}

// Validate checks configuration for correctness and safety
func (c *Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("collector name cannot be empty")
	}

	// Validate thresholds
	if c.StarvationThresholdMS <= 0 {
		return fmt.Errorf("starvation_threshold_ms must be positive, got %d", c.StarvationThresholdMS)
	}
	if c.SevereThresholdMS <= c.StarvationThresholdMS {
		return fmt.Errorf("severe_threshold_ms (%d) must be greater than starvation_threshold_ms (%d)",
			c.SevereThresholdMS, c.StarvationThresholdMS)
	}
	if c.CriticalThresholdMS <= c.SevereThresholdMS {
		return fmt.Errorf("critical_threshold_ms (%d) must be greater than severe_threshold_ms (%d)",
			c.CriticalThresholdMS, c.SevereThresholdMS)
	}

	// Validate sample rate
	if c.SampleRate < 0.0 || c.SampleRate > 1.0 {
		return fmt.Errorf("sample_rate must be between 0.0 and 1.0, got %f", c.SampleRate)
	}

	// Validate buffer sizes
	if c.RingBufferSizeKB <= 0 || c.RingBufferSizeKB > 16*1024 {
		return fmt.Errorf("ring_buffer_size_kb must be between 1 and 16384, got %d", c.RingBufferSizeKB)
	}
	if c.MaxEventsPerSec <= 0 {
		return fmt.Errorf("max_events_per_sec must be positive, got %d", c.MaxEventsPerSec)
	}

	// Validate pattern detection settings
	if c.EnablePatternDetection {
		if c.PatternWindowSec <= 0 {
			return fmt.Errorf("pattern_window_sec must be positive when pattern detection enabled, got %d",
				c.PatternWindowSec)
		}
		if c.MinPatternConfidence < 0.0 || c.MinPatternConfidence > 1.0 {
			return fmt.Errorf("min_pattern_confidence must be between 0.0 and 1.0, got %f",
				c.MinPatternConfidence)
		}
		if c.PatternCacheSize <= 0 {
			return fmt.Errorf("pattern_cache_size must be positive when pattern detection enabled, got %d",
				c.PatternCacheSize)
		}
	}

	// Validate resource limits
	if c.MaxMemoryMB <= 0 || c.MaxMemoryMB > 8*1024 {
		return fmt.Errorf("max_memory_mb must be between 1 and 8192, got %d", c.MaxMemoryMB)
	}
	if c.ProcessingTimeout <= 0 {
		return fmt.Errorf("processing_timeout must be positive, got %v", c.ProcessingTimeout)
	}
	if c.GracefulShutdownSec <= 0 || c.GracefulShutdownSec > 300 {
		return fmt.Errorf("graceful_shutdown_sec must be between 1 and 300, got %d", c.GracefulShutdownSec)
	}

	// Validate error handling
	if c.MaxConsecutiveErrors <= 0 {
		return fmt.Errorf("max_consecutive_errors must be positive, got %d", c.MaxConsecutiveErrors)
	}
	if c.CircuitBreakerEnabled && c.CircuitBreakerThreshold <= 0 {
		return fmt.Errorf("circuit_breaker_threshold must be positive when circuit breaker enabled, got %d",
			c.CircuitBreakerThreshold)
	}

	// Validate eBPF settings
	if c.MaxTrackedProcesses <= 0 || c.MaxTrackedProcesses > 100000 {
		return fmt.Errorf("max_tracked_processes must be between 1 and 100000, got %d", c.MaxTrackedProcesses)
	}
	if c.ProcessTrackingTTL <= 0 {
		return fmt.Errorf("process_tracking_ttl must be positive, got %v", c.ProcessTrackingTTL)
	}

	// Validate event processing
	if c.EventChannelSize <= 0 {
		return fmt.Errorf("event_channel_size must be positive, got %d", c.EventChannelSize)
	}
	if c.BatchSize <= 0 || c.BatchSize > 10000 {
		return fmt.Errorf("batch_size must be between 1 and 10000, got %d", c.BatchSize)
	}

	// Cross-validation checks
	if c.BatchTimeout >= c.ProcessingTimeout {
		return fmt.Errorf("batch_timeout (%v) must be less than processing_timeout (%v)",
			c.BatchTimeout, c.ProcessingTimeout)
	}

	return nil
}

// ApplyDefaults applies default values for any unset fields
func (c *Config) ApplyDefaults() {
	defaults := NewDefaultConfig()

	if c.Name == "" {
		c.Name = defaults.Name
	}
	if c.StarvationThresholdMS == 0 {
		c.StarvationThresholdMS = defaults.StarvationThresholdMS
	}
	if c.SevereThresholdMS == 0 {
		c.SevereThresholdMS = defaults.SevereThresholdMS
	}
	if c.CriticalThresholdMS == 0 {
		c.CriticalThresholdMS = defaults.CriticalThresholdMS
	}
	if c.SampleRate == 0.0 {
		c.SampleRate = defaults.SampleRate
	}
	if c.RingBufferSizeKB == 0 {
		c.RingBufferSizeKB = defaults.RingBufferSizeKB
	}
	if c.MaxEventsPerSec == 0 {
		c.MaxEventsPerSec = defaults.MaxEventsPerSec
	}
	if c.PatternWindowSec == 0 {
		c.PatternWindowSec = defaults.PatternWindowSec
	}
	if c.MinPatternConfidence == 0.0 {
		c.MinPatternConfidence = defaults.MinPatternConfidence
	}
	if c.PatternCacheSize == 0 {
		c.PatternCacheSize = defaults.PatternCacheSize
	}
	if c.PatternCleanupInterval == 0 {
		c.PatternCleanupInterval = defaults.PatternCleanupInterval
	}
	if c.NodeName == "" {
		c.NodeName = defaults.NodeName
	}
	if c.KubeletTimeout == 0 {
		c.KubeletTimeout = defaults.KubeletTimeout
	}
	if c.MaxMemoryMB == 0 {
		c.MaxMemoryMB = defaults.MaxMemoryMB
	}
	if c.ProcessingTimeout == 0 {
		c.ProcessingTimeout = defaults.ProcessingTimeout
	}
	if c.HealthCheckInterval == 0 {
		c.HealthCheckInterval = defaults.HealthCheckInterval
	}
	if c.GracefulShutdownSec == 0 {
		c.GracefulShutdownSec = defaults.GracefulShutdownSec
	}
	if c.MaxConsecutiveErrors == 0 {
		c.MaxConsecutiveErrors = defaults.MaxConsecutiveErrors
	}
	if c.ErrorBackoffDuration == 0 {
		c.ErrorBackoffDuration = defaults.ErrorBackoffDuration
	}
	if c.CircuitBreakerThreshold == 0 {
		c.CircuitBreakerThreshold = defaults.CircuitBreakerThreshold
	}
	if c.EBPFProgramTimeout == 0 {
		c.EBPFProgramTimeout = defaults.EBPFProgramTimeout
	}
	if c.MaxTrackedProcesses == 0 {
		c.MaxTrackedProcesses = defaults.MaxTrackedProcesses
	}
	if c.ProcessTrackingTTL == 0 {
		c.ProcessTrackingTTL = defaults.ProcessTrackingTTL
	}
	if c.EventChannelSize == 0 {
		c.EventChannelSize = defaults.EventChannelSize
	}
	if c.BatchSize == 0 {
		c.BatchSize = defaults.BatchSize
	}
	if c.BatchTimeout == 0 {
		c.BatchTimeout = defaults.BatchTimeout
	}
	if c.FlushTimeout == 0 {
		c.FlushTimeout = defaults.FlushTimeout
	}
}

// IsSafeMode returns true if the configuration is set to safe/conservative values
func (c *Config) IsSafeMode() bool {
	return c.SampleRate <= 0.5 && // Sample at most 50%
		c.MaxEventsPerSec <= 2000 && // Conservative rate limit
		c.MaxMemoryMB <= 1024 && // Reasonable memory limit
		c.CircuitBreakerEnabled && // Circuit breaker enabled
		!c.DebugMode // Debug mode disabled
}

// GetEffectiveRingBufferSize returns the ring buffer size in bytes
func (c *Config) GetEffectiveRingBufferSize() int {
	return c.RingBufferSizeKB * 1024
}

// GetEffectiveShutdownTimeout returns the shutdown timeout as a duration
func (c *Config) GetEffectiveShutdownTimeout() time.Duration {
	return time.Duration(c.GracefulShutdownSec) * time.Second
}

// String returns a string representation of key configuration values
func (c *Config) String() string {
	return fmt.Sprintf("StarvationCollectorConfig{name=%s, enabled=%v, starvation_threshold=%dms, "+
		"sample_rate=%.2f, safe_mode=%v}",
		c.Name, c.Enabled, c.StarvationThresholdMS, c.SampleRate, c.IsSafeMode())
}
