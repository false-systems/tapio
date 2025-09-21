package dns

import "time"

// Config holds configuration for DNS problem observer
type Config struct {
	// Observer name
	Name string

	// Problem detection thresholds
	SlowQueryThresholdMs int // DNS queries slower than this are problems (default: 100ms)
	TimeoutMs            int // No response after this = timeout (default: 5000ms)
	RepeatWindowSec      int // Window to track repeated failures (default: 60s)
	RepeatThreshold      int // Alert after N repeated failures (default: 3)

	// eBPF configuration
	EnableEBPF     bool
	RingBufferSize int
	BufferSize     int

	// Rate limiting
	MaxEventsPerSecond int           // Prevent event storms
	RateLimitWindow    time.Duration // Rate limit window

	// Filtering
	MonitoredPorts  []uint16 // DNS ports to monitor (default: [53])
	IgnoreLocalhost bool     // Ignore 127.0.0.1 queries
	OnlyProblems    bool     // Only emit problem events (no success)
}

// DefaultConfig returns default configuration for DNS problem detection
func DefaultConfig() *Config {
	return &Config{
		Name:                 "dns-problems",
		SlowQueryThresholdMs: 100,  // 100ms is slow for DNS
		TimeoutMs:            5000, // 5 second timeout
		RepeatWindowSec:      60,   // Track failures in last minute
		RepeatThreshold:      3,    // Alert after 3 failures
		EnableEBPF:           true,
		RingBufferSize:       4 * 1024 * 1024, // 4MB
		BufferSize:           1000,            // Small buffer - we expect few problems
		MaxEventsPerSecond:   100,             // DNS problems shouldn't flood
		RateLimitWindow:      time.Second,
		MonitoredPorts:       []uint16{53}, // Standard DNS port
		IgnoreLocalhost:      true,         // Don't monitor local resolver
		OnlyProblems:         true,         // We're a negative observer
	}
}

// Validate checks if configuration is valid
func (c *Config) Validate() error {
	// Set defaults for missing values
	if c.SlowQueryThresholdMs <= 0 {
		c.SlowQueryThresholdMs = 100
	}
	if c.TimeoutMs <= 0 {
		c.TimeoutMs = 5000
	}
	if c.RepeatWindowSec <= 0 {
		c.RepeatWindowSec = 60
	}
	if c.RepeatThreshold <= 0 {
		c.RepeatThreshold = 3
	}
	if c.RingBufferSize < 1024*1024 {
		c.RingBufferSize = 4 * 1024 * 1024
	}
	if c.BufferSize < 100 {
		c.BufferSize = 1000
	}
	if c.MaxEventsPerSecond <= 0 {
		c.MaxEventsPerSecond = 100
	}
	if len(c.MonitoredPorts) == 0 {
		c.MonitoredPorts = []uint16{53}
	}
	return nil
}
