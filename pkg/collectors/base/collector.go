// Package base provides common functionality for all Tapio collectors
// This reduces code duplication and ensures consistent observability
package base

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// BaseCollector provides common statistics and health tracking for all collectors
// Embed this in your collector to get Statistics() and Health() methods automatically
type BaseCollector struct {
	// Basic info
	name      string
	startTime time.Time

	// Statistics tracking (atomic for thread safety)
	eventsProcessed atomic.Int64
	eventsDropped   atomic.Int64
	eventsFiltered  atomic.Int64 // New: tracks filtered events
	errorCount      atomic.Int64

	// Atomic values for complex types
	lastEventTime atomic.Value // stores time.Time
	lastError     atomic.Value // stores error

	// Health tracking
	isHealthy          atomic.Bool
	healthCheckTimeout time.Duration
	errorRateThreshold float64 // Configurable error rate threshold

	// OTEL instrumentation
	tracer trace.Tracer
	meter  metric.Meter

	// Standard OTEL metrics
	eventsProcessedCounter metric.Int64Counter
	eventsDroppedCounter   metric.Int64Counter
	eventsFilteredCounter  metric.Int64Counter // New metric
	errorCounter           metric.Int64Counter
	processingDuration     metric.Float64Histogram
	eventSizeHistogram     metric.Int64Histogram
	healthStatus           metric.Int64Gauge

	// Ring buffer support (optional)
	ringBuffer    *RingBuffer
	useRingBuffer bool

	// Filter support (optional)
	filterManager *FilterManager
	useFilters    bool
	logger        *zap.Logger // Need logger for filter manager
}

// BaseCollectorConfig holds configuration for BaseCollector
type BaseCollectorConfig struct {
	Name               string
	HealthCheckTimeout time.Duration
	ErrorRateThreshold float64 // Default 0.1 (10%)

	// Ring buffer configuration (optional)
	EnableRingBuffer bool
	RingBufferSize   int           // Must be power of 2
	BatchSize        int           // Events to process at once
	BatchTimeout     time.Duration // Max time to wait for batch

	// Filter configuration (optional)
	EnableFilters    bool
	FilterConfigPath string // Path to filter config file (YAML)

	// Logger
	Logger *zap.Logger
}

// NewBaseCollector creates a new base collector with the given name
// healthCheckTimeout determines how long without events before marking degraded
func NewBaseCollector(name string, healthCheckTimeout time.Duration) *BaseCollector {
	return NewBaseCollectorWithConfig(BaseCollectorConfig{
		Name:               name,
		HealthCheckTimeout: healthCheckTimeout,
		ErrorRateThreshold: 0.1, // Default 10%
	})
}

// NewBaseCollectorWithConfig creates a new base collector with full configuration
func NewBaseCollectorWithConfig(config BaseCollectorConfig) *BaseCollector {
	if config.ErrorRateThreshold == 0 {
		config.ErrorRateThreshold = 0.1 // Default 10%
	}

	bc := &BaseCollector{
		name:               config.Name,
		startTime:          time.Now(),
		healthCheckTimeout: config.HealthCheckTimeout,
		errorRateThreshold: config.ErrorRateThreshold,
		tracer:             otel.Tracer(config.Name),
		meter:              otel.Meter(config.Name),
		useRingBuffer:      config.EnableRingBuffer,
		useFilters:         config.EnableFilters,
		logger:             config.Logger,
	}
	bc.isHealthy.Store(true)
	bc.lastEventTime.Store(time.Now())

	// Initialize ring buffer if enabled
	if config.EnableRingBuffer {
		rbConfig := RingBufferConfig{
			Size:          config.RingBufferSize,
			BatchSize:     config.BatchSize,
			BatchTimeout:  config.BatchTimeout,
			CollectorName: config.Name,
			Logger:        config.Logger,
		}

		// Set defaults if not specified
		if rbConfig.Size == 0 {
			rbConfig.Size = 8192
		}
		if rbConfig.BatchSize == 0 {
			rbConfig.BatchSize = 32
		}
		if rbConfig.BatchTimeout == 0 {
			rbConfig.BatchTimeout = 10 * time.Millisecond
		}

		ringBuffer, err := NewRingBuffer(rbConfig)
		if err == nil {
			bc.ringBuffer = ringBuffer
		}
		// If ring buffer creation fails, fall back to channel-only mode
	}

	// Initialize filter manager if enabled
	if config.EnableFilters {
		bc.filterManager = NewFilterManager(config.Name, config.Logger)

		// Start watching config file if provided
		if config.FilterConfigPath != "" {
			if err := bc.filterManager.WatchConfigFile(config.FilterConfigPath); err != nil {
				if config.Logger != nil {
					config.Logger.Warn("Failed to watch filter config file",
						zap.String("collector", config.Name),
						zap.String("path", config.FilterConfigPath),
						zap.Error(err))
				}
			}
		}
	}

	// Initialize OTEL metrics
	bc.initializeMetrics()

	return bc
}

