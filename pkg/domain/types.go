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

// Event represents a comprehensive system event supporting multiple use cases
type Event struct {
	// Core fields (backward compatible)
	ID        EventID                `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Type      EventType              `json:"type"`
	Source    SourceType             `json:"source"`
	Data      map[string]interface{} `json:"data"`

	// Classification fields
	Category   string        `json:"category,omitempty"`
	Severity   EventSeverity `json:"severity,omitempty"`
	Confidence float64       `json:"confidence,omitempty"`

	// Context (structured for OTEL correlation)
	Context EventContext `json:"context,omitempty"`

	// Message and tags
	Message string   `json:"message,omitempty"`
	Tags    []string `json:"tags,omitempty"`

	// Enrichment fields
	Attributes map[string]interface{} `json:"attributes,omitempty"`
	AiFeatures map[string]interface{} `json:"ai_features,omitempty"`
	Semantic   map[string]interface{} `json:"semantic,omitempty"`
	Anomaly    map[string]interface{} `json:"anomaly,omitempty"`
	Behavioral map[string]interface{} `json:"behavioral,omitempty"`
	Causality  *CausalityContext      `json:"causality,omitempty"`

	// Payload for specific event types
	Payload EventPayload `json:"payload,omitempty"`
}

// EventContext provides structured context for events
type EventContext struct {
	// Service context
	Service   string `json:"service,omitempty"`
	Component string `json:"component,omitempty"`

	// Kubernetes context
	Namespace string `json:"namespace,omitempty"`
	Host      string `json:"host,omitempty"`
	Node      string `json:"node,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Container string `json:"container,omitempty"`

	// Process context
	PID  int    `json:"pid,omitempty"`
	UID  int    `json:"uid,omitempty"`
	GID  int    `json:"gid,omitempty"`
	Comm string `json:"comm,omitempty"`

	// Labels and metadata
	Labels   map[string]string      `json:"labels,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Trace context for OTEL
	TraceID string `json:"trace_id,omitempty"`
	SpanID  string `json:"span_id,omitempty"`
}

// EventSeverity represents event severity levels
type EventSeverity string

const (
	EventSeverityDebug    EventSeverity = "debug"
	EventSeverityInfo     EventSeverity = "info"
	EventSeverityLow      EventSeverity = "low"
	EventSeverityMedium   EventSeverity = "medium"
	EventSeverityWarning  EventSeverity = "warning"
	EventSeverityHigh     EventSeverity = "high"
	EventSeverityError    EventSeverity = "error"
	EventSeverityCritical EventSeverity = "critical"
)

// EventPayload is an interface for event-specific payloads
type EventPayload interface {
	GetType() string
}

// Common event payload types

// MemoryEventPayload for memory-related events
type MemoryEventPayload struct {
	Usage     float64 `json:"usage_percent"`
	Available uint64  `json:"available_bytes"`
	Total     uint64  `json:"total_bytes"`
	RSS       uint64  `json:"rss_bytes,omitempty"`
	Cache     uint64  `json:"cache_bytes,omitempty"`
}

func (m MemoryEventPayload) GetType() string { return "memory" }

// CPUEventPayload for CPU-related events
type CPUEventPayload struct {
	Usage            float64 `json:"usage_percent"`
	UserTime         uint64  `json:"user_time_ns"`
	SystemTime       uint64  `json:"system_time_ns"`
	ThrottledTime    uint64  `json:"throttled_time_ns,omitempty"`
	ThrottledPeriods uint64  `json:"throttled_periods,omitempty"`
}

func (c CPUEventPayload) GetType() string { return "cpu" }

// NetworkEventPayload for network-related events
type NetworkEventPayload struct {
	Protocol    string `json:"protocol"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Bytes       uint64 `json:"bytes,omitempty"`
	Packets     uint64 `json:"packets,omitempty"`
	Latency     uint64 `json:"latency_ms,omitempty"`
}

func (n NetworkEventPayload) GetType() string { return "network" }

// DiskEventPayload for disk-related events
type DiskEventPayload struct {
	Device    string  `json:"device"`
	Mount     string  `json:"mount,omitempty"`
	Usage     float64 `json:"usage_percent"`
	Available uint64  `json:"available_bytes"`
	Total     uint64  `json:"total_bytes"`
	IOPs      uint64  `json:"iops,omitempty"`
}

func (d DiskEventPayload) GetType() string { return "disk" }

