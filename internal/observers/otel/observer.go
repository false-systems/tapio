package otel

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/internal/observers"
	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Observer transforms OTEL SDK data to Tapio CollectorEvents
//
// Architecture:
//   - Reads spans/metrics/logs from OTEL SDK (in-process)
//   - Transforms to domain.OTELSpanData / domain.OTELMetricData
//   - Extracts service dependencies from span relationships
//   - Emits CollectorEvents to NATS via orchestrator
//
// This observer does NOT run gRPC/HTTP servers. Applications instrument
// themselves with OTEL SDK, and this observer reads the SDK data directly.
//
// Platform: ALL (Mac, Linux, Windows) - no eBPF required
type Observer struct {
	*base.BaseObserver        // Provides Statistics() and Health()
	*base.EventChannelManager // Handles event channels
	*base.LifecycleManager    // Manages goroutines

	// Core configuration
	config *Config
	logger *zap.Logger

	// OTEL SDK readers
	spanReader   SpanReader
	metricReader MetricReader

	// OTLP exporter
	otlpExporter OTLPExporter

	// Service dependency tracking
	serviceDeps     map[string]map[string]int64 // service -> service -> count
	serviceDepsLock sync.RWMutex
	lastDepsEmit    time.Time

	// Statistics
	stats *Stats

	// OTEL-specific instrumentation
	tracer          trace.Tracer
	spansReceived   metric.Int64Counter
	metricsReceived metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
}

// Stats holds observer statistics with atomic access
type Stats struct {
	SpansReceived   atomic.Uint64
	MetricsReceived atomic.Uint64
	EventsEmitted   atomic.Uint64
	SpansDropped    atomic.Uint64
	ErrorCount      atomic.Uint64
	LastEventTime   time.Time
	mu              sync.RWMutex // Protects LastEventTime
}

// SetLastEventTime safely sets the LastEventTime field.
func (s *Stats) SetLastEventTime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastEventTime = t
}

// GetLastEventTime safely gets the LastEventTime field.
func (s *Stats) GetLastEventTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastEventTime
}

// Interface verification
var _ observers.Observer = (*Observer)(nil)

// NewObserver creates a new OTEL observer
func NewObserver(name string, config *Config) (*Observer, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Set name in config if not already set
	if config.Name == "" {
		config.Name = name
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize base components
	baseConfig := base.BaseObserverConfig{
		Name:               name,
		HealthCheckTimeout: 30 * time.Second,
		ErrorRateThreshold: 0.05,
		Logger:             logger,
	}

	baseObserver := base.NewBaseObserverWithConfig(baseConfig)
	eventManager := base.NewEventChannelManager(config.BufferSize, name, logger)
	lifecycle := base.NewLifecycleManager(context.Background(), logger)

	// Initialize OTEL-specific instrumentation
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create OTEL-specific metrics
	spansReceived, err := meter.Int64Counter(
		fmt.Sprintf("%s_spans_received_total", name),
		metric.WithDescription("Total OTEL spans received"),
	)
	if err != nil {
		logger.Warn("Failed to create spans counter", zap.Error(err))
	}

	metricsReceived, err := meter.Int64Counter(
		fmt.Sprintf("%s_metrics_received_total", name),
		metric.WithDescription("Total OTEL metrics received"),
	)
	if err != nil {
		logger.Warn("Failed to create metrics counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription("OTEL event processing duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription("Total errors encountered"),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	// Create span reader
	spanReader := NewBatchSpanReader(config.BufferSize)

	// Create OTLP exporter
	otlpExporter, err := NewExporter(config.OTLP, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	return &Observer{
		BaseObserver:        baseObserver,
		EventChannelManager: eventManager,
		LifecycleManager:    lifecycle,
		config:              config,
		logger:              logger.Named(name),
		spanReader:          spanReader,
		otlpExporter:        otlpExporter,
		serviceDeps:         make(map[string]map[string]int64),
		stats:               &Stats{},
		tracer:              tracer,
		spansReceived:       spansReceived,
		metricsReceived:     metricsReceived,
		errorsTotal:         errorsTotal,
		processingTime:      processingTime,
	}, nil
}

// Name returns the observer name
func (c *Observer) Name() string {
	return c.BaseObserver.GetName()
}

// Start begins OTEL data processing
func (c *Observer) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "otel.observer.start")
	defer span.End()

	// Start span processing goroutine
	c.LifecycleManager.Start("processSpans", c.processSpans)

	// Start service dependency emitter
	if c.config.EnableDependencies {
		c.LifecycleManager.Start("emitServiceDependencies", c.emitServiceDependencies)
	}

	c.BaseObserver.SetHealthy(true)
	c.lastDepsEmit = time.Now()

	c.logger.Info("OTEL observer started",
		zap.String("name", c.config.Name),
		zap.Bool("dependencies", c.config.EnableDependencies),
		zap.Float64("sampling_rate", c.config.SamplingRate),
	)

	return nil
}

// Stop gracefully shuts down the observer
func (c *Observer) Stop() error {
	c.logger.Info("Stopping OTEL observer", zap.String("name", c.config.Name))

	// Stop lifecycle manager with timeout
	c.LifecycleManager.Stop(30 * time.Second)

	// Shutdown OTLP exporter
	if c.otlpExporter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.otlpExporter.Shutdown(ctx); err != nil {
			c.logger.Error("Failed to shutdown OTLP exporter", zap.Error(err))
		}
	}

	// Close event channel
	c.EventChannelManager.Close()

	c.BaseObserver.SetHealthy(false)

	return nil
}

// Events returns the event channel
func (c *Observer) Events() <-chan *domain.CollectorEvent {
	return c.EventChannelManager.GetChannel()
}

// IsHealthy returns health status
func (c *Observer) IsHealthy() bool {
	return c.BaseObserver.IsHealthy()
}

// processSpans continuously reads and processes spans from the span reader
func (c *Observer) processSpans() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			c.processAvailableSpans()
		}
	}
}

