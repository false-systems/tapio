package domain

import "time"

// =============================================================================
// CORE EVENT ABSTRACTION
// =============================================================================

// EventID is a strongly-typed event identifier
type EventID string

// EventType represents the type of event
type EventType string

const (
	EventTypeSystem     EventType = "system"
	EventTypeKubernetes EventType = "kubernetes"
	EventTypeService    EventType = "service"
	EventTypeLog        EventType = "log"
	EventTypeNetwork    EventType = "network"
	EventTypeProcess    EventType = "process"
	EventTypeMemory     EventType = "memory"
	EventTypeCPU        EventType = "cpu"
	EventTypeDisk       EventType = "disk"
)

// SourceType represents the source of an event
type SourceType string

// Alias for backward compatibility
type Source = SourceType

const (
	SourceEBPF     SourceType = "ebpf"
	SourceK8s      SourceType = "kubernetes"
	SourceSystemd  SourceType = "systemd"
	SourceJournald SourceType = "journald"
	SourceCNI      SourceType = "cni"
	SourceCustom   SourceType = "custom"
)

// Core domain types - only the ones NOT already in interfaces.go

// Event represents a system event
type Event struct {
    ID         string                 `json:"id"`
    Timestamp  time.Time             `json:"timestamp"`
    Type       string                `json:"type"`
    Source     string                `json:"source"`
    Data       map[string]interface{} `json:"data"`
    Category   string                `json:"category,omitempty"`
    Confidence float64               `json:"confidence,omitempty"`
    Attributes map[string]interface{} `json:"attributes,omitempty"`
    AiFeatures map[string]interface{} `json:"ai_features,omitempty"`
    Semantic   map[string]interface{} `json:"semantic,omitempty"`
    Severity   string                `json:"severity,omitempty"`
    Anomaly    map[string]interface{} `json:"anomaly,omitempty"`
    Context    map[string]interface{} `json:"context,omitempty"`
    Behavioral map[string]interface{} `json:"behavioral,omitempty"`
    Causality  *CausalityContext      `json:"causality,omitempty"`
}

// Finding represents a correlation result  
type Finding struct {
    ID          string    `json:"id"`
    Type        string    `json:"type"`
    Confidence  float64   `json:"confidence"`
    Description string    `json:"description"`
    Events      []string  `json:"events"`
    Timestamp   time.Time `json:"timestamp"`
}

// Target represents a monitoring target
type Target struct {
    Type      string            `json:"type"`
    Name      string            `json:"name"`
    Namespace string            `json:"namespace,omitempty"`
    Labels    map[string]string `json:"labels,omitempty"`
}

// TimeWindow represents a time range for analysis
type TimeWindow struct {
    Start    time.Time     `json:"start"`
    End      time.Time     `json:"end"`
    Duration time.Duration `json:"duration"`
}

// Correlation represents a correlation between events
type Correlation struct {
    ID         string    `json:"id"`
    Type       string    `json:"type"`
    Events     []string  `json:"events"`
    Confidence float64   `json:"confidence"`
    Timestamp  time.Time `json:"timestamp"`
}

// Insight represents an analytical insight
type Insight struct {
    ID          string                 `json:"id"`
    Type        string                 `json:"type"`
    Title       string                 `json:"title"`
    Description string                 `json:"description"`
    Severity    SeverityLevel          `json:"severity"`
    Data        map[string]interface{} `json:"data"`
    Timestamp   time.Time              `json:"timestamp"`
}

// Evidence represents supporting evidence for a finding
type Evidence struct {
    Type        string                 `json:"type"`
    Source      string                 `json:"source"`
    Description string                 `json:"description"`
    Data        map[string]interface{} `json:"data"`
    Timestamp   time.Time              `json:"timestamp"`
}

// SeverityLevel represents the severity of an issue
type SeverityLevel string

const (
    SeverityLow      SeverityLevel = "low"
    SeverityMedium   SeverityLevel = "medium"
    SeverityHigh     SeverityLevel = "high"
    SeverityCritical SeverityLevel = "critical"
)

// MetricsReport represents a metrics collection report
type MetricsReport struct {
    ID        string                 `json:"id"`
    Source    string                 `json:"source"`
    Metrics   map[string]float64     `json:"metrics"`
    Labels    map[string]string      `json:"labels"`
    Timestamp time.Time              `json:"timestamp"`
}

