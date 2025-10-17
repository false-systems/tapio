package base

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/trace"
)

// Observer defines the interface all observers must implement
type Observer interface {
	Start(ctx context.Context) error
	Stop() error
	Name() string
	IsHealthy() bool
}

// BaseObserver provides common functionality for all observers
type BaseObserver struct {
	name      string
	startTime time.Time

	// Structured logging with OTEL trace context
	logger zerolog.Logger

	// Statistics (thread-safe atomic counters)
	eventsProcessed atomic.Int64
	eventsDropped   atomic.Int64
	errorsTotal     atomic.Int64

	// OTEL instrumentation
	tracer  trace.Tracer
	metrics *ObserverMetrics

	// Telemetry shutdown
	telemetryShutdown *TelemetryShutdown

	// Domain event publisher (NATS in enterprise, NoOp in OSS)
	eventPublisher domain.EventPublisher

	// Pipeline for observer stages
	pipeline *Pipeline

	// Lifecycle management
	running        atomic.Bool
	stopped        atomic.Bool
	cancelPipeline context.CancelFunc
}

// NewBaseObserver creates a new base observer with OTEL instrumentation
func NewBaseObserver(name string) (*BaseObserver, error) {
	return NewBaseObserverWithTelemetry(name, nil)
}

// NewBaseObserverWithTelemetry creates a base observer with optional telemetry initialization
func NewBaseObserverWithTelemetry(name string, telemetryConfig *TelemetryConfig) (*BaseObserver, error) {
	return NewBaseObserverWithConfig(name, telemetryConfig, nil)
}

// NewBaseObserverWithConfig creates a base observer with telemetry and event publisher
func NewBaseObserverWithConfig(name string, telemetryConfig *TelemetryConfig, eventPublisher domain.EventPublisher) (*BaseObserver, error) {
	metrics, err := NewObserverMetrics(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics for observer %s: %w", name, err)
	}

	// Default to NoOp publisher if not provided
	if eventPublisher == nil {
		eventPublisher = &domain.NoOpPublisher{}
	}

	observer := &BaseObserver{
		name:           name,
		startTime:      time.Now(),
		logger:         NewLogger(name),
		metrics:        metrics,
		pipeline:       NewPipeline(),
		eventPublisher: eventPublisher,
	}

	// Initialize telemetry if config provided
	if telemetryConfig != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		shutdown, err := InitTelemetry(ctx, telemetryConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize telemetry for observer %s: %w", name, err)
		}
		observer.telemetryShutdown = shutdown
	}

	return observer, nil
}

// Name returns the observer name
func (b *BaseObserver) Name() string {
	return b.name
}

// IsHealthy returns true if the observer is running without errors
func (b *BaseObserver) IsHealthy() bool {
	return b.running.Load() && !b.stopped.Load()
}

// Logger returns the observer's logger with optional trace context
func (b *BaseObserver) Logger(ctx context.Context) zerolog.Logger {
	return WithTraceContext(ctx, b.logger)
}

// Start initiates the observer pipeline
func (b *BaseObserver) Start(ctx context.Context) error {
	if b.running.Load() {
		b.logger.Warn().Msg("observer already running")
		return fmt.Errorf("observer %s already running", b.name)
	}

	b.running.Store(true)
	b.stopped.Store(false)
	b.startTime = time.Now()

	b.logger.Info().Msg("observer starting")

	// Create cancellable context for pipeline
	pipelineCtx, cancel := context.WithCancel(ctx)
	b.cancelPipeline = cancel

	if err := b.pipeline.Run(pipelineCtx); err != nil {
		b.stopped.Store(true)
		b.running.Store(false)
		b.logger.Error().Err(err).Msg("pipeline failed")
		cancel()
		return fmt.Errorf("pipeline failed for observer %s: %w", b.name, err)
	}

	return nil
}

// Stop gracefully shuts down the observer
func (b *BaseObserver) Stop() error {
	if !b.running.Load() {
		b.logger.Warn().Msg("observer not running")
		return fmt.Errorf("observer %s not running", b.name)
	}

	b.logger.Info().Msg("observer stopping")

	// Cancel pipeline context to stop all stages
	if b.cancelPipeline != nil {
		b.cancelPipeline()
	}

	b.stopped.Store(true)
	b.running.Store(false)

	// Shutdown telemetry if initialized
	if b.telemetryShutdown != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := b.telemetryShutdown.Shutdown(ctx); err != nil {
			b.logger.Error().Err(err).Msg("telemetry shutdown failed")
			return fmt.Errorf("failed to shutdown telemetry for observer %s: %w", b.name, err)
		}
	}

	b.logger.Info().Msg("observer stopped")
	return nil
}

