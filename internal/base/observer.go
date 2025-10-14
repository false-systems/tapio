package base

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/yairfalse/tapio/pkg/domain"
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

	// Pipeline for observer stages
	pipeline *Pipeline

	// Lifecycle management
	running atomic.Bool
	stopped atomic.Bool
}

// NewBaseObserver creates a new base observer with OTEL instrumentation
func NewBaseObserver(name string) (*BaseObserver, error) {
	return NewBaseObserverWithTelemetry(name, nil)
}

// NewBaseObserverWithTelemetry creates a base observer with optional telemetry initialization
func NewBaseObserverWithTelemetry(name string, telemetryConfig *TelemetryConfig) (*BaseObserver, error) {
	metrics, err := NewObserverMetrics(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics for observer %s: %w", name, err)
	}

	observer := &BaseObserver{
		name:      name,
		startTime: time.Now(),
		logger:    NewLogger(name),
		metrics:   metrics,
		pipeline:  NewPipeline(),
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

	if err := b.pipeline.Run(ctx); err != nil {
		b.stopped.Store(true)
		b.running.Store(false)
		b.logger.Error().Err(err).Msg("pipeline failed")
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
func (b *BaseObserver) RecordEvent(ctx context.Context, event *domain.ObserverEvent) {
	b.eventsProcessed.Add(1)
	b.metrics.RecordEvent(ctx, b.name, event)
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
