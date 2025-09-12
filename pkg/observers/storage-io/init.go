package storageio

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/observers"
	"github.com/yairfalse/tapio/pkg/observers/base"
	"github.com/yairfalse/tapio/pkg/observers/orchestrator"
	"go.uber.org/zap"
)

func init() {
	base.RegisterObserver("storage-io", Factory)
}

func Factory(name string, config *orchestrator.ObserverConfigData, logger *zap.Logger) (observers.Observer, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required for storage-io observer")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required for storage-io observer")
	}

	// Create typed config from orchestrator config
	cfg := &Config{
		EnableEBPF:            true,
		RingBufferSize:        config.BufferSize,
		BufferSize:            config.BufferSize,
		SlowIOThresholdMs:     100,  // 100ms slow I/O threshold
		BlockingIOThresholdMs: 1000, // 1s blocking I/O threshold
		MonitoredK8sPaths:     []string{"/var/lib/kubelet", "/var/log/pods"},
		EnableK8sIntegration:  true,
		EnableMetrics:         true,
		EnableProfiling:       false, // Disabled by default for performance
		MaxEventsPerSecond:    1000,
		CacheCleanupInterval:  5 * time.Minute,
	}

	observer, err := NewObserver(name, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage-io observer: %w", err)
	}

	return observer, nil
}
