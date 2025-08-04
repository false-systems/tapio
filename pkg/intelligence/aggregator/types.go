package aggregator

import (
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
	Attributes map[string]interface{} // Additional attributes for synthesis rules
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
