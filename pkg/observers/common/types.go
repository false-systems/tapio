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
	Data      map[string]interface{}
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