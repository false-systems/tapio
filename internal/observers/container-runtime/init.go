package containerruntime

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/internal/observers"
	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/internal/observers/orchestrator"
	"go.uber.org/zap"
)

func init() {
	base.RegisterObserver("container-runtime", Factory)
}

func Factory(name string, config *orchestrator.ObserverConfigData, logger *zap.Logger) (observers.Observer, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required for container-runtime observer")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required for container-runtime observer")
	}

	// Create typed config from orchestrator config
	cfg := &Config{
		Name:                 name,
		BufferSize:           config.BufferSize,
		EnableOOMKill:        true,
		EnableMemoryPressure: true,
		EnableProcessExit:    true,
		EnableProcessFork:    false, // Disabled by default to reduce noise
		BPFProgramPinPath:    "",    // Default empty path
		BPFLogLevel:          0,     // Default no BPF logging
		MetricsInterval:      30 * time.Second,
		MetadataCacheSize:    1000, // Cache up to 1000 containers
		MetadataCacheTTL:     5 * time.Minute,
	}

	observer, err := NewObserver(name, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create container-runtime observer: %w", err)
	}

	return observer, nil
}
