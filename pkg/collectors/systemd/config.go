package systemd

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

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		BufferSize:      10000,
		EnableEBPF:      true,
		EnableJournal:   true,
		ServicePatterns: []string{}, // Monitor all services
	}
}
