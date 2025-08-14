package kernel

import (
	"context"
	"fmt"
	"sync"

	"github.com/yairfalse/tapio/pkg/collectors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// ModularCollector implements modular kernel monitoring via eBPF
type ModularCollector struct {
	name        string
	logger      *zap.Logger
	events      chan collectors.RawEvent
	ctx         context.Context
	cancel      context.CancelFunc
	healthy     bool
	mu          sync.RWMutex
	config      *Config

	// eBPF components (platform-specific)
	ebpfState interface{}

	// OTEL instrumentation - REQUIRED fields
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	eventsDropped   metric.Int64Counter
	ebpfOperations  metric.Int64Counter
	activeModules   metric.Int64UpDownCounter
}

// NewModularCollector creates a new modular kernel collector
func NewModularCollector(name string) (*ModularCollector, error) {
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}
	config := &Config{Name: name}
	return NewModularCollectorWithConfig(config, logger)
}

// NewModularCollectorWithConfig creates a new modular kernel collector with config
func NewModularCollectorWithConfig(config *Config, logger *zap.Logger) (*ModularCollector, error) {
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	// Initialize OTEL components - MANDATORY pattern
	name := config.Name
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics with descriptive names and descriptions
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	eventsDropped, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_dropped_total", name),
		metric.WithDescription(fmt.Sprintf("Total events dropped by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events dropped counter", zap.Error(err))
	}

	ebpfOperations, err := meter.Int64Counter(
		fmt.Sprintf("%s_ebpf_operations_total", name),
		metric.WithDescription(fmt.Sprintf("Total eBPF operations in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf operations counter", zap.Error(err))
	}

	activeModules, err := meter.Int64UpDownCounter(
		fmt.Sprintf("%s_active_modules", name),
		metric.WithDescription(fmt.Sprintf("Active modules in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create active modules gauge", zap.Error(err))
	}

	c := &ModularCollector{
		name:            config.Name,
		logger:          logger,
		config:          config,
		events:          make(chan collectors.RawEvent, 15000), // Larger buffer for all modules
		healthy:         true,
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		eventsDropped:   eventsDropped,
		ebpfOperations:  ebpfOperations,
		activeModules:   activeModules,
	}

	return c, nil
}

// Name returns collector name
func (c *ModularCollector) Name() string {
	return c.name
}

// Start starts the modular kernel monitoring
func (c *ModularCollector) Start(ctx context.Context) error {
	// Create span for startup
	ctx, span := c.tracer.Start(ctx, "kernel.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start eBPF monitoring using platform-specific implementation
	if err := c.startEBPF(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_start_failed"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Start event processing loop
	go c.readEBPFEvents()

	c.healthy = true
	span.SetStatus(codes.Ok, "Kernel collector started successfully")
	c.logger.Info("Kernel collector started",
		zap.String("name", c.name),
	)
	return nil
}

// Stop stops the modular collector
func (c *ModularCollector) Stop() error {
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
func (c *ModularCollector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *ModularCollector) IsHealthy() bool {
	return c.healthy
}

