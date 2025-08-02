package systemd

// Config holds configuration for systemd collector
type Config struct {
	// Buffer size for events channel
	BufferSize int
	
	// Enable eBPF monitoring
	EnableEBPF bool
	
	// Service patterns to monitor (empty = all)
	ServicePatterns []string
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		BufferSize:      10000,
		EnableEBPF:      true,
		ServicePatterns: []string{}, // Monitor all services
	}
}