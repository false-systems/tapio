package cni

// Config holds configuration for CNI collector
type Config struct {
	// Buffer size for events channel
	BufferSize int

	// Enable eBPF monitoring
	EnableEBPF bool
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		BufferSize: 10000,
		EnableEBPF: true,
	}
}
