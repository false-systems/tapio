package aggregator

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CorrelatorOutput represents standardized output from any correlator
type CorrelatorOutput struct {
	CorrelatorName    string
	CorrelatorVersion string
	Findings          []Finding
	Context           map[string]string // Structured context, not interface{}
	Confidence        float64
	ProcessingTime    time.Duration
	Timestamp         time.Time
}

// Finding represents a single insight from a correlator
type Finding struct {
	ID         string
	Type       string
	Severity   Severity
	Confidence float64
	Message    string
	Evidence   Evidence
	Impact     Impact
	Timestamp  time.Time
}

// Evidence contains structured proof of a finding
type Evidence struct {
	Events     []domain.UnifiedEvent
	Metrics    []MetricPoint
	Logs       []LogEntry
	Traces     []TraceSpan
	GraphPaths []GraphPath
	Attributes map[string]string // Additional attributes for synthesis rules (strongly typed)
}

// MetricPoint represents a metric data point
type MetricPoint struct {
	Name      string
	Value     float64
	Timestamp time.Time
	Labels    map[string]string
}

// LogEntry represents a log line
type LogEntry struct {
	Message   string
	Level     string
	Timestamp time.Time
	Source    string
}

// TraceSpan represents a trace span
type TraceSpan struct {
	TraceID   string
	SpanID    string
	Operation string
	Duration  time.Duration
}

// GraphPath represents a path in Neo4j
type GraphPath struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

// GraphNode represents a node in the graph
type GraphNode struct {
	ID     string
	Type   string
	Labels map[string]string
}

// GraphEdge represents an edge in the graph
type GraphEdge struct {
	From         string
	To           string
	Relationship string
	Properties   map[string]string
}

// Severity levels
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Impact describes the effect of a finding
type Impact struct {
	Scope       string
	Resources   []string
	Services    []string
	UserImpact  string
	Degradation string
}

// FinalResult is the aggregated output
type FinalResult struct {
	ID             string
	Summary        string
	RootCause      string
	Impact         string
	Remediation    Remediation
	Confidence     float64
	CausalChain    []CausalLink
	Timeline       []TimelineEvent
	Evidence       map[string]Evidence
	Syntheses      []SynthesisResult
	ProcessingTime time.Duration
	Correlators    []string
	Timestamp      time.Time
}

// Remediation describes how to fix the issue
type Remediation struct {
	Automatic     bool
	Steps         []string
	Commands      []string
	Preventive    []string
	EstimatedTime time.Duration
}

// CausalLink represents one step in the cause chain
type CausalLink struct {
	From       string
	To         string
	Relation   string
	Confidence float64
	Evidence   Evidence
	Timestamp  time.Time
}

// TimelineEvent for the sequence of events
type TimelineEvent struct {
	Time     time.Time
	Event    string
	Source   string
	Severity Severity
	Related  []string
}

// AggregationRule defines how to combine findings
type AggregationRule struct {
	Name        string
	Priority    int
	Condition   func([]*CorrelatorOutput) bool
	Aggregate   func([]*CorrelatorOutput) *FinalResult
	Description string
}

// ConflictResolution strategies
type ConflictResolution string

const (
	ConflictResolutionHighestConfidence ConflictResolution = "highest_confidence"
	ConflictResolutionMostSpecific      ConflictResolution = "most_specific"
	ConflictResolutionMostRecent        ConflictResolution = "most_recent"
	ConflictResolutionConsensus         ConflictResolution = "consensus"
)

// AggregatorConfig for initialization
type AggregatorConfig struct {
	MinConfidence      float64
	ConflictResolution ConflictResolution
	TimeoutDuration    time.Duration
	MaxFindings        int
	EnableLearning     bool
}

// CorrelationQuery defines a query for correlations
type CorrelationQuery struct {
	ResourceType string
	Namespace    string
	Name         string
	TimeWindow   time.Duration
	Filters      map[string]string
}

// AggregatedResult represents the API response for a correlation query
type AggregatedResult struct {
	ID             string
	Resource       ResourceRef
	RootCause      *RootCause
	Impact         *ImpactAnalysis
	Remediation    *RemediationPlan
	CausalChain    []CausalLink
	Timeline       []TimelineEvent
	Evidence       map[string]Evidence
	Confidence     float64
	ProcessingTime time.Duration
	CreatedAt      time.Time
	Correlators    []string
}

