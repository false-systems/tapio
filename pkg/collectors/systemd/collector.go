package systemd

import (
	"context"
	"fmt"
	"sync"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// SystemdEvent represents a systemd event from eBPF
type SystemdEvent struct {
	Timestamp uint64
	PID       uint32
	PPID      uint32
	EventType uint32
	ExitCode  uint32
	Comm      [16]byte
	Filename  [256]byte
}

// Collector implements simple systemd monitoring via eBPF
type Collector struct {
	name    string
	logger  *zap.Logger
	tracer  trace.Tracer
	events  chan domain.RawEvent
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	config  Config
	mu      sync.RWMutex

	// eBPF components (platform-specific)
	ebpfState interface{}

	// Minimal OTEL Metrics
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
}

// NewCollector creates a new simple systemd collector
func NewCollector(name string, cfg Config) (*Collector, error) {
	// Create logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize minimal OTEL components
	tracer := otel.Tracer("systemd-collector")
	meter := otel.Meter("systemd-collector")

	// Only essential metrics
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events processed counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	c := &Collector{
		name:            name,
		logger:          logger.Named(name),
		tracer:          tracer,
		config:          cfg,
		events:          make(chan domain.RawEvent, cfg.BufferSize),
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
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
	return c.name
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "systemd.collector.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)
	c.logger.Info("Starting systemd collector", zap.Bool("enable_ebpf", c.config.EnableEBPF))

	// Start eBPF monitoring if enabled
	if c.config.EnableEBPF {
		if err := c.startEBPF(); err != nil {
			span.SetAttributes(attribute.String("error", "ebpf_start_failed"))
			return fmt.Errorf("failed to start eBPF: %w", err)
		}

		// Start event processing
		go c.processEvents()
	}

	c.healthy = true
	c.logger.Info("Systemd collector started successfully")
	span.SetAttributes(attribute.Bool("success", true))
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	c.logger.Info("Stopping systemd collector")

	if c.cancel != nil {
		c.cancel()
	}

	// Stop eBPF if running
	c.stopEBPF()

	close(c.events)
	c.healthy = false

	c.logger.Info("Systemd collector stopped successfully")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan domain.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return c.healthy
}
