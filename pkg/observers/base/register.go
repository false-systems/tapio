package base

import (
	"github.com/yairfalse/tapio/pkg/observers/orchestrator"
)

// RegisterObserver registers an observer factory with the orchestrator.
// This eliminates boilerplate in each observer's init.go.
// Usage in observer's init.go:
//
//	func init() {
//	    base.RegisterObserver("dns", CreateFactory())
//	}
func RegisterObserver(name string, factory orchestrator.ObserverFactory) {
	orchestrator.RegisterObserverFactory(name, factory)
}

// ObserverInitTemplate provides a template for observer init.go files.
// Copy this to your observer package and modify:
const ObserverInitTemplate = `
package YOURPACKAGE

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/observers"
	"github.com/yairfalse/tapio/pkg/observers/base"
	"github.com/yairfalse/tapio/pkg/observers/orchestrator"
	"go.uber.org/zap"
)

func init() {
	base.RegisterObserver("OBSERVER_NAME", Factory)
}

func Factory(name string, config *orchestrator.ObserverConfigData, logger *zap.Logger) (observers.Observer, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required for OBSERVER_NAME observer")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required for OBSERVER_NAME observer")
	}

	// Create typed config from orchestrator config
	cfg := &Config{
		BufferSize: config.BufferSize,
		Logger:     logger,
		// Add your specific fields here
	}
	
	observer, err := NewObserver(name, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create OBSERVER_NAME observer: %w", err)
	}
	
	return observer, nil
}
`

// ObserverStructTemplate shows how to structure an observer using base components
const ObserverStructTemplate = `
type Observer struct {
	*base.BaseObserver        // Provides Statistics() and Health()
	*base.EventChannelManager // Handles event channel
	*base.LifecycleManager    // Manages goroutines
	
	name   string
	config *Config
	logger *zap.Logger
	
	// Your observer-specific fields here
}

func NewObserver(name string, config *Config) (*Observer, error) {
	baseConfig := base.BaseObserverConfig{
		Name:               name,
		HealthCheckTimeout: 5 * time.Minute,
		ErrorRateThreshold: 0.1,
		EnableRingBuffer:   config.EnableRingBuffer,
		RingBufferSize:     config.RingBufferSize,
		Logger:             config.Logger,
	}
	
	o := &Observer{
		BaseObserver:        base.NewBaseObserverWithConfig(baseConfig),
		EventChannelManager: base.NewEventChannelManager(config.BufferSize, name, config.Logger),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), config.Logger),
		name:                name,
		config:              config,
		logger:              config.Logger,
	}
	
	return o, nil
}
`
