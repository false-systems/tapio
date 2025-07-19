package domain

import (
	"time"
)

// Core event types
type (
	EventID    string
	EventType  string
	SourceType string
	Severity   string
)

// Event severity levels
const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Event represents a system event
type Event struct {
	ID        EventID                `json:"id"`
	Type      EventType              `json:"type"`
	Source    SourceType             `json:"source"`
	Severity  Severity               `json:"severity"`
	Timestamp time.Time              `json:"timestamp"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Labels    map[string]string      `json:"labels,omitempty"`
}

// Finding represents a correlation finding
type Finding struct {
	ID          FindingID   `json:"id"`
	Type        FindingType `json:"type"`
	Severity    Severity    `json:"severity"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Timestamp   time.Time   `json:"timestamp"`
	EventIDs    []EventID   `json:"event_ids,omitempty"`
	Insights    []string    `json:"insights,omitempty"`
}

type (
	FindingID   string
	FindingType string
)

// Correlation represents a correlation between events
type Correlation struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	EventIDs    []string               `json:"event_ids"`
	Confidence  float64                `json:"confidence"`
	Description string                 `json:"description"`
	Timestamp   time.Time              `json:"timestamp"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// HealthStatus represents system health
type HealthStatus struct {
	Status    string                 `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// Configuration for services
type Config struct {
	ServerPort int    `json:"server_port"`
	LogLevel   string `json:"log_level"`
	APIKey     string `json:"api_key,omitempty"`
}
