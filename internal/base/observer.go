package base

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence"
)

// BaseObserver provides common infrastructure for all observers
// (metrics, telemetry, pipeline, logging, event publishing)
// Lifecycle management is handled by the Supervisor.
type BaseObserver struct {
	name      string
	startTime time.Time

	// Structured logging with OTEL trace context
	logger zerolog.Logger

	// Statistics (thread-safe atomic counters)
	eventsProcessed atomic.Int64
	eventsDropped   atomic.Int64
	errorsTotal     atomic.Int64

	// Prometheus metrics (native, fast)
	metrics *PromObserverMetrics

	// Intelligence service for event emission
	emitter intelligence.Service

	// Telemetry shutdown
	telemetryShutdown *TelemetryShutdown

	// Domain event publisher (NATS in enterprise, NoOp in OSS)
	eventPublisher domain.EventPublisher

	// Pipeline for observer stages
	pipeline *Pipeline
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
	// Use native Prometheus metrics (fast, no OTEL SDK overhead)
	metrics := NewPromObserverMetrics(GlobalRegistry)

	// Default to debug emitter (stdout) - observers can inject their own
	emitter, err := intelligence.New(intelligence.Config{Tier: intelligence.TierDebug})
	if err != nil {
		return nil, fmt.Errorf("failed to create emitter for observer %s: %w", name, err)
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
		emitter:        emitter,
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

// Logger returns the observer's logger with optional trace context
func (b *BaseObserver) Logger(ctx context.Context) zerolog.Logger {
	return WithTraceContext(ctx, b.logger)
}

// AddStage registers a pipeline stage
func (b *BaseObserver) AddStage(stage PipelineStage) {
	b.pipeline.Add(stage)
}

// RunPipeline executes all registered pipeline stages
// This is typically called from the observer's Run method
func (b *BaseObserver) RunPipeline(ctx context.Context) error {
	return b.pipeline.Run(ctx)
}

// Shutdown cleans up observer resources (telemetry, etc.)
// Should be called when observer is done running
func (b *BaseObserver) Shutdown(ctx context.Context) error {
	if b.telemetryShutdown != nil {
		if err := b.telemetryShutdown.Shutdown(ctx); err != nil {
			b.logger.Error().Err(err).Msg("telemetry shutdown failed")
			return fmt.Errorf("failed to shutdown telemetry for observer %s: %w", b.name, err)
		}
	}
	return nil
}

// RecordEvent increments events processed counter and records metrics
func (b *BaseObserver) RecordEvent(_ context.Context) {
	b.eventsProcessed.Add(1)
	b.metrics.RecordEvent(b.name, "event")
}

// RecordEventWithType increments events processed counter with specific event type
func (b *BaseObserver) RecordEventWithType(_ context.Context, eventType string) {
	b.eventsProcessed.Add(1)
	b.metrics.RecordEvent(b.name, eventType)
}

// RecordDrop increments events dropped counter and records metrics
func (b *BaseObserver) RecordDrop(_ context.Context, eventType string) {
	b.eventsDropped.Add(1)
	b.metrics.RecordDrop(b.name, eventType)
}

// RecordError increments error counter and records metrics
func (b *BaseObserver) RecordError(_ context.Context, event *domain.ObserverEvent) {
	b.errorsTotal.Add(1)
	eventType := "event"
	errorType := "unknown"
	if event != nil {
		eventType = event.Type
	}
	b.metrics.RecordError(b.name, eventType, errorType)
}

// RecordProcessingTime records event processing duration
func (b *BaseObserver) RecordProcessingTime(_ context.Context, event *domain.ObserverEvent, durationMs float64) {
	eventType := "event"
	if event != nil {
		eventType = event.Type
	}
	b.metrics.RecordProcessingTime(b.name, eventType, durationMs)
}

// PublishEvent publishes a domain event to the configured backend (NATS in enterprise, NoOp in OSS)
func (b *BaseObserver) PublishEvent(ctx context.Context, subject string, event any) error {
	return b.eventPublisher.Publish(ctx, subject, event)
}

// SendObserverEvent emits an event through the intelligence service
// This routes to NATS (free/enterprise) or stdout (debug) based on tier config
func (b *BaseObserver) SendObserverEvent(ctx context.Context, event *domain.ObserverEvent) {
	if event == nil {
		b.logger.Error().Msg("SendObserverEvent called with nil event")
		return
	}

	if b.emitter == nil {
		b.logger.Warn().Msg("no emitter configured, dropping event")
		return
	}

	if err := b.emitter.Emit(ctx, event); err != nil {
		// Log but don't fail - emitter handles criticality internally
		if b.emitter.IsCritical() {
			b.logger.Error().Err(err).Str("event_type", event.Type).Msg("failed to emit event")
		} else {
			b.logger.Debug().Err(err).Str("event_type", event.Type).Msg("event emission failed (non-critical)")
		}
	}
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