// AddStage registers a pipeline stage
func (b *BaseObserver) AddStage(stage PipelineStage) {
	b.pipeline.Add(stage)
}

// RecordEvent increments events processed counter and records metrics
func (b *BaseObserver) RecordEvent(ctx context.Context) {
	b.eventsProcessed.Add(1)
	b.metrics.RecordEvent(ctx, b.name, nil)
}

// RecordDrop increments events dropped counter and records metrics
func (b *BaseObserver) RecordDrop(ctx context.Context, eventType string) {
	b.eventsDropped.Add(1)
	b.metrics.RecordDrop(ctx, b.name, eventType)
}

// RecordError increments error counter and records metrics
func (b *BaseObserver) RecordError(ctx context.Context, event *domain.ObserverEvent) {
	b.errorsTotal.Add(1)
	b.metrics.RecordError(ctx, b.name, event)
}

// RecordProcessingTime records event processing duration
func (b *BaseObserver) RecordProcessingTime(ctx context.Context, event *domain.ObserverEvent, durationMs float64) {
	b.metrics.RecordProcessingTime(ctx, b.name, event, durationMs)
}

// PublishEvent publishes a domain event to the configured backend (NATS in enterprise, NoOp in OSS)
func (b *BaseObserver) PublishEvent(ctx context.Context, subject string, event any) error {
	return b.eventPublisher.Publish(ctx, subject, event)
}

// SendObserverEvent sends full ObserverEvent as structured log to OTLP (OSS value!)
// This gives free users all the raw data - they can query, analyze, build dashboards
func (b *BaseObserver) SendObserverEvent(ctx context.Context, event *domain.ObserverEvent) {
	if event == nil {
		b.logger.Error().Msg("SendObserverEvent called with nil event")
		return
	}
	logger := global.GetLoggerProvider().Logger(b.name)

	// Marshal event to JSON for log body
	eventJSON, err := json.Marshal(event)
	if err != nil {
		b.logger.Error().Err(err).Msg("failed to marshal observer event")
		return
	}

	// Build attributes for filtering/querying
	var attrs []log.KeyValue
	attrs = append(attrs, log.String("event.type", event.Type))
	attrs = append(attrs, log.String("event.source", event.Source))
	attrs = append(attrs, log.String("event.id", event.ID))

	// Add trace context if present
	if event.TraceID != "" {
		attrs = append(attrs, log.String("trace.id", event.TraceID))
	}
	if event.SpanID != "" {
		attrs = append(attrs, log.String("span.id", event.SpanID))
	}

	// Add type-specific attributes for easy querying
	if event.NetworkData != nil {
		if event.NetworkData.Protocol != "" {
			attrs = append(attrs, log.String("network.protocol", event.NetworkData.Protocol))
		}
		if event.NetworkData.PodName != "" {
			attrs = append(attrs, log.String("k8s.pod.name", event.NetworkData.PodName))
		}
		if event.NetworkData.Namespace != "" {
			attrs = append(attrs, log.String("k8s.namespace", event.NetworkData.Namespace))
		}
	}

	// Emit as structured log record
	var logRecord log.Record
	logRecord.SetTimestamp(event.Timestamp)
	logRecord.SetBody(log.StringValue(string(eventJSON)))
	logRecord.AddAttributes(attrs...)

	logger.Emit(ctx, logRecord)
}

// Stats returns current observer statistics
func (b *BaseObserver) Stats() ObserverStats {
	return ObserverStats{
		Name:            b.name,
		Uptime:          time.Since(b.startTime),
		EventsProcessed: b.eventsProcessed.Load(),
		EventsDropped:   b.eventsDropped.Load(),
		ErrorsTotal:     b.errorsTotal.Load(),
	}
}

// ObserverStats holds observer statistics
type ObserverStats struct {
	Name            string
	Uptime          time.Duration
	EventsProcessed int64
	EventsDropped   int64
	ErrorsTotal     int64
}
