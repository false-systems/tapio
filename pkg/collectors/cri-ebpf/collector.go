package criebpf

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// eBPF generation is handled by bpf/generate.go

// Collector implements eBPF-based container runtime monitoring using BaseCollector
// Provides real-time OOM detection and container process tracking
type Collector struct {
	*base.BaseCollector // Embed BaseCollector for standard functionality

	logger *zap.Logger
	events chan *domain.CollectorEvent
	ctx    context.Context
	cancel context.CancelFunc
	config *Config
	mu     sync.RWMutex

	// eBPF components (Linux-only)
	ebpfState interface{}

	// Container metadata tracking
	containerCache map[string]*ContainerMetadata
	cacheMu        sync.RWMutex

	// CRI-specific metrics (beyond BaseCollector)
	oomKillsTotal   metric.Int64Counter
	memoryPressure  metric.Int64Counter
	processExits    metric.Int64Counter
	containerStarts metric.Int64Counter
}

// NewCollector creates a new CRI eBPF collector
func NewCollector(name string, cfg *Config) (*Collector, error) {
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

	// Create BaseCollector with 5-minute health check timeout
	baseConfig := base.BaseCollectorConfig{
		Name:               name,
		HealthCheckTimeout: 5 * time.Minute,
		ErrorRateThreshold: 0.05, // 5% error rate threshold for CRI collector (stricter than default)
	}
	baseCollector := base.NewBaseCollectorWithConfig(baseConfig)

	// Get the meter from BaseCollector for consistency
	meter := baseCollector.GetMeter()

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

	c := &Collector{
		BaseCollector:   baseCollector, // Use BaseCollector
		logger:          logger.Named(name),
		config:          cfg,
		events:          make(chan *domain.CollectorEvent, cfg.BufferSize),
		containerCache:  make(map[string]*ContainerMetadata),
		oomKillsTotal:   oomKillsTotal,
		memoryPressure:  memoryPressure,
		processExits:    processExits,
		containerStarts: containerStarts,
	}

	c.logger.Info("CRI eBPF collector created",
		zap.String("name", c.GetName()),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.Bool("enable_oom_kill", cfg.EnableOOMKill),
		zap.Bool("enable_memory_pressure", cfg.EnableMemoryPressure),
	)

	return c, nil
}

// Name returns collector name (required by Collector interface)
func (c *Collector) Name() string {
	return c.GetName()
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.StartSpan(ctx, "cri-ebpf.collector.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start eBPF monitoring
	if err := c.startEBPF(); err != nil {
		c.RecordErrorWithContext(ctx, err)
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Start event processing goroutine
	go c.processEvents()

	// Start metrics collection goroutine
	go c.collectMetrics()

	c.SetHealthy(true)
	c.logger.Info("CRI eBPF collector started successfully")

	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	c.logger.Info("Stopping CRI eBPF collector")

	if c.cancel != nil {
		c.cancel()
	}

	c.stopEBPF()
	close(c.events)
	c.SetHealthy(false)

	c.logger.Info("CRI eBPF collector stopped successfully")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status (delegates to BaseCollector)
func (c *Collector) IsHealthy() bool {
	return c.BaseCollector.IsHealthy()
}

// loadEBPFPrograms loads the compiled eBPF programs
