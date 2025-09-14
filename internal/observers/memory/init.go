package memory

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/internal/observers"
	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/internal/observers/orchestrator"
	"go.uber.org/zap"
)

func init() {
	base.RegisterObserver("memory", Factory)
}

func Factory(name string, config *orchestrator.ObserverConfigData, logger *zap.Logger) (observers.Observer, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required for memory observer")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required for memory observer")
	}

	// Create typed config from orchestrator config
	cfg := &Config{
		Name:               name,
		BufferSize:         config.BufferSize,
		EnableEBPF:         true,
		Mode:               ModeGrowthDetection, // Default mode
		MinAllocationSize:  10 * 1024,           // 10KB minimum allocation
		MinUnfreedAge:      30 * time.Second,
		SamplingRate:       1, // Track all allocations by default
		MaxEventsPerSec:    1000,
		StackDedupWindow:   5 * time.Second,
		TargetPID:          0,                 // Track all processes
		TargetDuration:     0,                 // No time limit
		TargetCGroupID:     0,                 // Track all containers
		RSSGrowthThreshold: 100 * 1024 * 1024, // 100MB RSS growth
		RSSCheckInterval:   10 * time.Second,
		LibCPath:           "/lib/x86_64-linux-gnu/libc.so.6", // Default libc path
	}

	observer, err := NewObserver(name, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create memory observer: %w", err)
	}

	return observer, nil
}
