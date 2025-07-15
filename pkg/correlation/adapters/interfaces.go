package adapters

import (
	"context"

	"github.com/yairfalse/tapio/pkg/correlation/domain"
)

// EventAdapter converts external event formats to domain events
type EventAdapter interface {
	// Adapt converts an external event to a domain event
	Adapt(ctx context.Context, externalEvent interface{}) (*domain.Event, error)
	
	// GetSourceType returns the source type this adapter handles
	GetSourceType() string
	
	// CanHandle checks if this adapter can handle the given event type
	CanHandle(externalEvent interface{}) bool
}

// SourceAdapter wraps external data sources to provide domain events
type SourceAdapter interface {
	domain.EventSource
	
	// GetAdapter returns the event adapter used by this source
	GetAdapter() EventAdapter
	
	// GetExternalSource returns the underlying external source
	GetExternalSource() interface{}
}

// StoreAdapter wraps external storage systems to provide event storage
type StoreAdapter interface {
	domain.EventStore
	
	// GetExternalStore returns the underlying external store
	GetExternalStore() interface{}
}

// MetricsAdapter wraps external metrics systems
type MetricsAdapter interface {
	domain.MetricsCollector
	
	// GetExternalMetrics returns the underlying external metrics system
	GetExternalMetrics() interface{}
}

// ResultHandlerAdapter wraps external result handlers
type ResultHandlerAdapter interface {
	domain.ResultHandler
	
	// GetExternalHandler returns the underlying external handler
	GetExternalHandler() interface{}
}

// LoggerAdapter wraps external logging systems
type LoggerAdapter interface {
	domain.Logger
	
	// GetExternalLogger returns the underlying external logger
	GetExternalLogger() interface{}
}