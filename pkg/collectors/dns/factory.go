package dns

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/config"
)

// DNSFactory creates DNS collectors from type-safe configuration
type DNSFactory struct {
	*config.BaseCollectorFactory
}

// NewDNSFactory creates a new DNS collector factory
func NewDNSFactory() *DNSFactory {
	return &DNSFactory{
		BaseCollectorFactory: config.NewBaseCollectorFactory("DNS", "dns"),
	}
}

// CreateCollector creates a new DNS collector from configuration
func (f *DNSFactory) CreateCollector(ctx context.Context, cfg config.CollectorConfig) (config.Collector, error) {
	dnsConfig, ok := cfg.(*config.DNSConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type for DNS collector, expected *config.DNSConfig, got %T", cfg)
	}

	// Convert from typed config to internal DNS Config
	internalConfig := Config{
		Name:         dnsConfig.GetName(),
		BufferSize:   dnsConfig.GetBufferSize(),
		Interface:    dnsConfig.Interface,
		EnableEBPF:   dnsConfig.EnableEBPF,
		EnableSocket: dnsConfig.EnableSocket,

		// DNS specific defaults
		DNSPort:   53,
		Protocols: []string{"udp", "tcp"},

		// Rate limiting defaults
		RateLimitEnabled: false,
		RateLimitRPS:     1000.0,
		RateLimitBurst:   100,

		// Cache defaults
		CacheEnabled: false,
		CacheSize:    1000,
		CacheTTL:     time.Minute * 5,

		// Performance defaults
		WorkerCount:        2,
		BatchSize:          100,
		FlushInterval:      time.Second,
		SlowQueryThreshold: time.Millisecond * 100,
	}

	collector, err := NewCollector(dnsConfig.GetName(), internalConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create DNS collector: %w", err)
	}

	return collector, nil
}

// ValidateConfig validates that the provided config is compatible with this factory
func (f *DNSFactory) ValidateConfig(cfg config.CollectorConfig) error {
	// First run base validation
	if err := f.BaseCollectorFactory.ValidateConfig(cfg); err != nil {
		return err
	}

	// Check type
	dnsConfig, ok := cfg.(*config.DNSConfig)
	if !ok {
		return fmt.Errorf("invalid config type for DNS collector, expected *config.DNSConfig, got %T", cfg)
	}

	// DNS-specific validation
	if !dnsConfig.EnableEBPF && !dnsConfig.EnableSocket {
		return fmt.Errorf("at least one monitoring method (EnableEBPF or EnableSocket) must be enabled")
	}

	return nil
}
