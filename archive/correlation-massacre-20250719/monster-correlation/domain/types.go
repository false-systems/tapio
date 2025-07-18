package domain

import (
	"context"
	"time"
)

// Event represents a correlation event - Single source of truth
type Event struct {
	// Core identification
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`

	// Classification
	Type     string   `json:"type"`
	Source   string   `json:"source"`
	Severity Severity `json:"severity"`
	Category Category `json:"category"`

	// Entity information
	Entity Entity `json:"entity"`

	// Event data
	Data        map[string]interface{} `json:"data,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	Labels      map[string]string      `json:"labels,omitempty"`
	Fingerprint string                 `json:"fingerprint,omitempty"`
}

// Entity represents a Kubernetes or system entity
type Entity struct {
	Type      string `json:"type"` // pod, node, service, etc.
	Name      string `json:"name"` // Resource name
	Namespace string `json:"namespace,omitempty"`
	Node      string `json:"node,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Container string `json:"container,omitempty"`
	Process   string `json:"process,omitempty"`
	UID       string `json:"uid,omitempty"`

	// Metadata for extensions
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Severity levels
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// Category represents event categories
type Category string

const (
	CategoryPerformance Category = "performance"
	CategorySecurity    Category = "security"
	CategoryReliability Category = "reliability"
	CategoryResource    Category = "resource"
	CategoryNetwork     Category = "network"
	CategoryCost        Category = "cost"
	CategoryCapacity    Category = "capacity"
)

// TimeWindow represents a time range for analysis
type TimeWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Duration returns the duration of the time window
func (tw TimeWindow) Duration() time.Duration {
	return tw.End.Sub(tw.Start)
}

// Contains checks if a timestamp falls within the window
func (tw TimeWindow) Contains(t time.Time) bool {
	return !t.Before(tw.Start) && !t.After(tw.End)
}

// Correlation represents a correlation between events
type Correlation struct {
	ID          string        `json:"id"`
	Type        string        `json:"type"`
	Events      []string      `json:"events"` // Event IDs
	Confidence  float64       `json:"confidence"`
	Description string        `json:"description"`
	Timestamp   time.Time     `json:"timestamp"`
	TTL         time.Duration `json:"ttl"`

	// Metadata for extensions
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Result represents a correlation result
type Result struct {
	ID          string        `json:"id"`
	RuleID      string        `json:"rule_id"`
	RuleName    string        `json:"rule_name"`
	Type        string        `json:"type"`
	Confidence  float64       `json:"confidence"`
	Description string        `json:"description"`
	Timestamp   time.Time     `json:"timestamp"`
	TTL         time.Duration `json:"ttl"`

	// Related data
	Events       []Event       `json:"events"`
	Correlations []Correlation `json:"correlations"`

	// Metadata for extensions
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Filter represents event filtering criteria
type Filter struct {
	// Basic filters
	Source   string            `json:"source,omitempty"`
	Type     string            `json:"type,omitempty"`
	Severity Severity          `json:"severity,omitempty"`
	Category Category          `json:"category,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`

	// Entity filters
	EntityType string `json:"entity_type,omitempty"`
	EntityName string `json:"entity_name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Node       string `json:"node,omitempty"`

	// Time filters
	Since time.Time `json:"since,omitempty"`
	Until time.Time `json:"until,omitempty"`

	// Result limits
	Limit int `json:"limit,omitempty"`
}

// Matches checks if an event matches the filter criteria
func (f Filter) Matches(event Event) bool {
	// Source filter
	if f.Source != "" && event.Source != f.Source {
		return false
	}

	// Type filter
	if f.Type != "" && event.Type != f.Type {
		return false
	}

	// Severity filter
	if f.Severity != "" && event.Severity != f.Severity {
		return false
	}

	// Category filter
	if f.Category != "" && event.Category != f.Category {
		return false
	}

	// Entity filters
	if f.EntityType != "" && event.Entity.Type != f.EntityType {
		return false
	}

	if f.EntityName != "" && event.Entity.Name != f.EntityName {
		return false
	}

	if f.Namespace != "" && event.Entity.Namespace != f.Namespace {
		return false
	}

	if f.Node != "" && event.Entity.Node != f.Node {
		return false
	}

	// Time filters
	if !f.Since.IsZero() && event.Timestamp.Before(f.Since) {
		return false
	}

	if !f.Until.IsZero() && event.Timestamp.After(f.Until) {
		return false
	}

	// Label filters
	if len(f.Labels) > 0 {
		for key, value := range f.Labels {
			if eventValue, exists := event.Labels[key]; !exists || eventValue != value {
				return false
			}
		}
	}

	return true
}

// Stats represents engine statistics
type Stats struct {
	// Event processing
	EventsProcessed uint64 `json:"events_processed"`
	EventsFiltered  uint64 `json:"events_filtered"`
	EventsDropped   uint64 `json:"events_dropped"`

	// Correlation processing
	CorrelationsFound   uint64 `json:"correlations_found"`
	CorrelationsActive  uint64 `json:"correlations_active"`
	CorrelationsExpired uint64 `json:"correlations_expired"`

	// Rule processing
	RulesActive   uint64 `json:"rules_active"`
	RulesExecuted uint64 `json:"rules_executed"`
	RulesMatched  uint64 `json:"rules_matched"`
	RulesFailed   uint64 `json:"rules_failed"`

	// Performance
	ProcessingLatency time.Duration `json:"processing_latency"`
	LastProcessedAt   time.Time     `json:"last_processed_at"`

	// Memory usage
	MemoryUsage uint64 `json:"memory_usage"`

	// Rule-specific stats
	RuleExecutionTime map[string]time.Duration `json:"rule_execution_time,omitempty"`
}

// RuleEngine processes events through rules to generate insights
type RuleEngine interface {
	// ProcessEvent processes a single event through all rules
	ProcessEvent(ctx context.Context, event Event) ([]Insight, error)
	
	// ProcessEvents processes multiple events
	ProcessEvents(ctx context.Context, events []Event) ([]Insight, error)
	
	// RegisterRule registers a new rule
	RegisterRule(rule Rule) error
	
	// GetRules returns all registered rules
	GetRules() []Rule
}


// Insight represents a correlation insight
type Insight struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Severity    Severity               `json:"severity"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Timestamp   time.Time              `json:"timestamp"`
	Evidence    []Event                `json:"evidence"`
	Actions     []ActionItem           `json:"actions"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Pattern represents a pattern that can be detected in events
type Pattern struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Type        string                 `json:"type"`
	Confidence  float64                `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Prediction represents a future prediction
type Prediction struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Confidence   float64                `json:"confidence"`
	TimeHorizon  time.Duration          `json:"time_horizon"`
	Description  string                 `json:"description"`
	Evidence     []Event                `json:"evidence"`
	Actions      []ActionItem           `json:"actions"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// ActionItem represents a recommended action
type ActionItem struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`        // "manual", "automated", "preventive"
	Priority    string    `json:"priority"`    // "low", "medium", "high", "critical"
	Description string    `json:"description"`
	Command     string    `json:"command,omitempty"`
	Deadline    time.Time `json:"deadline,omitempty"`
	Status      string    `json:"status"`      // "pending", "in_progress", "completed", "failed"
}

