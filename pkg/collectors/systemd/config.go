package systemd

import "fmt"

// Config holds configuration for systemd collector
type Config struct {
	// Collector name
	Name string

	// Buffer size for events channel
	BufferSize int

	// Enable eBPF monitoring
	EnableEBPF bool

	// Enable journal log collection
	EnableJournal bool

	// Service patterns to monitor (empty = all)
	ServicePatterns []string
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

	return nil
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		BufferSize:      10000,
		EnableEBPF:      true,
		EnableJournal:   true,
		ServicePatterns: []string{}, // Monitor all services
	}
}
