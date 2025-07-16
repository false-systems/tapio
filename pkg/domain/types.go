// Package domain contains core business types with zero dependencies
// These types form the foundation of the Tapio system
package domain

import (
	"time"
)

// Event represents a single observable occurrence in the system
type Event struct {
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Source    string                 `json:"source"`
	Type      string                 `json:"type"`
	Severity  SeverityLevel          `json:"severity"`
	Data      map[string]interface{} `json:"data"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// SeverityLevel represents the severity of an event
type SeverityLevel int

const (
	SeverityInfo SeverityLevel = iota
	SeverityWarning
	SeverityError
	SeverityCritical
)

// String returns the string representation of severity
func (s SeverityLevel) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	case SeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// TimeWindow represents a time range for analysis
type TimeWindow struct {
	Start    time.Time     `json:"start"`
	End      time.Time     `json:"end"`
	Duration time.Duration `json:"duration"`
}

// Evidence represents supporting data for a correlation or insight
type Evidence struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Source      string                 `json:"source"`
	Content     interface{}            `json:"content"`
	Confidence  float64                `json:"confidence"`
	Description string                 `json:"description"`
	Timestamp   time.Time              `json:"timestamp"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Correlation represents a relationship between events
type Correlation struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Events      []string               `json:"events"` // Event IDs
	Confidence  float64                `json:"confidence"`
	Description string                 `json:"description"`
	Evidence    []Evidence             `json:"evidence"`
	Timestamp   time.Time              `json:"timestamp"`
	TTL         time.Duration          `json:"ttl"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Prediction represents a future event prediction
type Prediction struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Event       string                 `json:"event"`
	Probability float64                `json:"probability"`
	TimeWindow  TimeWindow             `json:"time_window"`
	Confidence  float64                `json:"confidence"`
	Evidence    []Evidence             `json:"evidence"`
	Description string                 `json:"description"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Insight represents an actionable finding from analysis
type Insight struct {
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	Description     string       `json:"description"`
	Severity        string       `json:"severity"`
	Category        string       `json:"category"`
	ResourceName    string       `json:"resource_name"`
	Namespace       string       `json:"namespace"`
	Timestamp       time.Time    `json:"timestamp"`
	Evidence        []Evidence   `json:"evidence"`
	RootCause       *RootCause   `json:"root_cause,omitempty"`
	Prediction      *Prediction  `json:"prediction,omitempty"`
	ActionableItems []ActionItem `json:"actionable_items"`
}

// RootCause represents the root cause analysis result
type RootCause struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Evidence    []Evidence `json:"evidence"`
	Confidence  float64    `json:"confidence"`
}

// ActionItem represents a recommended action
type ActionItem struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Command     string `json:"command,omitempty"`
	Impact      string `json:"impact"`
	Risk        string `json:"risk"`
}

// Pattern represents a recurring pattern in events
type Pattern struct {
	ID               string        `json:"id"`
	Type             string        `json:"type"`
	Description      string        `json:"description"`
	EventSignatures  []string      `json:"event_signatures"`
	TimeWindow       TimeWindow    `json:"time_window"`
	OccurrenceCount  int           `json:"occurrence_count"`
	Confidence       float64       `json:"confidence"`
	LastOccurrence   time.Time     `json:"last_occurrence"`
	PredictedNext    *time.Time    `json:"predicted_next,omitempty"`
}

// MetricsReport represents system metrics at a point in time
type MetricsReport struct {
	Timestamp        time.Time              `json:"timestamp"`
	EventsProcessed  int64                  `json:"events_processed"`
	CorrelationsFound int64                  `json:"correlations_found"`
	InsightsGenerated int64                  `json:"insights_generated"`
	ProcessingLatency time.Duration          `json:"processing_latency"`
	ErrorRate        float64                `json:"error_rate"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}