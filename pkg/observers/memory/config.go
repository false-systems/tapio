package memory

import (
	"fmt"
	"time"
)

// OperationMode defines how aggressive the collector is
type OperationMode string

const (
	ModeGrowthDetection OperationMode = "growth_detection" // RSS monitoring only (always on)
	ModeTargeted        OperationMode = "targeted"         // Track specific PID
	ModeDebugging       OperationMode = "debugging"        // Full tracking with stacks
)

// Config holds configuration for memory leak hunter
type Config struct {
	// Basic settings
	Name       string `json:"name"`
	BufferSize int    `json:"buffer_size"`
	EnableEBPF bool   `json:"enable_ebpf"`

	// Operation mode
	Mode OperationMode `json:"mode"`

	// Pre-processing filters (lean logic in collector)
	MinAllocationSize int64         `json:"min_allocation_size"` // Ignore allocations smaller than this
	MinUnfreedAge     time.Duration `json:"min_unfreed_age"`     // Only report if unfreed for this long
	SamplingRate      int           `json:"sampling_rate"`       // 1 in N allocations tracked
	MaxEventsPerSec   int           `json:"max_events_per_sec"`  // Rate limiting

	// Stack deduplication
	StackDedupWindow time.Duration `json:"stack_dedup_window"` // Dedup same stacks within window

	// Targeted mode settings
	TargetPID      int32         `json:"target_pid"`       // Specific PID to track (0 = all)
	TargetDuration time.Duration `json:"target_duration"`  // How long to track in targeted mode
	TargetCGroupID uint64        `json:"target_cgroup_id"` // Target specific container

	// RSS growth detection
	RSSGrowthThreshold int64         `json:"rss_growth_threshold"` // Report if RSS grows by this much (pages)
	RSSCheckInterval   time.Duration `json:"rss_check_interval"`   // How often to check RSS

	// Enhancement #3: Configurable libc path for portability
	LibCPath string `json:"libc_path"` // Path to libc.so for uprobe attachment
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be greater than 0")
	}
	if c.BufferSize > 1000000 {
		return fmt.Errorf("buffer size must not exceed 1,000,000")
	}

	if c.MinAllocationSize < 0 {
		return fmt.Errorf("min allocation size cannot be negative")
	}

	if c.SamplingRate < 1 {
		c.SamplingRate = 1 // No sampling
	}

	if c.MaxEventsPerSec <= 0 {
		c.MaxEventsPerSec = 1000 // Default rate limit
	}

	switch c.Mode {
	case ModeGrowthDetection, ModeTargeted, ModeDebugging:
		// Valid modes
	default:
		return fmt.Errorf("invalid operation mode: %s", c.Mode)
	}

	return nil
}

// DefaultConfig returns production-ready default configuration
func DefaultConfig() *Config {
	return &Config{
		Name:       "memory-leak-hunter",
		BufferSize: 10000,
		EnableEBPF: true,

		// Start with least invasive mode
		Mode: ModeGrowthDetection,

		// Realistic pre-processing
		MinAllocationSize: 10240, // 10KB minimum
		MinUnfreedAge:     30 * time.Second,
		SamplingRate:      10, // 1 in 10 for medium allocations
		MaxEventsPerSec:   1000,

		// Deduplication
		StackDedupWindow: 10 * time.Second,

		// RSS monitoring
		RSSGrowthThreshold: 256, // 1MB in 4KB pages
		RSSCheckInterval:   30 * time.Second,

		// Targeted mode (disabled by default)
		TargetPID:      0,
		TargetDuration: 5 * time.Minute,

		// Enhancement #3: Default libc path (Ubuntu/Debian)
		LibCPath: "/lib/x86_64-linux-gnu/libc.so.6",
	}
}
