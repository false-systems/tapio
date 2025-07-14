package types

import (
	"time"
)

// CollectorConfig defines configuration for collectors
type CollectorConfig struct {
	Name             string
	Type             string
	EventBufferSize  int
	Enabled          bool
	Tags             map[string]string
	AdditionalConfig map[string]interface{}
}

// Event represents a collected event
type Event struct {
	ID        string
	Timestamp time.Time
	Type      string
	Source    string
	Level     string
	Message   string
	Data      map[string]interface{}
}

// HealthStatus represents the health status of a collector
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// Health represents the health of a collector
type Health struct {
	Status  HealthStatus
	Message string
	Details map[string]interface{}
}