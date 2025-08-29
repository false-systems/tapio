package dns

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the DNS collector factory with the collector registry
func init() {
	// Register the DNS collector factory
	RegisterDNSCollector()
}

// RegisterDNSCollector registers the DNS collector factory with the orchestrator
func RegisterDNSCollector() {
	factory := func(name string, config *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
		// Convert YAML config to DNS-specific config
		dnsConfig := DefaultConfig()
		
		// Apply configuration from YAML
		if config != nil {
			if config.BufferSize > 0 {
				dnsConfig.BufferSize = config.BufferSize
			}
			dnsConfig.EnableEBPF = config.EnableEBPF
			dnsConfig.ContainerIDExtraction = true // Enable for operational monitoring
			dnsConfig.ParseAnswers = true // Enable for operational monitoring
		}

		// Create the DNS collector
		collector, err := NewCollector(name, dnsConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create DNS collector %s: %w", name, err)
		}

		return collector, nil
	}

	// Register the factory with the orchestrator
	orchestrator.RegisterCollectorFactory("dns", factory)
}

// CreateDNSCollector creates a new DNS collector with the given configuration
func CreateDNSCollector(config *collectors.CollectorConfig) (collectors.Collector, error) {
	dnsConfig := DefaultConfig()
	
	if config != nil {
		dnsConfig.BufferSize = config.BufferSize
		if config.Labels != nil {
			dnsConfig.Labels = config.Labels
		}
	}
	
	return NewCollector("dns", dnsConfig)
}