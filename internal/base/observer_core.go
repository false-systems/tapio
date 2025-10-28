package base

import (
	"sync/atomic"

	"github.com/rs/zerolog"
)

// ObserverCore provides minimal observer utilities (lean, no dependencies)
type ObserverCore struct {
	name   string
	logger zerolog.Logger

	// Atomic counters (no OTEL dependency!)
	eventsProcessed atomic.Int64
	eventsDropped   atomic.Int64
	errorsTotal     atomic.Int64

	// Lifecycle state
	running atomic.Bool
}

// CoreStats holds observer statistics snapshot
type CoreStats struct {
	EventsProcessed int64
	EventsDropped   int64
	ErrorsTotal     int64
}

// NewObserverCore creates minimal observer core
func NewObserverCore(name string) *ObserverCore {
	return &ObserverCore{
		name:   name,
		logger: NewLogger(name),
	}
}

// Name returns the observer name
func (o *ObserverCore) Name() string {
	return o.name
}

// Logger returns the structured logger
func (o *ObserverCore) Logger() zerolog.Logger {
	return o.logger
}

// IsRunning returns true if observer is running
func (o *ObserverCore) IsRunning() bool {
	return o.running.Load()
}

// MarkRunning sets the running state
func (o *ObserverCore) MarkRunning(running bool) {
	o.running.Store(running)
}

// RecordEvent increments events processed counter
func (o *ObserverCore) RecordEvent() {
	o.eventsProcessed.Add(1)
}

// RecordDrop increments events dropped counter
func (o *ObserverCore) RecordDrop() {
	o.eventsDropped.Add(1)
}

// RecordError increments errors counter
func (o *ObserverCore) RecordError() {
	o.errorsTotal.Add(1)
}

// Stats returns current statistics snapshot
func (o *ObserverCore) Stats() CoreStats {
	return CoreStats{
		EventsProcessed: o.eventsProcessed.Load(),
		EventsDropped:   o.eventsDropped.Load(),
		ErrorsTotal:     o.errorsTotal.Load(),
	}
}
