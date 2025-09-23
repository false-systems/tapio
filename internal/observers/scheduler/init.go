package scheduler

import (
	"fmt"

	"github.com/yairfalse/tapio/internal/observers"
	"github.com/yairfalse/tapio/internal/observers/orchestrator"
	"github.com/yairfalse/tapio/internal/observers/registration"
	"go.uber.org/zap"
)

func init() {
	registration.RegisterObserver("scheduler", Factory)
}

func Factory(name string, config *orchestrator.ObserverConfigData, logger *zap.Logger) (observers.Observer, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required for scheduler observer")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required for scheduler observer")
	}

	// Create typed config from orchestrator config
	cfg := &Config{
		SchedDelayThresholdMs:  10,  // 10ms scheduling delay threshold
		ThrottleThresholdMs:    100, // 100ms throttle threshold
		MigrationThreshold:     10,  // Migrations per second
		NoiseNeighborThreshold: 0.8, // CPU monopolization ratio
		RingBufferSize:         config.BufferSize,
		EventChannelSize:       config.BufferSize,
		EnableStackTraces:      false, // Disabled by default for performance
		EnablePatternDetect:    true,
		EnableNoiseDetection:   true,
	}

	observer, err := NewObserver(name, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduler observer: %w", err)
	}

	return observer, nil
}
