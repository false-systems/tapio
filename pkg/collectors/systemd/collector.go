//go:build linux

package systemd

import (
	"context"
	"fmt"
	"sync"
	"time"

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
	CgroupPath  [64]byte  // Reduced from 256 to fit BPF stack limit
	ExitCode    uint32
	Signal      uint32
}

// eBPFState is defined in collector_linux.go for Linux builds
// and as an empty struct for non-Linux builds to maintain compatibility

// Collector implements simple systemd monitoring via eBPF
type Collector struct {
	name    string
	logger  *zap.Logger
	tracer  trace.Tracer
	events  chan *domain.CollectorEvent
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	config  Config
	mu      sync.RWMutex
	wg      sync.WaitGroup // For graceful shutdown

	// eBPF components (platform-specific) - properly typed
	ebpfState *eBPFState

	// Essential OTEL Metrics (5 core metrics)
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Int64Gauge
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

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total dropped events by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription(fmt.Sprintf("Current buffer usage for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	c := &Collector{
		name:            name,
		logger:          logger.Named(name),
		tracer:          tracer,
		config:          cfg,
		events:          make(chan *domain.CollectorEvent, cfg.BufferSize),
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		droppedEvents:   droppedEvents,
		bufferUsage:     bufferUsage,
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

	c.mu.Lock()
	c.healthy = true
	c.mu.Unlock()

	c.logger.Info("Systemd collector started successfully")
	span.SetAttributes(attribute.Bool("success", true))
	return nil
}

// Stop stops the collector idempotently
func (c *Collector) Stop() error {
	c.mu.Lock()
	if !c.healthy {
		c.mu.Unlock()
		return nil // Already stopped
	}
	c.healthy = false
	c.mu.Unlock()

	c.logger.Info("Stopping systemd collector")

	// Signal shutdown
	if c.cancel != nil {
		c.cancel()
	}

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.logger.Info("All goroutines stopped gracefully")
	case <-time.After(5 * time.Second):
		c.logger.Warn("Timeout waiting for goroutines to stop")
	}

	// Stop eBPF if running
	c.stopEBPF()

	// Drain remaining events before closing channel
	drained := 0
	for {
		select {
		case <-c.events:
			drained++
		default:
			goto done
		}
	}
done:
	if drained > 0 {
		c.logger.Info("Drained remaining events", zap.Int("count", drained))
	}

	close(c.events)

	c.logger.Info("Systemd collector stopped successfully")
	return nil

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status (thread-safe)
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}
