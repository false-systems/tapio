package dns

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

// Config for DNS collector
type Config struct {
	Name       string
	BufferSize int
	EnableEBPF bool
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		Name:       "dns",
		BufferSize: 10000,
		EnableEBPF: true,
	}
}

// RawDNSEvent represents the DNS event from eBPF - must match C struct exactly
type RawDNSEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	EventType uint8
	Protocol  uint8
	SrcPort   uint16
	DstPort   uint16
	QueryName [128]byte
	Data      [512]byte
}

// Collector implements simple DNS monitoring via eBPF
type Collector struct {
	// Core
	name    string
	logger  *zap.Logger
	config  Config
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	mu      sync.RWMutex

	// eBPF components (platform-specific)
	ebpfState interface{}

	// Event processing
	events chan domain.RawEvent

	// Minimal OpenTelemetry
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	bufferUsage     metric.Int64Gauge
	droppedEvents   metric.Int64Counter
}

// NewCollector creates a new DNS collector
func NewCollector(name string, cfg Config) (*Collector, error) {
	// Initialize logger if not provided
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize minimal OTEL components
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Only essential metrics
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total DNS events processed by %s", name)),
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
		metric.WithDescription(fmt.Sprintf("DNS processing duration in milliseconds for %s", name)),
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

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total dropped DNS events by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	return &Collector{
		name:            name,
		logger:          logger,
		config:          cfg,
		events:          make(chan domain.RawEvent, cfg.BufferSize),
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		bufferUsage:     bufferUsage,
		droppedEvents:   droppedEvents,
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	// Create span for startup
	ctx, span := c.tracer.Start(ctx, "dns.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	if !c.config.EnableEBPF {
		c.healthy = true
		return nil
	}

	// Start eBPF monitoring using platform-specific implementation
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
	c.logger.Info("DNS collector started",
		zap.String("name", c.name),
		zap.Bool("ebpf_enabled", c.config.EnableEBPF),
		zap.Int("buffer_size", c.config.BufferSize),
	)
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	// Stop eBPF if running
	c.stopEBPF()

	// Close events channel
	if c.events != nil {
		close(c.events)
	}
	c.healthy = false
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
