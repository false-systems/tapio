package dns

import (
	"fmt"

	"github.com/yairfalse/tapio/internal/observers"
	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/internal/observers/orchestrator"
	"go.uber.org/zap"
)

func init() {
	base.RegisterObserver("dns", Factory)
}

func Factory(name string, config *orchestrator.ObserverConfigData, logger *zap.Logger) (observers.Observer, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required for dns observer")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required for dns observer")
	}

	// Create typed config from orchestrator config
	cfg := Config{
		Name:                  name,
		BufferSize:            config.BufferSize,
		EnableEBPF:            true,
		XDPInterfaces:         nil, // Auto-detect
		CircuitBreakerConfig:  DefaultCircuitBreakerConfig(),
		ContainerIDExtraction: true,
		ParseAnswers:          true,
	}

	observer, err := NewObserver(name, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create dns observer: %w", err)
	}

	return observer, nil
}
