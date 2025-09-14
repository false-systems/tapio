package health

import (
	"fmt"

	"github.com/yairfalse/tapio/internal/observers"
	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/internal/observers/orchestrator"
	"go.uber.org/zap"
)

func init() {
	base.RegisterObserver("health", Factory)
}

func Factory(name string, config *orchestrator.ObserverConfigData, logger *zap.Logger) (observers.Observer, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required for health observer")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required for health observer")
	}

	// Create typed config from orchestrator config
	cfg := &Config{
		RingBufferSize:   config.BufferSize,
		EventChannelSize: config.BufferSize,
		RateLimitMs:      100, // Default rate limit
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": true,
			"memory":  true,
		},
		RequireAllMetrics: false, // Default to graceful degradation
	}

	observer, err := NewObserver(logger, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create health observer: %w", err)
	}

	return observer, nil
}
