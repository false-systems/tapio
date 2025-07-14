package collectors

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/types"
	"github.com/yairfalse/tapio/pkg/ebpf"
	"github.com/yairfalse/tapio/pkg/logging"
)

// EBPFAdapter provides a cross-platform interface to eBPF functionality
type EBPFAdapter struct {
	config   types.CollectorConfig
	logger   *logging.Logger
	monitor  ebpf.Monitor
	platform Platform
	enabled  bool
	eventCh  chan *types.Event
}

// NewEBPFAdapter creates a new eBPF adapter that works on all platforms
func NewEBPFAdapter() (*EBPFAdapter, error) {
	logger := logging.WithComponent("ebpf-adapter")
	platform := GetCurrentPlatform()
	
	adapter := &EBPFAdapter{
		logger:   logger,
		platform: platform,
		eventCh:  make(chan *types.Event, 1000),
	}

	// Create monitor based on platform
	if platform.HasEBPF {
		adapter.monitor = ebpf.NewMonitor(ebpf.DefaultConfig())
		adapter.enabled = true
		logger.Info("eBPF adapter initialized with native Linux support")
	} else {
		adapter.monitor = ebpf.NewMonitor(ebpf.DefaultConfig()) // This will be a stub
		adapter.enabled = false
		logger.Info("eBPF adapter initialized with stub implementation", 
			"platform", platform.OS,
			"message", GetPlatformMessage("ebpf"))
	}

	return adapter, nil
}

// Name returns the adapter name
func (a *EBPFAdapter) Name() string {
	return "ebpf-adapter"
}

// Type returns the adapter type
func (a *EBPFAdapter) Type() string {
	return "ebpf"
}

// Configure configures the eBPF adapter
func (a *EBPFAdapter) Configure(config types.CollectorConfig) error {
	a.config = config
	
	// On non-Linux platforms, we accept the configuration but disable functionality
	if !a.platform.HasEBPF {
		a.logger.Warn("eBPF configuration accepted but functionality is disabled on this platform",
			"platform", a.platform.OS)
		return nil
	}

	// Configure the actual eBPF monitor on Linux
	if ebpfConfig, ok := config.Extra["ebpf_config"]; ok {
		if cfg, ok := ebpfConfig.(*ebpf.Config); ok {
			a.monitor = ebpf.NewMonitor(cfg)
		}
	}

	return nil
}

// Start starts the eBPF adapter
func (a *EBPFAdapter) Start(ctx context.Context) error {
	if !a.platform.HasEBPF {
		a.logger.Info("eBPF adapter started in stub mode (no-op)")
		go a.generateMockEvents(ctx)
		return nil
	}

	if !a.enabled {
		return fmt.Errorf("eBPF adapter is disabled")
	}

	// Start the actual eBPF monitor
	if err := a.monitor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start eBPF monitor: %w", err)
	}

	a.logger.Info("eBPF adapter started successfully")
	return nil
}

// Stop stops the eBPF adapter
func (a *EBPFAdapter) Stop() error {
	if !a.platform.HasEBPF {
		a.logger.Info("eBPF adapter stopped (stub mode)")
		return nil
	}

	if err := a.monitor.Stop(); err != nil {
		return fmt.Errorf("failed to stop eBPF monitor: %w", err)
	}

	close(a.eventCh)
	a.logger.Info("eBPF adapter stopped successfully")
	return nil
}

// Events returns the event channel
func (a *EBPFAdapter) Events() <-chan *types.Event {
	return a.eventCh
}