// initializeMetrics registers standard OTEL metrics for all collectors
func (bc *BaseCollector) initializeMetrics() {
	var err error

	// Events processed counter
	bc.eventsProcessedCounter, err = bc.meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", bc.name),
		metric.WithDescription("Total events processed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		// Log but don't fail - metrics are optional
		bc.eventsProcessedCounter = nil
	}

	// Events dropped counter
	bc.eventsDroppedCounter, err = bc.meter.Int64Counter(
		fmt.Sprintf("%s_events_dropped_total", bc.name),
		metric.WithDescription("Total events dropped"),
		metric.WithUnit("1"),
	)
	if err != nil {
		bc.eventsDroppedCounter = nil
	}

	// Events filtered counter
	bc.eventsFilteredCounter, err = bc.meter.Int64Counter(
		fmt.Sprintf("%s_events_filtered_total", bc.name),
		metric.WithDescription("Total events filtered"),
		metric.WithUnit("1"),
	)
	if err != nil {
		bc.eventsFilteredCounter = nil
	}

	// Error counter
	bc.errorCounter, err = bc.meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", bc.name),
		metric.WithDescription("Total errors encountered"),
		metric.WithUnit("1"),
	)
	if err != nil {
		bc.errorCounter = nil
	}

	// Processing duration histogram
	bc.processingDuration, err = bc.meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_seconds", bc.name),
		metric.WithDescription("Event processing duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0),
	)
	if err != nil {
		bc.processingDuration = nil
	}

	// Event size histogram
	bc.eventSizeHistogram, err = bc.meter.Int64Histogram(
		fmt.Sprintf("%s_event_size_bytes", bc.name),
		metric.WithDescription("Event size distribution"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(100, 500, 1000, 5000, 10000, 50000, 100000),
	)
	if err != nil {
		bc.eventSizeHistogram = nil
	}

	// Health status gauge (0=unhealthy, 1=degraded, 2=healthy)
	bc.healthStatus, err = bc.meter.Int64Gauge(
		fmt.Sprintf("%s_health_status", bc.name),
		metric.WithDescription("Health status (0=unhealthy, 1=degraded, 2=healthy)"),
		metric.WithUnit("1"),
	)
	if err != nil {
		bc.healthStatus = nil
	}
}

// RecordEvent should be called when an event is successfully processed
func (bc *BaseCollector) RecordEvent() {
	bc.eventsProcessed.Add(1)
	bc.lastEventTime.Store(time.Now())

	// Update OTEL metric if available
	if bc.eventsProcessedCounter != nil {
		bc.eventsProcessedCounter.Add(context.Background(), 1)
	}
}

// RecordEventWithContext records an event with trace context
func (bc *BaseCollector) RecordEventWithContext(ctx context.Context) {
	bc.eventsProcessed.Add(1)
	bc.lastEventTime.Store(time.Now())

	// Update OTEL metric with context for trace correlation
	if bc.eventsProcessedCounter != nil {
		bc.eventsProcessedCounter.Add(ctx, 1)
	}
}

// RecordError should be called when an error occurs
func (bc *BaseCollector) RecordError(err error) {
	bc.errorCount.Add(1)
	if err != nil {
		bc.lastError.Store(err)
	}

	// Update OTEL metric if available
	if bc.errorCounter != nil {
		attrs := []attribute.KeyValue{}
		if err != nil {
			attrs = append(attrs, attribute.String("error_type", fmt.Sprintf("%T", err)))
		}
		bc.errorCounter.Add(context.Background(), 1, metric.WithAttributes(attrs...))
	}
}

