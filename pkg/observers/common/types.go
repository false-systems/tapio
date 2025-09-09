package common

import (
	"context"
	"time"
)

// Observer defines the interface for all observers
type Observer interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
	GetEvents() <-chan ObserverEvent
}

// ObserverEvent represents an event from an observer
type ObserverEvent struct {
	Type      EventType
	Timestamp time.Time
	Service   string
	Data      EventData
}

// EventData holds event-specific data
type EventData struct {
	ErrorCount uint64            `json:"error_count,omitempty"`
	TotalCount uint64            `json:"total_count,omitempty"`
	ErrorRate  float64           `json:"error_rate,omitempty"`
	AvgLatency float64           `json:"avg_latency,omitempty"`
	ErrorTypes map[uint16]uint64 `json:"error_types,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// EventType represents the type of observer event
type EventType string

const (
	EventTypeStatus   EventType = "status"
	EventTypeMemory   EventType = "memory"
	EventTypeNetwork  EventType = "network"
	EventTypeResource EventType = "resource"
	EventTypeSystemd  EventType = "systemd"
)