// CausalityContext represents causality information for events
type CausalityContext struct {
    RootCause    string                 `json:"root_cause"`
    CausalChain  []string              `json:"causal_chain"`
    Confidence   float64               `json:"confidence"`
    Metadata     map[string]interface{} `json:"metadata"`
}

// Problem represents an issue detected in the system
type Problem struct {
    Resource    ResourceRef            `json:"resource"`
    Severity    SeverityLevel          `json:"severity"`
    Title       string                 `json:"title"`
    Description string                 `json:"description"`
    Evidence    []Evidence             `json:"evidence,omitempty"`
    Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ResourceRef represents a reference to a Kubernetes resource
type ResourceRef struct {
    Kind      string `json:"kind"`
    Name      string `json:"name"`
    Namespace string `json:"namespace,omitempty"`
}

// SystemEvent represents a system-level event from eBPF
type SystemEvent struct {
    ID        string                 `json:"id"`
    Timestamp time.Time             `json:"timestamp"`
    Type      string                `json:"type"`
    PID       int                   `json:"pid"`
    Data      map[string]interface{} `json:"data,omitempty"`
}

// BehavioralContext represents behavioral patterns and context
type BehavioralContext struct {
    Pattern     string                 `json:"pattern"`
    Frequency   float64               `json:"frequency"`
    Confidence  float64               `json:"confidence"`
    TimeWindow  TimeWindow            `json:"time_window"`
    Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ActionItem represents a recommended action
type ActionItem struct {
    ID          string                 `json:"id"`
    Type        string                 `json:"type"`
    Description string                 `json:"description"`
    Priority    string                 `json:"priority"`
    Command     string                 `json:"command,omitempty"`
    Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// AnomalyDimensions represents dimensions of an anomaly
type AnomalyDimensions struct {
    Temporal   float64 `json:"temporal"`
    Spatial    float64 `json:"spatial"`
    Behavioral float64 `json:"behavioral"`
    Contextual float64 `json:"contextual"`
}

// ServiceEvent represents a systemd service event
type ServiceEvent struct {
    ServiceName string                 `json:"service_name"`
    EventType   string                 `json:"event_type"`
    Timestamp   time.Time             `json:"timestamp"`
    Message     string                 `json:"message"`
    OldState    string                 `json:"old_state,omitempty"`
    NewState    string                 `json:"new_state,omitempty"`
    Reason      string                 `json:"reason,omitempty"`
    Properties  map[string]interface{} `json:"properties,omitempty"`
    Data        map[string]interface{} `json:"data,omitempty"`
}

// LogEvent represents a journald log event
type LogEvent struct {
    Timestamp   time.Time              `json:"timestamp"`
    Unit        string                 `json:"unit"`
    Message     string                 `json:"message"`
    Priority    int                    `json:"priority"`
    Hostname    string                 `json:"hostname,omitempty"`
    Identifier  string                 `json:"identifier,omitempty"`
    PID         int                    `json:"pid,omitempty"`
    UID         int                    `json:"uid,omitempty"`
    Fields      map[string]interface{} `json:"fields,omitempty"`
}

// Pattern represents a correlation pattern
type Pattern struct {
    ID          string                 `json:"id"`
    Name        string                 `json:"name"`
    Type        string                 `json:"type"`
    Description string                 `json:"description"`
    Conditions  []interface{}          `json:"conditions"`
    Actions     []interface{}          `json:"actions"`
    Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// CheckResult represents the result of a system check
type CheckResult struct {
    Status      string    `json:"status"`
    Summary     string    `json:"summary"`
    Problems    []Problem `json:"problems"`
    Timestamp   time.Time `json:"timestamp"`
}

// Severity represents severity levels for issues
type Severity = SeverityLevel

// Define additional severity constants if needed
const (
    SeverityWarning SeverityLevel = "warning"
)

// Additional types needed by interfaces.go
type CorrelationType string
type FindingID string
type FindingType string

const (
    CorrelationTypeTemporal CorrelationType = "temporal"
    CorrelationTypeSpatial  CorrelationType = "spatial"
    CorrelationTypeCausal   CorrelationType = "causal"
)

const (
    FindingTypeAnomaly     FindingType = "anomaly"
    FindingTypePattern     FindingType = "pattern"
    FindingTypePrediction  FindingType = "prediction"
)

// NOTE: Rule, RuleCondition, RuleAction, RuleMatch, and QueryCriteria 
// are already defined in interfaces.go, so we don't redefine them here