// processAvailableSpans reads and processes all available spans
func (c *Observer) processAvailableSpans() {
	ctx := c.LifecycleManager.Context()
	startTime := time.Now()

	// Read spans from reader
	spans, err := c.spanReader.ReadSpans(ctx)
	if err != nil {
		c.logger.Error("Failed to read spans", zap.Error(err))
		c.BaseObserver.RecordError(err)
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1)
		}
		return
	}

	if len(spans) == 0 {
		return
	}

	// Process each span
	for _, spanData := range spans {
		if spanData == nil {
			continue
		}

		// Count all received spans (before sampling)
		atomic.AddUint64(&c.stats.SpansReceived, 1)
		if c.spansReceived != nil {
			c.spansReceived.Add(ctx, 1)
		}

		// Apply sampling
		if !c.ShouldSample(spanData) {
			continue
		}

		// Create CollectorEvent
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("otel-span-%s", spanData.SpanID),
			Timestamp: spanData.StartTime,
			Source:    c.config.Name,
			Type:      domain.EventTypeOTELSpan,
			Severity:  c.determineEventSeverity(spanData),
			EventData: domain.EventDataContainer{
				OTELSpan: spanData,
			},
			Metadata: c.buildEventMetadata(spanData),
		}

		// Send event
		if c.EventChannelManager.SendEvent(event) {
			func() {
				defer func() {
					if r := recover(); r != nil {
						// Optionally log the panic here, e.g. using zap
						// c.logger.Warn("BaseObserver.RecordEvent panicked", zap.Any("recover", r))
					} else {
						c.stats.EventsEmitted.Add(1)
					}
				}()
				c.BaseObserver.RecordEvent()
			}()
		} else {
			c.stats.SpansDropped.Add(1)
			c.BaseObserver.RecordDrop()
		}

		// Extract service dependency
		if c.config.EnableDependencies {
			if from, to, found := ExtractServiceDependency(spanData); found {
				c.RecordServiceDependency(from, to)
			}
		}
	}

	// Export to OTLP if enabled (with retry)
	if c.config.OTLP.Enabled && len(spans) > 0 {
		retryConfig := DefaultRetryConfig()
		if err := ExportWithRetry(ctx, c.otlpExporter, spans, retryConfig, c.logger); err != nil {
			c.logger.Error("OTLP export failed after retries",
				zap.Error(err),
				zap.Int("span_count", len(spans)),
			)
			c.BaseObserver.RecordError(err)
		}
	}

	// Record processing time
	duration := time.Since(startTime)
	if c.processingTime != nil {
		c.processingTime.Record(ctx, float64(duration.Milliseconds()))
	}
}

