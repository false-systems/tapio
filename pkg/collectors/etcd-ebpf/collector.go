package etcdebpf

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Collector implements minimal etcd syscall monitoring via eBPF
type Collector struct {
	name      string
	config    Config
	events    chan *domain.CollectorEvent
	ctx       context.Context
	cancel    context.CancelFunc
	healthy   bool
	mu        sync.RWMutex
	ebpfState interface{} // Platform-specific eBPF state
	stats     CollectorStats
	startTime time.Time
	logger    *zap.Logger

	// OTEL instrumentation - 5 Core Metrics (MANDATORY)
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Int64Gauge

	// eBPF-specific metrics
	syscallsMonitored metric.Int64Counter
	processesTracked  metric.Int64Gauge
}

// NewCollector creates a new minimal etcd eBPF collector
func NewCollector(name string, config Config) (*Collector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create logger if not provided
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize OTEL components - MANDATORY pattern
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

	syscallsMonitored, err := meter.Int64Counter(
		fmt.Sprintf("%s_syscalls_monitored_total", name),
		metric.WithDescription(fmt.Sprintf("Total syscalls monitored by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create syscalls counter", zap.Error(err))
	}

	processesTracked, err := meter.Int64Gauge(
		fmt.Sprintf("%s_processes_tracked", name),
		metric.WithDescription(fmt.Sprintf("Number of etcd processes tracked by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create processes gauge", zap.Error(err))
	}

	return &Collector{
		name:              name,
		config:            config,
		events:            make(chan *domain.CollectorEvent, config.BufferSize),
		healthy:           true,
		startTime:         time.Now(),
		logger:            logger,
		tracer:            tracer,
		eventsProcessed:   eventsProcessed,
		errorsTotal:       errorsTotal,
		processingTime:    processingTime,
		droppedEvents:     droppedEvents,
		bufferUsage:       bufferUsage,
		syscallsMonitored: syscallsMonitored,
		processesTracked:  processesTracked,
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins eBPF-based etcd syscall monitoring
func (c *Collector) Start(ctx context.Context) error {
	// Create span for startup
	ctx, span := c.tracer.Start(ctx, "etcd-ebpf.start")
	defer span.End()

	start := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		err := fmt.Errorf("collector already started")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	span.SetAttributes(
		attribute.Int("buffer_size", c.config.BufferSize),
		attribute.Int("process_discovery_interval", c.config.ProcessDiscoveryInterval),
		attribute.Int("pid_verification_timeout", c.config.PIDVerificationTimeout),
		attribute.Bool("capture_data_payload", c.config.CaptureDataPayload),
		attribute.Int("max_data_capture_size", c.config.MaxDataCaptureSize),
	)

	// Start eBPF monitoring - this is the core functionality
	if err := c.startEBPF(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_setup"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "eBPF setup failed")
		return fmt.Errorf("failed to start eBPF monitoring: %w", err)
	}

	// Record startup duration
	duration := time.Since(start)
	if c.processingTime != nil {
		c.processingTime.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("operation", "startup"),
		))
	}

	span.SetAttributes(
		attribute.Float64("startup_duration_seconds", duration.Seconds()),
	)

	c.healthy = true
	c.logger.Info("etcd eBPF collector started",
		zap.String("name", c.name),
		zap.Int("buffer_size", c.config.BufferSize),
		zap.Duration("startup_duration", duration))

	return nil
}

// Stop gracefully shuts down the eBPF collector
func (c *Collector) Stop() error {
	// Create span for shutdown
	ctx, span := c.tracer.Start(context.Background(), "etcd-ebpf.stop")
	defer span.End()

	start := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	span.AddEvent("Starting collector shutdown")

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
		span.AddEvent("Context cancelled")
	}

	// Stop eBPF monitoring
	c.stopEBPF()
	span.AddEvent("eBPF monitoring stopped")

	// Close events channel
	if c.events != nil {
		close(c.events)
		c.events = nil
		span.AddEvent("Events channel closed")
	}

	c.healthy = false

	// Record shutdown duration
	duration := time.Since(start)
	if c.processingTime != nil {
		c.processingTime.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("operation", "shutdown"),
		))
	}

	span.SetAttributes(
		attribute.Float64("shutdown_duration_ms", duration.Seconds()*1000),
	)

	c.logger.Info("etcd eBPF collector stopped",
		zap.String("name", c.name),
		zap.Duration("shutdown_duration", duration))

	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return c.healthy
}

// Health returns strongly-typed health information
func (c *Collector) Health() *HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	message := "eBPF collector running normally"
	if !c.healthy {
		message = "eBPF collector is unhealthy"
	}

	// Build component info map
	componentInfo := make(map[string]string)
	componentInfo["ebpf_active"] = fmt.Sprintf("%t", c.ebpfState != nil)
	componentInfo["instrumentation"] = "etcd-ebpf"
	componentInfo["buffer_size"] = fmt.Sprintf("%d", cap(c.events))
	componentInfo["buffer_available"] = fmt.Sprintf("%d", cap(c.events)-len(c.events))
	componentInfo["events_processed"] = fmt.Sprintf("%d", c.stats.EventsProcessed)
	componentInfo["error_count"] = fmt.Sprintf("%d", c.stats.ErrorCount)
	componentInfo["process_discovery_interval"] = fmt.Sprintf("%ds", c.config.ProcessDiscoveryInterval)
	componentInfo["pid_verification_timeout"] = fmt.Sprintf("%ds", c.config.PIDVerificationTimeout)
	componentInfo["capture_data_payload"] = fmt.Sprintf("%t", c.config.CaptureDataPayload)

	return &HealthStatus{
		Healthy:       c.healthy,
		Message:       message,
		LastCheck:     time.Now(),
		ComponentInfo: componentInfo,
	}
}

// Statistics returns strongly-typed collector statistics
func (c *Collector) Statistics() *CollectorStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Build custom metrics map
	customMetrics := make(map[string]string)
	customMetrics["collector_name"] = c.name
	customMetrics["service_name"] = "etcd-ebpf"
	customMetrics["buffer_capacity"] = fmt.Sprintf("%d", cap(c.events))
	customMetrics["buffer_current_size"] = fmt.Sprintf("%d", len(c.events))
	customMetrics["buffer_utilization"] = fmt.Sprintf("%.2f", float64(len(c.events))/float64(cap(c.events)))
	customMetrics["ebpf_enabled"] = fmt.Sprintf("%t", c.ebpfState != nil)
	customMetrics["process_discovery_interval"] = fmt.Sprintf("%d", c.config.ProcessDiscoveryInterval)
	customMetrics["capture_data_payload"] = fmt.Sprintf("%t", c.config.CaptureDataPayload)

	return &CollectorStats{
		EventsProcessed: c.stats.EventsProcessed,
		ErrorCount:      c.stats.ErrorCount,
		LastEventTime:   c.stats.LastEventTime,
		Uptime:          time.Since(c.startTime),
		CustomMetrics:   customMetrics,
	}
}
