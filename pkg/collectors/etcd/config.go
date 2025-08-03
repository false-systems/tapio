package etcd

// TLSConfig holds TLS configuration
type TLSConfig struct {
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	CAFile   string `json:"ca_file"`
}

// Config holds configuration for etcd collector
type Config struct {
	// Buffer size for events channel
	BufferSize int

	// Enable eBPF monitoring
	EnableEBPF bool

	// etcd endpoints for API monitoring
	Endpoints []string `json:"endpoints"`
	
	// Authentication
	Username  string     `json:"username"`
	Password  string     `json:"password"`
	
	// TLS configuration
	TLS       *TLSConfig `json:"tls"`
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		BufferSize: 10000,
		EnableEBPF: true,
		Endpoints:  []string{}, // No API monitoring by default
	}
}
