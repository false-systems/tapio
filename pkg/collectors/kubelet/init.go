package kubelet

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
	"go.uber.org/zap"
)

func init() {
	registry.Register("kubelet", createKubeletCollector)
}

func createKubeletCollector(config map[string]interface{}) (collectors.Collector, error) {
	// Parse configuration
	cfg := DefaultConfig()

	if address, ok := config["address"].(string); ok {
		cfg.Address = address
	}

	if insecure, ok := config["insecure"].(bool); ok {
		cfg.Insecure = insecure
	}

	if clientCert, ok := config["client_cert"].(string); ok {
		cfg.ClientCert = clientCert
	}

	if clientKey, ok := config["client_key"].(string); ok {
		cfg.ClientKey = clientKey
	}

	if nodeName, ok := config["node_name"].(string); ok {
		cfg.NodeName = nodeName
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
	if n, ok := config["name"].(string); ok {
		name = n
	}

	collector, err := NewCollector(name, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubelet collector: %w", err)
	}

	return collector, nil
}