// RecordErrorWithContext records an error with trace context
func (bc *BaseCollector) RecordErrorWithContext(ctx context.Context, err error) {
	bc.errorCount.Add(1)
	if err != nil {
		bc.lastError.Store(err)
	}

	// Record error in span if tracing
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.RecordError(err)
	}

	// Update OTEL metric with context
	if bc.errorCounter != nil {
		attrs := []attribute.KeyValue{}
		if err != nil {
			attrs = append(attrs, attribute.String("error_type", fmt.Sprintf("%T", err)))
		}
		bc.errorCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordDrop should be called when an event is dropped
func (bc *BaseCollector) RecordDrop() {
	bc.eventsDropped.Add(1)

	// Update OTEL metric if available
	if bc.eventsDroppedCounter != nil {
		bc.eventsDroppedCounter.Add(context.Background(), 1)
	}
}

// RecordDropWithReason records a dropped event with a reason
func (bc *BaseCollector) RecordDropWithReason(ctx context.Context, reason string) {
	bc.eventsDropped.Add(1)

	// Update OTEL metric with reason
	if bc.eventsDroppedCounter != nil {
		bc.eventsDroppedCounter.Add(ctx, 1,
			metric.WithAttributes(attribute.String("reason", reason)))
	}
}

// RecordProcessingDuration records the time taken to process an event
func (bc *BaseCollector) RecordProcessingDuration(ctx context.Context, duration time.Duration) {
	if bc.processingDuration != nil {
		bc.processingDuration.Record(ctx, duration.Seconds())
	}
}

// RecordEventSize records the size of an event in bytes
func (bc *BaseCollector) RecordEventSize(ctx context.Context, sizeBytes int64) {
	if bc.eventSizeHistogram != nil {
		bc.eventSizeHistogram.Record(ctx, sizeBytes)
	}
}

// StartSpan starts a new span for event processing
func (bc *BaseCollector) StartSpan(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return bc.tracer.Start(ctx, spanName, opts...)
}

// GetTracer returns the tracer for custom instrumentation
func (bc *BaseCollector) GetTracer() trace.Tracer {
	return bc.tracer
}

// GetMeter returns the meter for custom metrics
func (bc *BaseCollector) GetMeter() metric.Meter {
	return bc.meter
}

// SetHealthy sets the collector health status
func (bc *BaseCollector) SetHealthy(healthy bool) {
	bc.isHealthy.Store(healthy)
}

// IsHealthy returns true if the collector is healthy
func (bc *BaseCollector) IsHealthy() bool {
	return bc.isHealthy.Load()
}

// Statistics returns collector statistics (implements CollectorWithStats)
func (bc *BaseCollector) Statistics() *domain.CollectorStats {
	lastEventTime := time.Time{}
	if t, ok := bc.lastEventTime.Load().(time.Time); ok {
		lastEventTime = t
	}

	customMetrics := map[string]string{
		"events_dropped":  fmt.Sprintf("%d", bc.eventsDropped.Load()),
		"events_filtered": fmt.Sprintf("%d", bc.eventsFiltered.Load()),
	}

	// Add ring buffer stats if enabled
	if rbStats := bc.GetRingBufferStats(); rbStats != nil {
		customMetrics["ring_buffer_capacity"] = fmt.Sprintf("%d", rbStats.Capacity)
		customMetrics["ring_buffer_produced"] = fmt.Sprintf("%d", rbStats.Produced)
		customMetrics["ring_buffer_consumed"] = fmt.Sprintf("%d", rbStats.Consumed)
		customMetrics["ring_buffer_dropped"] = fmt.Sprintf("%d", rbStats.Dropped)
		customMetrics["ring_buffer_utilization"] = fmt.Sprintf("%.2f%%", rbStats.Utilization)
		customMetrics["ring_buffer_consumers"] = fmt.Sprintf("%d", rbStats.Consumers)
	}

	// Add filter stats if enabled
	if filterStats := bc.GetFilterStatistics(); filterStats != nil {
		customMetrics["filter_version"] = fmt.Sprintf("%d", filterStats.Version)
		customMetrics["filter_allow_count"] = fmt.Sprintf("%d", filterStats.AllowFilters)
		customMetrics["filter_deny_count"] = fmt.Sprintf("%d", filterStats.DenyFilters)
		customMetrics["filter_events_processed"] = fmt.Sprintf("%d", filterStats.EventsProcessed)
		customMetrics["filter_events_allowed"] = fmt.Sprintf("%d", filterStats.EventsAllowed)
		customMetrics["filter_events_denied"] = fmt.Sprintf("%d", filterStats.EventsDenied)
	}

	return &domain.CollectorStats{
		EventsProcessed: bc.eventsProcessed.Load(),
		ErrorCount:      bc.errorCount.Load(),
		LastEventTime:   lastEventTime,
		Uptime:          time.Since(bc.startTime),
		CustomMetrics:   customMetrics,
	}
}

// Health returns health status (implements CollectorWithStats)
func (bc *BaseCollector) Health() *domain.HealthStatus {
	if !bc.isHealthy.Load() {
		var lastErr error
		if e := bc.lastError.Load(); e != nil {
			lastErr = e.(error)
		}
		return domain.NewUnhealthyStatus(
			fmt.Sprintf("%s collector is unhealthy", bc.name),
			lastErr,
		)
	}

	// Check if we're receiving events (only if we've processed at least one)
	if bc.eventsProcessed.Load() > 0 {
		lastEventTime := time.Time{}
		if t, ok := bc.lastEventTime.Load().(time.Time); ok {
			lastEventTime = t
		}

		timeSinceLastEvent := time.Since(lastEventTime)
		if timeSinceLastEvent > bc.healthCheckTimeout {
			return domain.NewHealthStatus(
				domain.HealthDegraded,
				fmt.Sprintf("No events received for %v", timeSinceLastEvent),
			)
		}
	}

	// Check error rate
	errorRate := float64(0)
	if processed := bc.eventsProcessed.Load(); processed > 0 {
		errorRate = float64(bc.errorCount.Load()) / float64(processed)
	}

	if errorRate > bc.errorRateThreshold {
		// Update health gauge
		if bc.healthStatus != nil {
			bc.healthStatus.Record(context.Background(), 1, // 1 = degraded
				metric.WithAttributes(attribute.String("reason", "high_error_rate")))
		}
		return domain.NewHealthStatus(
			domain.HealthDegraded,
			fmt.Sprintf("High error rate: %.1f%% (threshold: %.1f%%)",
				errorRate*100, bc.errorRateThreshold*100),
		)
	}

	// Update health gauge to healthy
	if bc.healthStatus != nil {
		bc.healthStatus.Record(context.Background(), 2) // 2 = healthy
	}

	return domain.NewHealthyStatus(fmt.Sprintf("%s collector operating normally", bc.name))
}

// GetName returns the collector name
func (bc *BaseCollector) GetName() string {
	return bc.name
}

// GetUptime returns how long the collector has been running
func (bc *BaseCollector) GetUptime() time.Duration {
	return time.Since(bc.startTime)
}

// GetEventCount returns the total number of events processed
func (bc *BaseCollector) GetEventCount() int64 {
	return bc.eventsProcessed.Load()
}

// GetErrorCount returns the total number of errors
func (bc *BaseCollector) GetErrorCount() int64 {
	return bc.errorCount.Load()
}

// GetDroppedCount returns the total number of dropped events
func (bc *BaseCollector) GetDroppedCount() int64 {
	return bc.eventsDropped.Load()
}

// Ring Buffer Methods

// StartRingBuffer starts the ring buffer processing if enabled
func (bc *BaseCollector) StartRingBuffer(ctx context.Context) {
	if bc.ringBuffer != nil {
		bc.ringBuffer.Start(ctx)
	}
}

// StopRingBuffer stops the ring buffer processing
func (bc *BaseCollector) StopRingBuffer() {
	if bc.ringBuffer != nil {
		bc.ringBuffer.Stop()
	}
}

// WriteToRingBuffer writes an event to the ring buffer if enabled
// Falls back to returning false if ring buffer is not enabled
func (bc *BaseCollector) WriteToRingBuffer(event *domain.CollectorEvent) bool {
	if bc.ringBuffer != nil {
		success := bc.ringBuffer.Write(event)
		if success {
			bc.RecordEvent()
		} else {
			bc.RecordDrop()
		}
		return success
	}
	return false
}

// RegisterLocalConsumer adds a local consumer for events
// Only works if ring buffer is enabled
func (bc *BaseCollector) RegisterLocalConsumer(consumer LocalConsumer) error {
	if bc.ringBuffer == nil {
		return fmt.Errorf("ring buffer not enabled for collector %s", bc.name)
	}
	bc.ringBuffer.RegisterLocalConsumer(consumer)
	return nil
}

// GetRingBufferStats returns ring buffer statistics if enabled
func (bc *BaseCollector) GetRingBufferStats() *RingBufferStats {
	if bc.ringBuffer != nil {
		stats := bc.ringBuffer.Statistics()
		return &stats
	}
	return nil
}

// SetRingBufferOutputChannel sets the output channel for the ring buffer
// This is useful for connecting to the orchestrator
func (bc *BaseCollector) SetRingBufferOutputChannel(ch chan *domain.CollectorEvent) {
	if bc.ringBuffer != nil {
		bc.ringBuffer.outputChan = ch
	}
}

// IsRingBufferEnabled returns true if ring buffer is enabled
func (bc *BaseCollector) IsRingBufferEnabled() bool {
	return bc.useRingBuffer && bc.ringBuffer != nil
}

// Filter Methods

// ShouldProcess checks if an event should be processed based on filters
// Returns true if event passes filters, false if it should be dropped
func (bc *BaseCollector) ShouldProcess(event *domain.CollectorEvent) bool {
	if bc.filterManager == nil || !bc.useFilters {
		return true // No filters, process everything
	}

	allowed := bc.filterManager.ShouldAllow(event)
	if !allowed {
		bc.eventsFiltered.Add(1)
		bc.RecordFilteredEvent(event)
	}
	return allowed
}

// RecordFilteredEvent records that an event was filtered
func (bc *BaseCollector) RecordFilteredEvent(event *domain.CollectorEvent) {
	if bc.eventsFilteredCounter != nil {
		ctx := context.Background()
		bc.eventsFilteredCounter.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("event_type", string(event.Type)),
				attribute.String("severity", string(event.Severity)),
			))
	}
}

