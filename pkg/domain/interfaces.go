package domain

import (
	"context"
)

// EventInterface represents a generic event interface
// This is the minimal interface that collectors should implement
type EventInterface interface {
	GetID() string
	GetType() EventType
	GetTimestamp() int64
}

// Collector is the interface that all collectors must implement
type Collector interface {
	// Name returns the collector name
	Name() string

	// Start begins collecting events
	Start(ctx context.Context) error

	// Stop gracefully shuts down the collector
	Stop() error
}

// EventProcessor processes events
type EventProcessor interface {
	// Process handles an event
	Process(ctx context.Context, event *ObservationEvent) error
}

// CollectorWithStats extends Collector with statistics
type CollectorWithStats interface {
	Collector
	// Statistics returns TYPED stats - NO map[string]interface{}
	Statistics() *CollectorStats
	// Health returns TYPED health - NO map[string]interface{}
	Health() *HealthStatus
}
