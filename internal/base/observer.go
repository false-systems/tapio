package base

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence"
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

	// Lifecycle management
	running        atomic.Bool
	stopped        atomic.Bool
	mu             sync.Mutex // Protects cancelPipeline
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

		// Pass nil for observers - health checks are for external monitoring, not self-monitoring
		shutdown, err := InitTelemetry(ctx, telemetryConfig, nil)
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
	b.mu.Lock()
	b.cancelPipeline = cancel
	b.mu.Unlock()

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
	b.mu.Lock()
	cancel := b.cancelPipeline
	b.mu.Unlock()

	if cancel != nil {
		cancel()
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
