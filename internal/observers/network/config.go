package network

import (
	"fmt"
	"os"
	"time"
)

// Config holds network observer configuration
type Config struct {
	// General settings
	Name          string        `json:"name" yaml:"name"`
	BufferSize    int           `json:"buffer_size" yaml:"buffer_size"`
	FlushInterval time.Duration `json:"flush_interval" yaml:"flush_interval"`

	// Network protocols
	EnableIPv4 bool `json:"enable_ipv4" yaml:"enable_ipv4"`
	EnableIPv6 bool `json:"enable_ipv6" yaml:"enable_ipv6"`
	EnableTCP  bool `json:"enable_tcp" yaml:"enable_tcp"`
	EnableUDP  bool `json:"enable_udp" yaml:"enable_udp"`
	EnableICMP bool `json:"enable_icmp" yaml:"enable_icmp"`

	// L7 protocols
	EnableHTTP  bool  `json:"enable_http" yaml:"enable_http"`
	EnableHTTPS bool  `json:"enable_https" yaml:"enable_https"`
	EnableDNS   bool  `json:"enable_dns" yaml:"enable_dns"`
	EnableGRPC  bool  `json:"enable_grpc" yaml:"enable_grpc"`
	HTTPPorts   []int `json:"http_ports" yaml:"http_ports"`
	HTTPSPorts  []int `json:"https_ports" yaml:"https_ports"`
	DNSPort     int   `json:"dns_port" yaml:"dns_port"`
	GRPCPorts   []int `json:"grpc_ports" yaml:"grpc_ports"`

	// Performance settings
	MaxEventsPerSecond int     `json:"max_events_per_second" yaml:"max_events_per_second"`
	SamplingRate       float64 `json:"sampling_rate" yaml:"sampling_rate"`
	RingBufferSize     int     `json:"ring_buffer_size" yaml:"ring_buffer_size"`

	// Features
	EnableL7Parse       bool `json:"enable_l7_parse" yaml:"enable_l7_parse"`
	EnableK8sEnrichment bool `json:"enable_k8s_enrichment" yaml:"enable_k8s_enrichment"`
	EnableFlowTracking  bool `json:"enable_flow_tracking" yaml:"enable_flow_tracking"`

	// Connection tracking
	MaxConnections            int           `json:"max_connections" yaml:"max_connections"`
	ConnectionTimeout         time.Duration `json:"connection_timeout" yaml:"connection_timeout"`
	ConnectionCleanupInterval time.Duration `json:"connection_cleanup_interval" yaml:"connection_cleanup_interval"`

	// Mock mode for development
	MockMode bool `json:"mock_mode" yaml:"mock_mode"`

	// eBPF settings
	EnableCORE      bool `json:"enable_core" yaml:"enable_core"`
	VerifierLogSize int  `json:"verifier_log_size" yaml:"verifier_log_size"`
}

// DefaultConfig returns default network observer configuration
func DefaultConfig() *Config {
	return &Config{
		Name:          "network-observer",
		BufferSize:    10000,
		FlushInterval: 10 * time.Second,

		// Enable all protocols by default
		EnableIPv4: true,
		EnableIPv6: true,
		EnableTCP:  true,
		EnableUDP:  true,
		EnableICMP: true,

		// L7 protocols
		EnableHTTP:  true,
		EnableHTTPS: false, // Disabled by default (requires TLS parsing)
		EnableDNS:   true,
		EnableGRPC:  false, // Disabled by default (requires HTTP/2 parsing)
		HTTPPorts:   []int{80, 8080, 8081, 3000, 5000, 9000},
		HTTPSPorts:  []int{443, 8443},
		DNSPort:     53,
		GRPCPorts:   []int{50051, 9090},

		// Performance
		MaxEventsPerSecond: 10000,
		SamplingRate:       1.0,             // Sample all events by default
		RingBufferSize:     8 * 1024 * 1024, // 8MB

		// Features
		EnableL7Parse:       true,
		EnableK8sEnrichment: true,
		EnableFlowTracking:  true,

		// Connection tracking
		MaxConnections:            65536,
		ConnectionTimeout:         5 * time.Minute,
		ConnectionCleanupInterval: 30 * time.Second,

		// Mock mode from env
		MockMode: os.Getenv("TAPIO_MOCK_MODE") == "true",

		// eBPF
		EnableCORE:      true,
		VerifierLogSize: 64 * 1024 * 1024, // 64MB for verifier logs
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be positive")
	}

	if c.MaxEventsPerSecond <= 0 {
		return fmt.Errorf("max_events_per_second must be positive")
	}

	if c.SamplingRate < 0 || c.SamplingRate > 1 {
		return fmt.Errorf("sampling_rate must be between 0 and 1")
	}

	if c.RingBufferSize <= 0 {
		return fmt.Errorf("ring_buffer_size must be positive")
	}

	if c.MaxConnections <= 0 {
		return fmt.Errorf("max_connections must be positive")
	}

	if !c.EnableIPv4 && !c.EnableIPv6 {
		return fmt.Errorf("at least one IP version must be enabled")
	}

	if !c.EnableTCP && !c.EnableUDP {
		return fmt.Errorf("at least one transport protocol must be enabled")
	}

	return nil
}

// IsL7Enabled returns true if any L7 protocol is enabled
func (c *Config) IsL7Enabled() bool {
	return c.EnableL7Parse && (c.EnableHTTP || c.EnableHTTPS || c.EnableDNS || c.EnableGRPC)
}
