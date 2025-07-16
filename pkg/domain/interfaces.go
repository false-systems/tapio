package domain

import (
	"context"
	"time"
)

// EventSource provides events from various sources
type EventSource interface {
	// GetEvents retrieves events within the specified time window
	GetEvents(ctx context.Context, window TimeWindow) ([]Event, error)
	// Subscribe creates a subscription for real-time events
	Subscribe(ctx context.Context) (<-chan Event, error)
	// GetSourceType returns the type of this event source
	GetSourceType() string
}

// CorrelationEngine analyzes events to find relationships
type CorrelationEngine interface {
	// ProcessEvent processes a single event
	ProcessEvent(ctx context.Context, event Event) error
	// GetCorrelations retrieves correlations within a time window
	GetCorrelations(ctx context.Context, window TimeWindow) ([]Correlation, error)
	// GetInsights retrieves insights generated from correlations
	GetInsights(ctx context.Context, window TimeWindow) ([]Insight, error)
}

// RuleEngine evaluates rules against events
type RuleEngine interface {
	// EvaluateEvent evaluates rules against a single event
	EvaluateEvent(ctx context.Context, event Event) ([]RuleMatch, error)
	// RegisterRule registers a new rule
	RegisterRule(ctx context.Context, rule Rule) error
	// GetRule retrieves a rule by ID
	GetRule(ctx context.Context, id string) (*Rule, error)
	// ListRules lists all registered rules
	ListRules(ctx context.Context) ([]Rule, error)
}

// Rule represents a correlation rule
type Rule struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Enabled     bool                   `json:"enabled"`
	Priority    int                    `json:"priority"`
	Conditions  []RuleCondition        `json:"conditions"`
	Actions     []RuleAction           `json:"actions"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// RuleCondition represents a condition for rule matching
type RuleCondition struct {
	Type     string                 `json:"type"`
	Field    string                 `json:"field"`
	Operator string                 `json:"operator"`
	Value    interface{}            `json:"value"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// RuleAction represents an action to take when a rule matches
type RuleAction struct {
	Type     string                 `json:"type"`
	Target   string                 `json:"target"`
	Params   map[string]interface{} `json:"params"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// RuleMatch represents a rule that matched an event
type RuleMatch struct {
	RuleID      string    `json:"rule_id"`
	EventID     string    `json:"event_id"`
	Timestamp   time.Time `json:"timestamp"`
	Confidence  float64   `json:"confidence"`
	Evidence    []Evidence `json:"evidence"`
}

// EventStore persists and retrieves events
type EventStore interface {
	// Store persists an event
	Store(ctx context.Context, event Event) error
	// Get retrieves an event by ID
	Get(ctx context.Context, id string) (*Event, error)
	// Query retrieves events matching criteria
	Query(ctx context.Context, criteria QueryCriteria) ([]Event, error)
	// Delete removes an event
	Delete(ctx context.Context, id string) error
}

// QueryCriteria represents criteria for querying events
type QueryCriteria struct {
	TimeWindow TimeWindow             `json:"time_window"`
	Types      []string               `json:"types,omitempty"`
	Sources    []string               `json:"sources,omitempty"`
	Severities []SeverityLevel        `json:"severities,omitempty"`
	Tags       map[string]interface{} `json:"tags,omitempty"`
	Limit      int                    `json:"limit,omitempty"`
	Offset     int                    `json:"offset,omitempty"`
}

// MetricsCollector collects and reports system metrics
type MetricsCollector interface {
	// RecordEvent records an event was processed
	RecordEvent(eventType string)
	// RecordCorrelation records a correlation was found
	RecordCorrelation(correlationType string)
	// RecordInsight records an insight was generated
	RecordInsight(insightType string)
	// RecordError records an error occurred
	RecordError(errorType string)
	// GetReport returns current metrics
	GetReport() MetricsReport
}

// Logger provides structured logging
type Logger interface {
	Debug(msg string, fields ...interface{})
	Info(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	WithField(key string, value interface{}) Logger
	WithFields(fields map[string]interface{}) Logger
}