// AddAllowFilter adds a named allow filter at runtime
func (bc *BaseCollector) AddAllowFilter(name string, filter FilterFunc) {
	if bc.filterManager == nil {
		bc.filterManager = NewFilterManager(bc.name, bc.logger)
		bc.useFilters = true
	}
	bc.filterManager.AddAllowFilter(name, filter)
}

// AddDenyFilter adds a named deny filter at runtime
func (bc *BaseCollector) AddDenyFilter(name string, filter FilterFunc) {
	if bc.filterManager == nil {
		bc.filterManager = NewFilterManager(bc.name, bc.logger)
		bc.useFilters = true
	}
	bc.filterManager.AddDenyFilter(name, filter)
}

// RemoveFilter removes a filter by name
func (bc *BaseCollector) RemoveFilter(name string) {
	if bc.filterManager != nil {
		bc.filterManager.RemoveFilter(name)
	}
}

// GetFilterStatistics returns filter statistics
func (bc *BaseCollector) GetFilterStatistics() *FilterStatistics {
	if bc.filterManager != nil {
		stats := bc.filterManager.GetStatistics()
		return &stats
	}
	return nil
}

// LoadFiltersFromFile loads filters from a YAML file
func (bc *BaseCollector) LoadFiltersFromFile(path string) error {
	if bc.filterManager == nil {
		bc.filterManager = NewFilterManager(bc.name, bc.logger)
		bc.useFilters = true
	}
	return bc.filterManager.LoadFromFile(path)
}

// StopFilters stops the filter manager (stops watching config file)
func (bc *BaseCollector) StopFilters() {
	if bc.filterManager != nil {
		bc.filterManager.Stop()
	}
}
