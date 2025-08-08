package dns

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// Config represents DNS collector configuration
type Config struct {
	// Base collector config
	collectors.CollectorConfig

	// DNS-specific configuration
	EnabledProtocols []string `json:"enabled_protocols" yaml:"enabled_protocols"` // UDP, TCP
	CapturePort      uint16   `json:"capture_port" yaml:"capture_port"`           // DNS port (53)

	// Cache configuration
	CacheEnabled bool          `json:"cache_enabled" yaml:"cache_enabled"`
	CacheSize    int           `json:"cache_size" yaml:"cache_size"`       // Max cache entries
	CacheTTLMin  time.Duration `json:"cache_ttl_min" yaml:"cache_ttl_min"` // Minimum cache TTL
	CacheTTLMax  time.Duration `json:"cache_ttl_max" yaml:"cache_ttl_max"` // Maximum cache TTL

	// Rate limiting
	RateLimitEnabled bool `json:"rate_limit_enabled" yaml:"rate_limit_enabled"`
	RateLimitRPS     int  `json:"rate_limit_rps" yaml:"rate_limit_rps"`     // Requests per second
	RateLimitBurst   int  `json:"rate_limit_burst" yaml:"rate_limit_burst"` // Burst size

	// Correlation settings
	CorrelationTimeout time.Duration `json:"correlation_timeout" yaml:"correlation_timeout"` // Query-response timeout

	// eBPF settings
	EBPFEnabled    bool `json:"ebpf_enabled" yaml:"ebpf_enabled"`
	EBPFBufferSize int  `json:"ebpf_buffer_size" yaml:"ebpf_buffer_size"` // Ring buffer size
	EBPFMapSize    int  `json:"ebpf_map_size" yaml:"ebpf_map_size"`       // BPF map entries

	// Filtering
	IgnoredDomains []string `json:"ignored_domains" yaml:"ignored_domains"`   // Domains to ignore
	OnlyServiceDNS bool     `json:"only_service_dns" yaml:"only_service_dns"` // Only capture k8s service DNS

	// Performance tuning
	WorkerCount     int           `json:"worker_count" yaml:"worker_count"`
	PacketBatchSize int           `json:"packet_batch_size" yaml:"packet_batch_size"`
	FlushInterval   time.Duration `json:"flush_interval" yaml:"flush_interval"`
}

// Validate validates the DNS collector configuration
func (c *Config) Validate() error {
	// Validate base config
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be positive, got %d", c.BufferSize)
	}

	// Validate protocols
	if len(c.EnabledProtocols) == 0 {
		return fmt.Errorf("at least one protocol must be enabled")
	}

	validProtocols := map[string]bool{"UDP": true, "TCP": true}
	for _, proto := range c.EnabledProtocols {
		if !validProtocols[proto] {
			return fmt.Errorf("invalid protocol: %s (must be UDP or TCP)", proto)
		}
	}

	// Validate port
	if c.CapturePort == 0 {
		return fmt.Errorf("capture_port must be specified")
	}

	// Validate cache settings
	if c.CacheEnabled {
		if c.CacheSize <= 0 {
			return fmt.Errorf("cache_size must be positive when cache is enabled, got %d", c.CacheSize)
		}
		if c.CacheTTLMin <= 0 {
			return fmt.Errorf("cache_ttl_min must be positive when cache is enabled")
		}
		if c.CacheTTLMax < c.CacheTTLMin {
			return fmt.Errorf("cache_ttl_max (%v) must be >= cache_ttl_min (%v)", c.CacheTTLMax, c.CacheTTLMin)
		}
	}

	// Validate rate limiting
	if c.RateLimitEnabled {
		if c.RateLimitRPS <= 0 {
			return fmt.Errorf("rate_limit_rps must be positive when rate limiting is enabled, got %d", c.RateLimitRPS)
		}
		if c.RateLimitBurst <= 0 {
			return fmt.Errorf("rate_limit_burst must be positive when rate limiting is enabled, got %d", c.RateLimitBurst)
		}
	}

	// Validate timeouts
	if c.CorrelationTimeout <= 0 {
		return fmt.Errorf("correlation_timeout must be positive")
	}

	// Validate eBPF settings
	if c.EBPFEnabled {
		if c.EBPFBufferSize <= 0 {
			return fmt.Errorf("ebpf_buffer_size must be positive when eBPF is enabled, got %d", c.EBPFBufferSize)
		}
		if c.EBPFMapSize <= 0 {
			return fmt.Errorf("ebpf_map_size must be positive when eBPF is enabled, got %d", c.EBPFMapSize)
		}
	}

	// Validate performance settings
	if c.WorkerCount <= 0 {
		return fmt.Errorf("worker_count must be positive, got %d", c.WorkerCount)
	}

	if c.PacketBatchSize <= 0 {
		return fmt.Errorf("packet_batch_size must be positive, got %d", c.PacketBatchSize)
	}

	if c.FlushInterval <= 0 {
		return fmt.Errorf("flush_interval must be positive")
	}

	return nil
}

// DefaultConfig returns default DNS collector configuration
func DefaultConfig() Config {
	return Config{
		CollectorConfig: collectors.DefaultCollectorConfig(),

		// DNS settings
		EnabledProtocols: []string{"UDP", "TCP"},
		CapturePort:      53,

		// Cache settings
		CacheEnabled: true,
		CacheSize:    10000,
		CacheTTLMin:  30 * time.Second,
		CacheTTLMax:  24 * time.Hour,

		// Rate limiting
		RateLimitEnabled: true,
		RateLimitRPS:     1000,
		RateLimitBurst:   2000,

		// Correlation
		CorrelationTimeout: 5 * time.Second,

		// eBPF settings
		EBPFEnabled:    true,
		EBPFBufferSize: 1024 * 1024, // 1MB ring buffer
		EBPFMapSize:    10240,       // 10K entries

		// Performance
		WorkerCount:     4,
		PacketBatchSize: 100,
		FlushInterval:   100 * time.Millisecond,

		// Filtering - empty means capture all
		IgnoredDomains: []string{},
		OnlyServiceDNS: false,
	}
}

// IsProtocolEnabled checks if a protocol is enabled
func (c *Config) IsProtocolEnabled(protocol string) bool {
	for _, p := range c.EnabledProtocols {
		if p == protocol {
			return true
		}
	}
	return false
}

// IsDomainIgnored checks if a domain should be ignored
func (c *Config) IsDomainIgnored(domain string) bool {
	for _, ignored := range c.IgnoredDomains {
		if domain == ignored {
			return true
		}
		// Simple wildcard matching for subdomains
		if len(ignored) > 0 && ignored[0] == '*' {
			suffix := ignored[1:]
			if len(domain) >= len(suffix) && domain[len(domain)-len(suffix):] == suffix {
				return true
			}
		}
	}
	return false
}
