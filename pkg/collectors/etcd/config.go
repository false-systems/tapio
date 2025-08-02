package etcd

// Config holds configuration for etcd collector
type Config struct {
	// Buffer size for events channel
	BufferSize int
	
	// Enable eBPF monitoring
	EnableEBPF bool
	
	// etcd endpoints for API monitoring (optional)
	Endpoints []string
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		BufferSize: 10000,
		EnableEBPF: true,
		Endpoints:  []string{}, // No API monitoring by default
	}
}