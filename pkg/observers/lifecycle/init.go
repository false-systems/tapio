package lifecycle

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/observers"
	"github.com/yairfalse/tapio/pkg/observers/base"
	"github.com/yairfalse/tapio/pkg/observers/orchestrator"
	"go.uber.org/zap"
)

func init() {
	base.RegisterObserver("lifecycle", Factory)
}

func Factory(name string, config *orchestrator.ObserverConfigData, logger *zap.Logger) (observers.Observer, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required for lifecycle observer")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required for lifecycle observer")
	}

	// Create typed config from orchestrator config
	cfg := Config{
		BufferSize:       config.BufferSize,
		ResyncPeriod:     5 * time.Minute, // Resync with K8s API every 5 minutes
		TrackPods:        true,
		TrackDeployments: true,
		TrackNodes:       true,
		TrackServices:    true,
	}

	observer, err := NewObserver(logger, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create lifecycle observer: %w", err)
	}

	return observer, nil
}
