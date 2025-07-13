package core

import (
	"context"
	"time"
)

// Core interfaces that break import cycles
// All other packages depend on these, but this package depends on nothing

// Engine defines the core correlation engine interface
type Engine interface {
	ProcessEvents(ctx context.Context, events []Event) ([]Result, error)
	ProcessWindow(ctx context.Context, window TimeWindow, events []Event) ([]Result, error)
	RegisterRule(rule Rule) error
	UnregisterRule(ruleID string) error
	Start(ctx context.Context) error
	Stop() error
	Health() error
}

// Rule defines the interface for correlation rules
type Rule interface {
	GetMetadata() RuleMetadata
	CheckRequirements(ctx context.Context, data *DataCollection) error
	Execute(ctx context.Context, ruleCtx *RuleContext) ([]Finding, error)
	GetConfidenceFactors() []ConfidenceFactor
}

// PatternDetector defines the interface for pattern detection
type PatternDetector interface {
	Name() string
	Configure(config interface{}) error
	Detect(ctx context.Context, events []Event) ([]PatternResult, error)
	GetConfig() interface{}
}

// EventStore defines the interface for event storage
type EventStore interface {
	Store(ctx context.Context, events []Event) error
	Query(ctx context.Context, filter EventFilter, window TimeWindow) ([]Event, error)
	GetMetrics(ctx context.Context, window TimeWindow) (map[string]MetricSeries, error)
}

// DataSource defines the interface for data sources
type DataSource interface {
	Name() string
	GetData(ctx context.Context, dataType string, config map[string]interface{}) (interface{}, error)
	Subscribe(ctx context.Context, dataType string, handler func(interface{})) error
	Health() error
}

// Core data types (no dependencies)

type Event struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Source      string                 `json:"source"`
	Type        string                 `json:"type"`
	Entity      Entity                 `json:"entity"`
	Attributes  map[string]interface{} `json:"attributes"`
	Fingerprint string                 `json:"fingerprint"`
	Labels      map[string]string      `json:"labels,omitempty"`
}

type Entity struct {
	Type      string            `json:"type"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Node      string            `json:"node,omitempty"`
	Pod       string            `json:"pod,omitempty"`
	Container string            `json:"container,omitempty"`
	Process   string            `json:"process,omitempty"`
	UID       string            `json:"uid"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type Result struct {
	ID           string                 `json:"id"`
	RuleID       string                 `json:"rule_id"`
	RuleName     string                 `json:"rule_name"`
	Type         string                 `json:"type"`
	Title        string                 `json:"title"`
	Description  string                 `json:"description"`
	Severity     Severity               `json:"severity"`
	Confidence   float64                `json:"confidence"`
	Events       []string               `json:"events"`
	Entities     []Entity               `json:"entities"`
	Evidence     []Evidence             `json:"evidence"`
	Metadata     map[string]interface{} `json:"metadata"`
	CreatedAt    time.Time              `json:"created_at"`
	TTL          time.Duration          `json:"ttl"`
}

type PatternResult struct {
	PatternID        string            `json:"pattern_id"`
	PatternName      string            `json:"pattern_name"`
	Type             string            `json:"type"`
	Confidence       float64           `json:"confidence"`
	Detected         time.Time         `json:"detected"`
	AffectedEntities []Entity          `json:"affected_entities"`
	Severity         Severity          `json:"severity"`
	Description      string            `json:"description"`
	Evidence         []Evidence        `json:"evidence"`
	Predictions      []Prediction      `json:"predictions"`
	Metadata         map[string]interface{} `json:"metadata"`
}

type TimeWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type EventFilter struct {
	Source       string            `json:"source,omitempty"`
	Type         string            `json:"type,omitempty"`
	EntityType   string            `json:"entity_type,omitempty"`
	EntityName   string            `json:"entity_name,omitempty"`
	Namespace    string            `json:"namespace,omitempty"`
	Node         string            `json:"node,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Since        time.Time         `json:"since,omitempty"`
	Until        time.Time         `json:"until,omitempty"`
	Limit        int               `json:"limit,omitempty"`
}

type MetricSeries struct {
	Name      string                 `json:"name"`
	Labels    map[string]string      `json:"labels"`
	Points    []MetricPoint          `json:"points"`
	Metadata  map[string]interface{} `json:"metadata"`
}

type MetricPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// Support types

type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityError
	SeverityCritical
)

func (s Severity) String() string {
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

type Evidence struct {
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Data        interface{} `json:"data"`
	Confidence  float64     `json:"confidence"`
}

type Prediction struct {
	Event       string        `json:"event"`
	TimeToEvent time.Duration `json:"time_to_event"`
	Confidence  float64       `json:"confidence"`
	Factors     []string      `json:"factors"`
	Mitigation  []string      `json:"mitigation"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type RuleMetadata struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Tags        []string `json:"tags"`
}

type DataCollection struct {
	Events      []Event                `json:"events"`
	Metrics     map[string]MetricSeries `json:"metrics"`
	Window      TimeWindow             `json:"window"`
	Sources     []string               `json:"sources"`
}

type RuleContext struct {
	RuleID         string                 `json:"rule_id"`
	CorrelationID  string                 `json:"correlation_id"`
	Window         TimeWindow             `json:"window"`
	Events         []Event                `json:"events"`
	Metrics        map[string]MetricSeries `json:"metrics"`
	EventsBySource map[string][]Event     `json:"events_by_source"`
	EventsByType   map[string][]Event     `json:"events_by_type"`
	EventsByEntity map[string][]Event     `json:"events_by_entity"`
	Metadata       map[string]string      `json:"metadata"`
}

type Finding struct {
	ID          string                 `json:"id"`
	RuleID      string                 `json:"rule_id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Severity    Severity               `json:"severity"`
	Confidence  float64                `json:"confidence"`
	Resource    ResourceInfo           `json:"resource,omitempty"`
	Evidence    []Evidence             `json:"evidence"`
	Prediction  *Prediction            `json:"prediction,omitempty"`
	Tags        []string               `json:"tags"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

type ResourceInfo struct {
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type ConfidenceFactor struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
	Value  float64 `json:"value"`
}