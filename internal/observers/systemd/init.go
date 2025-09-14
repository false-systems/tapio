package systemd

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/internal/observers"
	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/internal/observers/orchestrator"
	"go.uber.org/zap"
)

func init() {
	base.RegisterObserver("systemd", Factory)
}

func Factory(name string, config *orchestrator.ObserverConfigData, logger *zap.Logger) (observers.Observer, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required for systemd observer")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required for systemd observer")
	}

	// Create typed config from orchestrator config
	cfg := &Config{
		BufferSize:           config.BufferSize,
		EnableEBPF:           true,
		EnableJournal:        true,
		ServicePatterns:      []string{}, // Monitor all services by default
		MonitorServiceStates: true,
		MonitorCgroups:       true,
		RateLimitPerSecond:   1000, // Rate limit to 1000 events per second
		HealthCheckInterval:  30 * time.Second,
		Logger:               logger,
	}

	observer, err := NewObserver(name, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create systemd observer: %w", err)
	}

	return observer, nil
}
