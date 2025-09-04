package systemd

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// SystemdEvent represents a systemd event from eBPF - must match C struct
type SystemdEvent struct {
	Timestamp   uint64
	PID         uint32
	PPID        uint32
	UID         uint32
	GID         uint32
	CgroupID    uint64
	EventType   uint8
	Pad         [3]uint8
	Comm        [16]byte
	ServiceName [64]byte
	CgroupPath  [256]byte
	ExitCode    uint32
	Signal      uint32
}

// Collector implements simple systemd monitoring via eBPF
type Collector struct {
	*base.BaseCollector       // Provides Statistics() and Health()
	*base.EventChannelManager // Handles event channels
	*base.LifecycleManager    // Manages goroutines

	// Core configuration
	config Config
	logger *zap.Logger

	// eBPF components (platform-specific)
	ebpfState interface{}

	// Systemd-specific OTEL components
	tracer         trace.Tracer
	processingTime metric.Float64Histogram
	bufferUsage    metric.Int64Gauge
}

// NewCollector creates a new simple systemd collector
func NewCollector(name string, cfg Config, logger *zap.Logger) (*Collector, error) {
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	// Initialize base components
	baseConfig := base.BaseCollectorConfig{
		Name:               name,
		HealthCheckTimeout: 30 * time.Second,
		ErrorRateThreshold: 0.05,
		Logger:             logger,
	}

	baseCollector := base.NewBaseCollectorWithConfig(baseConfig)
	eventManager := base.NewEventChannelManager(cfg.BufferSize, name, logger)
	lifecycle := base.NewLifecycleManager(context.Background(), logger)

	// Initialize systemd-specific OTEL components
	tracer := otel.Tracer("systemd-collector")
	meter := otel.Meter("systemd-collector")

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription(fmt.Sprintf("Current buffer usage for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	c := &Collector{
		BaseCollector:       baseCollector,
		EventChannelManager: eventManager,
		LifecycleManager:    lifecycle,
		logger:              logger.Named(name),
		tracer:              tracer,
		config:              cfg,
		processingTime:      processingTime,
		bufferUsage:         bufferUsage,
	}

	c.logger.Info("Systemd collector created",
		zap.String("name", name),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.Bool("enable_ebpf", cfg.EnableEBPF),
	)

	return c, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.BaseCollector.GetName()
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "systemd.collector.start")
	defer span.End()

	c.logger.Info("Starting systemd collector", zap.Bool("enable_ebpf", c.config.EnableEBPF))

	// Start platform-specific monitoring if enabled
	if c.config.EnableEBPF {
		if err := c.startMonitoring(); err != nil {
			span.SetAttributes(attribute.String("error", "monitoring_start_failed"))
			return fmt.Errorf("failed to start monitoring: %w", err)
		}
	}

	c.BaseCollector.SetHealthy(true)
	c.logger.Info("Systemd collector started successfully")
	span.SetAttributes(attribute.Bool("success", true))
	return nil
}

// Stop stops the collector idempotently
func (c *Collector) Stop() error {
	c.logger.Info("Stopping systemd collector")

	// Stop lifecycle manager with timeout
	c.LifecycleManager.Stop(30 * time.Second)

	// Stop platform-specific monitoring if running
	c.stopMonitoring()

	// Close event channel
	c.EventChannelManager.Close()

	c.BaseCollector.SetHealthy(false)
	c.logger.Info("Systemd collector stopped successfully")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.EventChannelManager.GetChannel()
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return c.BaseCollector.IsHealthy()
}
