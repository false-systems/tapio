package servicemap

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
)

// init registers the service-map collector with the orchestrator
func init() {
	orchestrator.RegisterCollectorFactory("service-map", NewServiceMapCollector)
}

// NewServiceMapCollector creates a new service map collector for the orchestrator
func NewServiceMapCollector(name string, configData *orchestrator.CollectorConfigData, logger *zap.Logger) (collectors.Collector, error) {
	// Parse config data into our Config struct
	config := DefaultConfig()
	
	// Map common fields from CollectorConfigData
	if configData.BufferSize > 0 {
		config.BufferSize = configData.BufferSize
	}
	
	config.EnableEBPF = configData.EnableEBPF
	
	// Use default configuration - let the collector handle its own specialized config
	// The orchestrator only provides common fields like BufferSize and EnableEBPF
	
	// Create collector
	collector, err := NewCollector("service-map", config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create service-map collector: %w", err)
	}
	
	return collector, nil
}