// SystemEventPayload for system-level events
type SystemEventPayload struct {
	Subsystem string                 `json:"subsystem"`
	EventType string                 `json:"event_type"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

func (s SystemEventPayload) GetType() string { return "system" }

// ServiceEventPayload for service-related events
type ServiceEventPayload struct {
	ServiceName string `json:"service_name"`
	EventType   string `json:"event_type"`
	OldState    string `json:"old_state,omitempty"`
	NewState    string `json:"new_state,omitempty"`
	Message     string `json:"message,omitempty"`
}

func (s ServiceEventPayload) GetType() string { return "service" }

// KubernetesEventPayload for Kubernetes events
type KubernetesEventPayload struct {
	Resource  ResourceInfo `json:"resource"`
	EventType string       `json:"event_type"`
	Reason    string       `json:"reason"`
	Message   string       `json:"message"`
}

func (k KubernetesEventPayload) GetType() string { return "kubernetes" }

// ResourceInfo contains Kubernetes resource information
type ResourceInfo struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid,omitempty"`
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
	ID          string              `json:"id"`
	Type        string              `json:"type"`
	Events      []string            `json:"events"`
	Confidence  float64             `json:"confidence"`
	Timestamp   time.Time           `json:"timestamp"`
	Pattern     PatternSignature    `json:"pattern,omitempty"`
	Description string              `json:"description,omitempty"`
	Metadata    CorrelationMetadata `json:"metadata,omitempty"`
}

// String converts CorrelationID to string
func (c CorrelationID) String() string {
	return string(c)
}

// String converts PatternSignature to string
func (p PatternSignature) String() string {
	return string(p)
}

// Float64 converts ConfidenceScore to float64
func (c ConfidenceScore) Float64() float64 {
	return float64(c)
}

// Insight represents an analytical insight
type Insight struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Severity    SeverityLevel          `json:"severity"`
	Source      string                 `json:"source,omitempty"`
	Data        map[string]interface{} `json:"data"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
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
	ID        string             `json:"id"`
	Source    string             `json:"source"`
	Metrics   map[string]float64 `json:"metrics"`
	Labels    map[string]string  `json:"labels"`
	Timestamp time.Time          `json:"timestamp"`
}

// CausalityContext represents causality information for events
type CausalityContext struct {
	RootCause   string                 `json:"root_cause"`
	CausalChain []string               `json:"causal_chain"`
	Confidence  float64                `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata"`
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
	Timestamp time.Time              `json:"timestamp"`
	Type      string                 `json:"type"`
	PID       int                    `json:"pid"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// BehavioralContext represents behavioral patterns and context
type BehavioralContext struct {
	Pattern    string                 `json:"pattern"`
	Frequency  float64                `json:"frequency"`
	Confidence float64                `json:"confidence"`
	TimeWindow TimeWindow             `json:"time_window"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
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
	Timestamp   time.Time              `json:"timestamp"`
	Message     string                 `json:"message"`
	OldState    string                 `json:"old_state,omitempty"`
	NewState    string                 `json:"new_state,omitempty"`
	Reason      string                 `json:"reason,omitempty"`
	Properties  map[string]interface{} `json:"properties,omitempty"`
	Data        map[string]interface{} `json:"data,omitempty"`
}

// LogEvent represents a journald log event
type LogEvent struct {
	Timestamp  time.Time              `json:"timestamp"`
	Unit       string                 `json:"unit"`
	Message    string                 `json:"message"`
	Priority   int                    `json:"priority"`
	Hostname   string                 `json:"hostname,omitempty"`
	Identifier string                 `json:"identifier,omitempty"`
	PID        int                    `json:"pid,omitempty"`
	UID        int                    `json:"uid,omitempty"`
	Fields     map[string]interface{} `json:"fields,omitempty"`
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
	Status    string    `json:"status"`
	Summary   string    `json:"summary"`
	Problems  []Problem `json:"problems"`
	Timestamp time.Time `json:"timestamp"`
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
	CorrelationTypeResource CorrelationType = "resource"
)

// Additional severity constants for compatibility
const (
	SeverityError SeverityLevel = "error"
	SeverityWarn  SeverityLevel = "warning"
	SeverityInfo  SeverityLevel = "info"
)

const (
	FindingTypeAnomaly    FindingType = "anomaly"
	FindingTypePattern    FindingType = "pattern"
	FindingTypePrediction FindingType = "prediction"
)

// NOTE: Rule, RuleCondition, RuleAction, RuleMatch, and QueryCriteria
// are already defined in interfaces.go, so we don't redefine them here

// EventReference represents a reference to an event
type EventReference struct {
	ID           EventID
	EventID      EventID // Alias for compatibility
	Timestamp    time.Time
	Type         EventType
	Source       SourceType
	Role         string  // Role in correlation
	Relationship string  // Type of relationship
	Weight       float64 // Weight/importance
}

// CorrelationID is a strongly-typed correlation identifier
type CorrelationID string

// PatternSignature represents a unique pattern signature
type PatternSignature string

// ConfidenceScore represents a confidence score (0.0-1.0)
type ConfidenceScore float64

// FloatToConfidenceScore converts a float to a ConfidenceScore with bounds checking
func FloatToConfidenceScore(f float64) ConfidenceScore {
	if f < 0.0 {
		return ConfidenceScore(0.0)
	}
	if f > 1.0 {
		return ConfidenceScore(1.0)
	}
	return ConfidenceScore(f)
}

// CorrelationMetadata contains metadata about a correlation
type CorrelationMetadata struct {
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ProcessedAt   time.Time // When correlation was processed
	Source        string
	Algorithm     string
	Version       string
	SchemaVersion string // Schema version for compatibility
	ProcessedBy   string // Pattern processor name
	Parameters    map[string]interface{}
}