// determineEventSeverity determines severity based on span status
func (c *Observer) determineEventSeverity(span *domain.OTELSpanData) domain.EventSeverity {
	if span.StatusCode == "ERROR" {
		return domain.EventSeverityError
	}
	return domain.EventSeverityInfo
}

// buildEventMetadata constructs metadata from span data
func (c *Observer) buildEventMetadata(span *domain.OTELSpanData) domain.EventMetadata {
	return domain.EventMetadata{
		TraceID:      span.TraceID,
		SpanID:       span.SpanID,
		PodName:      span.K8sPodName,
		PodNamespace: span.K8sNamespace,
		Labels: map[string]string{
			"observer":     c.config.Name,
			"service_name": span.ServiceName,
			"span_kind":    span.Kind,
			"version":      "1.0",
		},
	}
}

// emitServiceDependencies periodically emits service dependency events
func (c *Observer) emitServiceDependencies() {
	ticker := time.NewTicker(c.config.ServiceMapInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			c.emitDependencyEvents()
		}
	}
}

// emitDependencyEvents emits service dependency events
func (c *Observer) emitDependencyEvents() {
	c.serviceDepsLock.RLock()
	defer c.serviceDepsLock.RUnlock()

	now := time.Now()

	for fromService, toServices := range c.serviceDeps {
		for toService, count := range toServices {
			event := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("otel-dep-%s-%s-%d", fromService, toService, now.UnixNano()),
				Timestamp: now,
				Source:    c.config.Name,
				Type:      domain.EventTypeOTELMetric,
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Custom: map[string]string{
						"metric_type":  "service_dependency",
						"from_service": fromService,
						"to_service":   toService,
						"call_count":   fmt.Sprintf("%d", count),
						"window":       c.config.ServiceMapInterval.String(),
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer":     c.config.Name,
						"from_service": fromService,
						"to_service":   toService,
						"metric_type":  "service_dependency",
						"version":      "1.0",
					},
				},
			}

			if c.EventChannelManager.SendEvent(event) {
				c.BaseObserver.RecordEvent()
				if c.metricsReceived != nil {
					c.metricsReceived.Add(c.LifecycleManager.Context(), 1)
				}
			} else {
				c.BaseObserver.RecordDrop()
			}
		}
	}

	// Clear the map for next interval
	c.serviceDepsLock.Lock()
	c.serviceDeps = make(map[string]map[string]int64)
	c.serviceDepsLock.Unlock()
}

// RecordServiceDependency records a service dependency from a span
func (c *Observer) RecordServiceDependency(fromService, toService string) {
	if fromService == "" || toService == "" || fromService == toService {
		return
	}

	c.serviceDepsLock.Lock()
	defer c.serviceDepsLock.Unlock()

	if c.serviceDeps[fromService] == nil {
		c.serviceDeps[fromService] = make(map[string]int64)
	}
	c.serviceDeps[fromService][toService]++
}

// GetServiceDependencies returns a copy of current service dependencies (for testing)
func (c *Observer) GetServiceDependencies() map[string]map[string]int64 {
	c.serviceDepsLock.RLock()
	defer c.serviceDepsLock.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]map[string]int64)
	for from, toMap := range c.serviceDeps {
		result[from] = make(map[string]int64)
		for to, count := range toMap {
			result[from][to] = count
		}
	}
	return result
}

// ShouldSample determines if a span should be sampled
func (c *Observer) ShouldSample(span *domain.OTELSpanData) bool {
	// Always sample errors
	if c.config.AlwaysSampleErrors && span.StatusCode == "ERROR" {
		return true
	}

	// Simple probabilistic sampling
	// In production, use a proper hash of trace ID for consistent sampling
	return c.config.SamplingRate >= 1.0 || (time.Now().UnixNano()%100) < int64(c.config.SamplingRate*100)
}
