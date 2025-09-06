package containerruntime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/observers/base"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// eBPF generation is handled by bpf/generate.go

// Observer implements eBPF-based container runtime monitoring using BaseObserver
// Provides real-time OOM detection and container process tracking
type Observer struct {
	*base.BaseObserver        // Embed for Statistics() and Health()
	*base.EventChannelManager // Embed for event channel management
	*base.LifecycleManager    // Embed for lifecycle management

	logger *zap.Logger
	config *Config
	mu     sync.RWMutex

	// eBPF components (Linux-only)
	ebpfState interface{}

	// Container metadata tracking
	containerCache map[string]*ContainerMetadata
	cacheMu        sync.RWMutex

	// CRI-specific metrics (beyond BaseObserver)
	oomKillsTotal   metric.Int64Counter
	memoryPressure  metric.Int64Counter
	processExits    metric.Int64Counter
	containerStarts metric.Int64Counter
}

// NewObserver creates a new Container Runtime observer
func NewObserver(name string, cfg *Config) (*Observer, error) {
	if cfg == nil {
		cfg = NewDefaultConfig(name)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Create logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize base components
	ctx := context.Background()
	baseObserver := base.NewBaseObserver("container-runtime", 5*time.Minute)
	eventManager := base.NewEventChannelManager(cfg.BufferSize, "container-runtime", logger)
	lifecycleManager := base.NewLifecycleManager(ctx, logger)

	// Get the meter from BaseObserver for consistency
	meter := baseObserver.GetMeter()

	// Create CRI-specific metrics using the same meter
	oomKillsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_oom_kills_total", name),
		metric.WithDescription(fmt.Sprintf("Total OOM kills detected by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create OOM kills counter", zap.Error(err))
	}

	memoryPressure, err := meter.Int64Counter(
		fmt.Sprintf("%s_memory_pressure_total", name),
		metric.WithDescription(fmt.Sprintf("Total memory pressure events by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create memory pressure counter", zap.Error(err))
	}

	processExits, err := meter.Int64Counter(
		fmt.Sprintf("%s_process_exits_total", name),
		metric.WithDescription(fmt.Sprintf("Total process exits detected by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create process exits counter", zap.Error(err))
	}

	containerStarts, err := meter.Int64Counter(
		fmt.Sprintf("%s_container_starts_total", name),
		metric.WithDescription(fmt.Sprintf("Total container starts detected by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create container starts counter", zap.Error(err))
	}

	c := &Observer{
		BaseObserver:        baseObserver,
		EventChannelManager: eventManager,
		LifecycleManager:    lifecycleManager,
		logger:              logger.Named(name),
		config:              cfg,
		containerCache:      make(map[string]*ContainerMetadata),
		oomKillsTotal:       oomKillsTotal,
		memoryPressure:      memoryPressure,
		processExits:        processExits,
		containerStarts:     containerStarts,
	}

	c.logger.Info("Container Runtime observer created",
		zap.String("name", c.GetName()),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.Bool("enable_oom_kill", cfg.EnableOOMKill),
		zap.Bool("enable_memory_pressure", cfg.EnableMemoryPressure),
	)

	return c, nil
}

// Name returns observer name
func (c *Observer) Name() string {
	return c.config.Name
}

// Start starts the eBPF monitoring
func (c *Observer) Start(ctx context.Context) error {
	tracer := c.BaseObserver.GetTracer()
	ctx, span := tracer.Start(ctx, "container-runtime.observer.start")
	defer span.End()

	// Context is managed by LifecycleManager

	// Start eBPF monitoring
	if err := c.startEBPF(); err != nil {
		c.BaseObserver.RecordError(err)
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Start event processing goroutine
	go c.processEvents()

	// Start metrics collection goroutine
	go c.collectMetrics()

	c.SetHealthy(true)
	c.logger.Info("Container Runtime observer started successfully")

	return nil
}

// Stop stops the observer
func (c *Observer) Stop() error {
	c.logger.Info("Stopping Container Runtime observer")

	// Shutdown lifecycle manager
	if err := c.LifecycleManager.Stop(5 * time.Second); err != nil {
		c.logger.Warn("Timeout during shutdown", zap.Error(err))
	}

	c.stopEBPF()
	c.EventChannelManager.Close()
	c.BaseObserver.SetHealthy(false)

	c.logger.Info("Container Runtime observer stopped successfully")
	return nil
}

// Events returns the event channel
func (c *Observer) Events() <-chan *domain.CollectorEvent {
	return c.EventChannelManager.GetChannel()
}

// Statistics delegates to base observer
func (c *Observer) Statistics() *domain.CollectorStats {
	return c.BaseObserver.Statistics()
}

// Health delegates to base observer
func (c *Observer) Health() *domain.HealthStatus {
	return c.BaseObserver.Health()
}

// loadEBPFPrograms loads the compiled eBPF programs