// ResourceRef identifies a Kubernetes resource
type ResourceRef struct {
	Type      string `json:"type"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid,omitempty"`
}

// RootCause describes the primary cause of an issue
type RootCause struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Confidence  float64  `json:"confidence"`
	Evidence    Evidence `json:"evidence"`
}

// ImpactAnalysis describes the impact of an issue
type ImpactAnalysis struct {
	Scope        string   `json:"scope"`
	Affected     []string `json:"affected"`
	Severity     Severity `json:"severity"`
	UserImpact   string   `json:"user_impact,omitempty"`
	BusinessCost string   `json:"business_cost,omitempty"`
}

// RemediationPlan provides steps to fix the issue
type RemediationPlan struct {
	Automatic     bool              `json:"automatic"`
	Steps         []RemediationStep `json:"steps"`
	EstimatedTime time.Duration     `json:"estimated_time"`
	RiskLevel     string            `json:"risk_level"`
	Alternatives  []RemediationPlan `json:"alternatives,omitempty"`
}

// RemediationStep is a single step in remediation
type RemediationStep struct {
	Order       int    `json:"order"`
	Description string `json:"description"`
	Command     string `json:"command,omitempty"`
	Manual      bool   `json:"manual"`
	RiskLevel   string `json:"risk_level"`
}

// CorrelationList is a paginated list of correlations
type CorrelationList struct {
	Correlations []CorrelationSummary `json:"correlations"`
	Total        int                  `json:"total"`
	Limit        int                  `json:"limit"`
	Offset       int                  `json:"offset"`
}

// CorrelationSummary is a summary of a correlation
type CorrelationSummary struct {
	ID        string      `json:"id"`
	Resource  ResourceRef `json:"resource"`
	RootCause string      `json:"root_cause"`
	Severity  Severity    `json:"severity"`
	CreatedAt time.Time   `json:"created_at"`
}

// CorrelationFeedback represents user feedback on a correlation
type CorrelationFeedback struct {
	UserID    string `json:"user_id"`
	Useful    bool   `json:"useful"`
	Comment   string `json:"comment,omitempty"`
	CorrectRC bool   `json:"correct_root_cause"`
}

// Common errors
var (
	ErrNotFound     = fmt.Errorf("correlation not found")
	ErrInvalidQuery = fmt.Errorf("invalid query parameters")
	ErrTimeout      = fmt.Errorf("query timeout")
	ErrNoData       = fmt.Errorf("insufficient data for analysis")
)

// CorrelationStorage defines the interface for storing and retrieving correlations
type CorrelationStorage interface {
	// Store saves a correlation result
	Store(ctx context.Context, result *FinalResult) error

	// GetByID retrieves a correlation by its ID
	GetByID(ctx context.Context, id string) (*StoredCorrelation, error)

	// GetRecent retrieves recent correlations
	GetRecent(ctx context.Context, limit int) ([]*StoredCorrelation, error)

	// GetByResource retrieves correlations for a specific resource
	GetByResource(ctx context.Context, resourceType, namespace, name string) ([]*StoredCorrelation, error)

	// StoreFeedback stores user feedback for a correlation
	StoreFeedback(ctx context.Context, correlationID string, feedback CorrelationFeedback) error

	// HealthCheck verifies the storage is healthy
	HealthCheck(ctx context.Context) error
}

// GraphStore defines the interface for graph database operations
type GraphStore interface {
	// ExecuteQuery runs a graph query and returns results
	ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) (interface{}, error)

	// HealthCheck verifies the graph store is healthy
	HealthCheck(ctx context.Context) error
}

// StoredCorrelation represents a correlation stored in the backend
type StoredCorrelation struct {
	ID           string
	ResourceType string
	Namespace    string
	Name         string
	RootCause    string
	Severity     Severity
	Confidence   float64
	Timestamp    time.Time
	Correlators  []string
	Result       *FinalResult
}

// CorrelatorInfo contains information about a correlator
type CorrelatorInfo struct {
	Name        string
	Type        string
	Enabled     bool
	HealthCheck func(context.Context) error
}
