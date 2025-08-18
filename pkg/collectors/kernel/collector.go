package kernel

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

// Collector implements efficient kernel monitoring via eBPF
type Collector struct {
	name    string
	logger  *zap.Logger
	events  chan domain.RawEvent
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	mu      sync.RWMutex

	// eBPF components (platform-specific)
	ebpfState interface{}

	// Minimal OTEL instrumentation
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	bufferUsage     metric.Int64Gauge
	droppedEvents   metric.Int64Counter
}

// NewCollector creates a new kernel collector
func NewCollector(name string) (*Collector, error) {
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}
	config := &Config{Name: name}
	return NewCollectorWithConfig(config, logger)
}

// NewCollectorWithLogger creates a new kernel collector with logger
func NewCollectorWithConfig(config *Config, logger *zap.Logger) (*Collector, error) {
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	// Initialize minimal OTEL components
	tracer := otel.Tracer(config.Name)
	meter := otel.Meter(config.Name)

	// Only essential metrics
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", config.Name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", config.Name)),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", config.Name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", config.Name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", config.Name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", config.Name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", config.Name),
		metric.WithDescription(fmt.Sprintf("Current buffer usage for %s", config.Name)),
	)
	if err != nil {
		logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", config.Name),
		metric.WithDescription(fmt.Sprintf("Total dropped events by %s", config.Name)),
	)
	if err != nil {
		logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	c := &Collector{
		name:            config.Name,
		logger:          logger,
		events:          make(chan domain.RawEvent, DefaultEventBufferSize),
		healthy:         true,
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		bufferUsage:     bufferUsage,
		droppedEvents:   droppedEvents,
	}

	return c, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the kernel monitoring
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "kernel.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start eBPF monitoring
	if err := c.startEBPF(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_start_failed"),
			))
		}
		span.RecordError(err)
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Start event processing loop
	go c.readEBPFEvents()

	c.healthy = true
	c.logger.Info("Kernel collector started", zap.String("name", c.name))
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	// Stop eBPF if running
	c.stopEBPF()

	if c.events != nil {
		close(c.events)
	}

	c.healthy = false
	c.logger.Info("Kernel collector stopped")
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
