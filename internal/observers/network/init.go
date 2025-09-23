package network

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/internal/observers"
	"github.com/yairfalse/tapio/internal/observers/orchestrator"
	"github.com/yairfalse/tapio/internal/observers/registration"
	"go.uber.org/zap"
)

func init() {
	registration.RegisterObserver("network", Factory)
}

func Factory(name string, config *orchestrator.ObserverConfigData, logger *zap.Logger) (observers.Observer, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required for network observer")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required for network observer")
	}

	// Create typed config from orchestrator config
	cfg := &Config{
		BufferSize:         config.BufferSize,
		FlushInterval:      5 * time.Second,
		EnableIPv4:         true,
		EnableIPv6:         true,
		EnableTCP:          true,
		EnableUDP:          true,
		EnableHTTP:         true,
		EnableHTTPS:        true,
		EnableDNS:          true,
		HTTPPorts:          []int{80, 8080, 3000},
		HTTPSPorts:         []int{443, 8443},
		DNSPort:            53,
		MaxEventsPerSecond: 1000,
		SamplingRate:       1.0, // 100% sampling by default
	}

	observer, err := NewObserver(name, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create network observer: %w", err)
	}

	return observer, nil
}