// Health returns the adapter health status
func (a *EBPFAdapter) Health() *types.Health {
	if !a.platform.HasEBPF {
		return &types.Health{
			Status:  types.HealthStatusHealthy,
			Message: fmt.Sprintf("eBPF adapter running in stub mode on %s", a.platform.OS),
			Metrics: map[string]interface{}{
				"platform": a.platform.OS,
				"mode":     "stub",
				"native":   false,
			},
		}
	}

	// Check actual eBPF monitor health
	if !a.monitor.IsAvailable() {
		return &types.Health{
			Status:  types.HealthStatusUnhealthy,
			Message: "eBPF monitor is not available",
			Metrics: map[string]interface{}{
				"platform": a.platform.OS,
				"mode":     "native",
				"native":   true,
				"error":    a.monitor.GetLastError(),
			},
		}
	}

	return &types.Health{
		Status:  types.HealthStatusHealthy,
		Message: "eBPF adapter is healthy",
		Metrics: map[string]interface{}{
			"platform": a.platform.OS,
			"mode":     "native",
			"native":   true,
		},
	}
}

// GetStats returns adapter statistics
func (a *EBPFAdapter) GetStats() *types.Stats {
	return &types.Stats{
		EventsCollected: 0, // TODO: Implement proper statistics
		EventsDropped:   0,
		EventsFiltered:  0,
		ErrorCount:      0,
		StartTime:       time.Now(), // TODO: Track actual start time
		LastEventTime:   time.Now(),
		Custom: map[string]interface{}{
			"platform":       a.platform.OS,
			"ebpf_available": a.platform.HasEBPF,
			"native_mode":    a.platform.HasEBPF && a.enabled,
		},
	}
}

// generateMockEvents generates mock events for development on non-Linux platforms
func (a *EBPFAdapter) generateMockEvents(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Generate a mock event to show the adapter is working
			event := &types.Event{
				ID:        fmt.Sprintf("mock-ebpf-%d", time.Now().Unix()),
				Type:      types.EventTypeLog,
				Category:  types.CategorySystem,
				Severity:  types.SeverityInfo,
				Timestamp: time.Now(),
				Source: types.EventSource{
					Collector: "ebpf-adapter",
					Component: "mock",
					Node:      "localhost",
				},
				Data: map[string]interface{}{
					"message": fmt.Sprintf("Mock eBPF event generated on %s", runtime.GOOS),
					"platform": runtime.GOOS,
					"mode":     "development",
				},
				Attributes: map[string]interface{}{
					"mock":     true,
					"platform": runtime.GOOS,
				},
				Labels: map[string]string{
					"source": "ebpf-mock",
					"mode":   "development",
				},
				Context: &types.EventContext{
					Hostname: "localhost",
				},
			}

			select {
			case a.eventCh <- event:
			default:
				// Drop if channel is full
			}
		}
	}
}

// GetMemoryStats returns memory statistics (cross-platform)
func (a *EBPFAdapter) GetMemoryStats() ([]ebpf.ProcessMemoryStats, error) {
	if !a.platform.HasEBPF {
		// Return mock data for development
		return []ebpf.ProcessMemoryStats{
			{
				PID:            1,
				Command:        "mock-process",
				TotalAllocated: 1024 * 1024, // 1MB
				CurrentUsage:   512 * 1024,  // 512KB
				LastUpdate:     time.Now(),
				InContainer:    false,
			},
		}, nil
	}

	return a.monitor.GetMemoryStats()
}

// GetMemoryPredictions returns OOM predictions (cross-platform)
func (a *EBPFAdapter) GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*ebpf.OOMPrediction, error) {
	if !a.platform.HasEBPF {
		// Return mock predictions for development
		return map[uint32]*ebpf.OOMPrediction{
			1: {
				PID:                1,
				TimeToOOM:          time.Hour,
				Confidence:         0.1, // Low confidence for mock
				CurrentUsage:       512 * 1024,
				MemoryLimit:        1024 * 1024,
				PredictedPeakUsage: 600 * 1024,
			},
		}, nil
	}

	return a.monitor.GetMemoryPredictions(limits)
}

// IsAvailable returns whether eBPF is available
func (a *EBPFAdapter) IsAvailable() bool {
	return a.platform.HasEBPF && a.monitor.IsAvailable()
}

// GetLastError returns the last error
func (a *EBPFAdapter) GetLastError() error {
	return a.monitor.GetLastError()
}