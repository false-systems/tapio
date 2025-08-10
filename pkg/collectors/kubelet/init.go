package kubelet

import (
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
	"go.uber.org/zap"
)

func init() {
	// Register the Kubelet collector typed factory with error handling
	factory := NewKubeletFactory()
	if err := registry.RegisterTypedFactory("kubelet", factory); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register Kubelet typed factory: %v", err)
		log.Printf("Kubelet collector will not be available")
	}

	// Also register legacy factory for backward compatibility
	if err := registry.Register("kubelet", createKubeletCollector); err != nil {
		// Log error but don't panic - this allows the application to continue
		log.Printf("WARNING: failed to register Kubelet legacy factory: %v", err)
	}
}

// createKubeletCollector is the legacy factory function
// DEPRECATED: This is for backward compatibility. Use the typed factory instead.
func createKubeletCollector(configMap map[string]interface{}) (collectors.Collector, error) {
	// Parse configuration from map to proper Config struct
	cfg, err := parseConfigFromMap(configMap)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubelet configuration: %w", err)
	}

	// Create logger if not provided
	if cfg.Logger == nil {
		logger, err := zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
		cfg.Logger = logger
	}

	name := "kubelet"
	if n, ok := configMap["name"].(string); ok {
		name = n
	}

	collector, err := NewCollector(name, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubelet collector: %w", err)
	}

	return collector, nil
}

// parseConfigFromMap converts map[string]interface{} to proper Config struct
func parseConfigFromMap(configMap map[string]interface{}) (*Config, error) {
	cfg := DefaultConfig()

	if address, ok := configMap["address"].(string); ok {
		cfg.Address = address
	}

	if insecure, ok := configMap["insecure"].(bool); ok {
		cfg.Insecure = insecure
	}

	if clientCert, ok := configMap["client_cert"].(string); ok {
		cfg.ClientCert = clientCert
	}

	if clientKey, ok := configMap["client_key"].(string); ok {
		cfg.ClientKey = clientKey
	}

	if nodeName, ok := configMap["node_name"].(string); ok {
		cfg.NodeName = nodeName
	}

	// Parse duration fields if provided
	if metricsInterval, ok := configMap["metrics_interval"].(string); ok {
		duration, err := time.ParseDuration(metricsInterval)
		if err != nil {
			return nil, fmt.Errorf("invalid metrics_interval: %w", err)
		}
		cfg.MetricsInterval = duration
	}

	if statsInterval, ok := configMap["stats_interval"].(string); ok {
		duration, err := time.ParseDuration(statsInterval)
		if err != nil {
			return nil, fmt.Errorf("invalid stats_interval: %w", err)
		}
		cfg.StatsInterval = duration
	}

	return cfg, nil
}