// SeverityLevel is an alias for Severity (for compatibility)
type SeverityLevel = Severity

// Additional severity constants
const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// RuleMatch represents a rule match result
type RuleMatch struct {
	RuleID      string                 `json:"rule_id"`
	RuleName    string                 `json:"rule_name"`
	Confidence  float64                `json:"confidence"`
	Evidence    []Event                `json:"evidence"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// SystemEvent represents a system-level event
type SystemEvent struct {
	Event
	SystemInfo map[string]interface{} `json:"system_info,omitempty"`
}

// Evidence represents evidence for a finding
type Evidence struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Events      []Event                `json:"events"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// AnomalyDimensions represents dimensions for anomaly detection
type AnomalyDimensions struct {
	Temporal    bool `json:"temporal"`
	Spatial     bool `json:"spatial"`
	Behavioral  bool `json:"behavioral"`
	Statistical bool `json:"statistical"`
}

// EventQuery represents a query for events
type EventQuery struct {
	TimeRange   *TimeRange `json:"time_range,omitempty"`
	ResourceIDs []string   `json:"resource_ids,omitempty"`
	Types       []string   `json:"types,omitempty"`
	MinSeverity Severity   `json:"min_severity,omitempty"`
	Limit       int        `json:"limit,omitempty"`
}

// TimeRange represents a time range for queries
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}
