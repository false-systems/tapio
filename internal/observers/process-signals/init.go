package processsignals

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/internal/observers"
	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/internal/observers/orchestrator"
	"go.uber.org/zap"
)

// init registers the process-signals observer factory
func init() {
	base.RegisterObserver("process-signals", Factory)
}

// Factory creates a new process-signals observer
func Factory(name string, config *orchestrator.ObserverConfigData, logger *zap.Logger) (observers.Observer, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required for process-signals observer")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required for process-signals observer")
	}

	// Create typed config from orchestrator config
	cfg := &Config{
		BufferSize:       config.BufferSize,
		EnableEBPF:       true, // Default to true for Linux
		EnableRingBuffer: true,
		RingBufferSize:   8192,
		BatchSize:        32,
		BatchTimeout:     10 * time.Millisecond,
		EnableFilters:    true,
		Logger:           logger,
	}

	observer, err := NewObserver(name, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create process-signals observer: %w", err)
	}

	return observer, nil
}
