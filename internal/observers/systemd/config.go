package systemd

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

// Config holds configuration for systemd observer
type Config struct {
	// Buffer size for events channel
	BufferSize int

	// Enable eBPF monitoring
	EnableEBPF bool

	// Enable journal log collection
	EnableJournal bool

	// Service patterns to monitor (empty = all)
	ServicePatterns []string

	// Monitor service state changes
	MonitorServiceStates bool

	// Monitor cgroup events
	MonitorCgroups bool

	// Rate limiting
	RateLimitPerSecond int

	// Health check interval
	HealthCheckInterval time.Duration

	// Logger
	Logger *zap.Logger
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be greater than 0")
	}
	if c.BufferSize > 1000000 {
		return fmt.Errorf("buffer size must not exceed 1,000,000")
	}

	// At least one monitoring method must be enabled
	if !c.EnableEBPF && !c.EnableJournal {
		return fmt.Errorf("at least one of EnableEBPF or EnableJournal must be true")
	}

	if c.RateLimitPerSecond < 0 {
		return fmt.Errorf("rate limit must be non-negative")
	}

	if c.HealthCheckInterval < 0 {
		return fmt.Errorf("health check interval must be non-negative")
	}

	return nil
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		BufferSize:           10000,
		EnableEBPF:           true,
		EnableJournal:        false,      // Journal disabled by default
		ServicePatterns:      []string{}, // Monitor all services
		MonitorServiceStates: true,
		MonitorCgroups:       true,
		RateLimitPerSecond:   1000,
		HealthCheckInterval:  30 * time.Second,
	}
}

// SetDefaults fills in default values for unset fields
func (c *Config) SetDefaults() {
	if c.BufferSize == 0 {
		c.BufferSize = 10000
	}
	if c.RateLimitPerSecond == 0 {
		c.RateLimitPerSecond = 1000
	}
	if c.HealthCheckInterval == 0 {
		c.HealthCheckInterval = 30 * time.Second
	}
}
