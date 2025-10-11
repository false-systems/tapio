package base

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

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

	// Statistics (thread-safe atomic counters)
	eventsProcessed atomic.Int64
	eventsDropped   atomic.Int64
	errorsTotal     atomic.Int64

	// OTEL instrumentation
	tracer  trace.Tracer
	metrics *ObserverMetrics

	// Pipeline for observer stages
	pipeline *Pipeline

	// Lifecycle management
	running atomic.Bool
	stopped atomic.Bool
}

// NewBaseObserver creates a new base observer with OTEL instrumentation
func NewBaseObserver(name string) (*BaseObserver, error) {
	metrics, err := NewObserverMetrics(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics for observer %s: %w", name, err)
	}

	return &BaseObserver{
		name:      name,
		startTime: time.Now(),
		metrics:   metrics,
		pipeline:  NewPipeline(),
	}, nil
}

// Name returns the observer name
func (b *BaseObserver) Name() string {
	return b.name
}

// IsHealthy returns true if the observer is running without errors
func (b *BaseObserver) IsHealthy() bool {
	return b.running.Load() && !b.stopped.Load()
}

// Start initiates the observer pipeline
func (b *BaseObserver) Start(ctx context.Context) error {
	if b.running.Load() {
		return fmt.Errorf("observer %s already running", b.name)
	}

	b.running.Store(true)
	b.stopped.Store(false)
	b.startTime = time.Now()

	if err := b.pipeline.Run(ctx); err != nil {
		b.stopped.Store(true)
		b.running.Store(false)
		return fmt.Errorf("pipeline failed for observer %s: %w", b.name, err)
	}

	return nil
}

// Stop gracefully shuts down the observer
func (b *BaseObserver) Stop() error {
	if !b.running.Load() {
		return fmt.Errorf("observer %s not running", b.name)
	}

	b.stopped.Store(true)
	b.running.Store(false)

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
