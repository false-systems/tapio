package domain

import (
	"context"
	"fmt"
	"time"
)

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
	SourceRuntime  SourceType = "runtime"
)

// Aliases for backward compatibility
const (
	SourceKubernetes = SourceK8s
)

// Severity represents the severity level
type Severity string

const (
	SeverityTrace    Severity = "trace"
	SeverityDebug    Severity = "debug"
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Event is the core event abstraction - all events implement this
type Event struct {
	// Core identification
	ID        EventID    `json:"id"`
	Type      EventType  `json:"type"`
	Source    SourceType `json:"source"`
	Timestamp time.Time  `json:"timestamp"`

	// Content and context
	Payload  EventPayload  `json:"payload"`
	Context  EventContext  `json:"context"`
	Metadata EventMetadata `json:"metadata"`

	// Correlation and analysis
	Severity    Severity         `json:"severity"`
	Confidence  float64          `json:"confidence"`
	Fingerprint EventFingerprint `json:"fingerprint"`
}

// EventPayload contains the actual event data - strongly typed
type EventPayload interface {
	PayloadType() string
	Validate() error
}

// EventContext provides environmental context
type EventContext struct {
	// Resource context
	Resource  *ResourceRef `json:"resource,omitempty"`
	Host      string       `json:"host,omitempty"`
	Cluster   string       `json:"cluster,omitempty"`
	Namespace string       `json:"namespace,omitempty"`

	// Process context
	PID       *int32 `json:"pid,omitempty"`
	UID       *int32 `json:"uid,omitempty"`
	GID       *int32 `json:"gid,omitempty"`
	Container string `json:"container,omitempty"`

	// Labels and tags
	Labels Labels `json:"labels,omitempty"`
	Tags   Tags   `json:"tags,omitempty"`
}

// EventMetadata contains processing metadata
type EventMetadata struct {
	SchemaVersion string            `json:"schema_version"`
	ProcessedAt   time.Time         `json:"processed_at"`
	ProcessedBy   string            `json:"processed_by"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// EventFingerprint uniquely identifies similar events for deduplication
type EventFingerprint struct {
	Hash      string            `json:"hash"`
	Signature string            `json:"signature"`
	Fields    map[string]string `json:"fields"`
}

// =============================================================================
// SPECIFIC EVENT PAYLOADS - TYPE SAFE
// =============================================================================

// SystemEventPayload for eBPF system events
type SystemEventPayload struct {
	Syscall    string            `json:"syscall,omitempty"`
	ReturnCode int32             `json:"return_code,omitempty"`
	Arguments  map[string]string `json:"arguments,omitempty"`

	// Memory events
	MemoryUsage *int64 `json:"memory_usage,omitempty"`
	MemoryLimit *int64 `json:"memory_limit,omitempty"`

	// Network events
	SourceIP      string `json:"source_ip,omitempty"`
	DestIP        string `json:"dest_ip,omitempty"`
	Port          *int32 `json:"port,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	BytesSent     *int64 `json:"bytes_sent,omitempty"`
	BytesReceived *int64 `json:"bytes_received,omitempty"`
}

func (p SystemEventPayload) PayloadType() string { return "system" }
func (p SystemEventPayload) Validate() error {
	// Add validation logic
	return nil
}

// KubernetesEventPayload for K8s events
type KubernetesEventPayload struct {
	Resource  ResourceRef `json:"resource"`
	EventType string      `json:"event_type"`
	Reason    string      `json:"reason"`
	Message   string      `json:"message"`
	Count     int32       `json:"count"`

	// State changes
	OldState string `json:"old_state,omitempty"`
	NewState string `json:"new_state,omitempty"`

	// Resource details
	ResourceVersion string `json:"resource_version,omitempty"`
	FieldPath       string `json:"field_path,omitempty"`
}

func (p KubernetesEventPayload) PayloadType() string { return "kubernetes" }
func (p KubernetesEventPayload) Validate() error {
	if p.Resource.Kind == "" || p.Resource.Name == "" {
		return fmt.Errorf("resource kind and name are required")
	}
	return nil
}

// ServiceEventPayload for systemd events
type ServiceEventPayload struct {
	ServiceName string            `json:"service_name"`
	EventType   string            `json:"event_type"`
	OldState    string            `json:"old_state,omitempty"`
	NewState    string            `json:"new_state,omitempty"`
	ExitCode    *int32            `json:"exit_code,omitempty"`
	Signal      *int32            `json:"signal,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
}

func (p ServiceEventPayload) PayloadType() string { return "service" }
func (p ServiceEventPayload) Validate() error {
	if p.ServiceName == "" {
		return fmt.Errorf("service_name is required")
	}
	return nil
}

// State returns the current state (NewState if available, otherwise OldState)
func (p ServiceEventPayload) State() string {
	if p.NewState != "" {
		return p.NewState
	}
	return p.OldState
}

// LogEventPayload for journald/log events
type LogEventPayload struct {
	Message    string            `json:"message"`
	Unit       string            `json:"unit,omitempty"`
	Priority   int32             `json:"priority"`
	Facility   string            `json:"facility,omitempty"`
	Identifier string            `json:"identifier,omitempty"`
	Fields     map[string]string `json:"fields,omitempty"`
}

func (p LogEventPayload) PayloadType() string { return "log" }
func (p LogEventPayload) Validate() error {
	if p.Message == "" {
		return fmt.Errorf("message is required")
	}
	return nil
}

// =============================================================================
// SUPPORT TYPES
// =============================================================================

// ResourceRef represents a reference to a Kubernetes resource
type ResourceRef struct {
	APIVersion string `json:"api_version,omitempty"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	UID        string `json:"uid,omitempty"`
}

// Labels represents key-value labels
type Labels map[string]string

// Tags represents categorization tags
type Tags []string

// TimeWindow represents a time range
type TimeWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func (tw TimeWindow) Duration() time.Duration {
	return tw.End.Sub(tw.Start)
}

func (tw TimeWindow) Contains(t time.Time) bool {
	return !t.Before(tw.Start) && !t.After(tw.End)
}

// =============================================================================
// CORRELATION AND FINDINGS
// =============================================================================

// CorrelationID uniquely identifies a correlation
type CorrelationID string

// FindingID uniquely identifies a finding
type FindingID string

// Correlation represents relationships between events
type Correlation struct {
	ID         CorrelationID       `json:"id"`
	Type       CorrelationType     `json:"type"`
	Events     []EventReference    `json:"events"`
	Confidence ConfidenceScore     `json:"confidence"`
	Timestamp  time.Time           `json:"timestamp"`
	TimeWindow TimeWindow          `json:"time_window"`
	Pattern    PatternSignature    `json:"pattern"`
	Metadata   CorrelationMetadata `json:"metadata"`
	// Additional fields for correlation engine compatibility
	Description string             `json:"description"`
	Context     CorrelationContext `json:"context"`
	Findings    []Finding          `json:"findings"`
}

// CorrelationType defines types of correlations
type CorrelationType string

const (
	CorrelationCausal       CorrelationType = "causal"
	CorrelationTemporal     CorrelationType = "temporal"
	CorrelationSpatial      CorrelationType = "spatial"
	CorrelationPatternBased CorrelationType = "pattern"
	CorrelationAnomaly      CorrelationType = "anomaly"
	// Additional types for correlation engine
	CorrelationTypeResource    CorrelationType = "resource"
	CorrelationTypeNetwork     CorrelationType = "network"
	CorrelationTypeService     CorrelationType = "service"
	CorrelationTypeSecurity    CorrelationType = "security"
	CorrelationTypePerformance CorrelationType = "performance"
	CorrelationTypeCascade     CorrelationType = "cascade"
	CorrelationTypePredictive  CorrelationType = "predictive"
	CorrelationTypeStatistical CorrelationType = "statistical"
	CorrelationTypeGeneral     CorrelationType = "general"
)

// Aliases for backward compatibility
const (
	CorrelationTypeCausal   = CorrelationCausal
	CorrelationTypeTemporal = CorrelationTemporal
)

// EventReference links to an event with relationship context
type EventReference struct {
	EventID      EventID           `json:"event_id"`
	Role         string            `json:"role"`         // "trigger", "effect", "context"
	Relationship string            `json:"relationship"` // "causes", "follows", "coincides"
	Weight       float64           `json:"weight"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// ConfidenceScore provides detailed confidence breakdown
type ConfidenceScore struct {
	Overall     float64 `json:"overall"`
	Temporal    float64 `json:"temporal"`
	Causal      float64 `json:"causal"`
	Pattern     float64 `json:"pattern"`
	Statistical float64 `json:"statistical"`
}

// Float64 returns the overall confidence as a float64
func (c ConfidenceScore) Float64() float64 {
	return c.Overall
}

// GreaterThan checks if confidence is greater than a float64 value
func (c ConfidenceScore) GreaterThan(threshold float64) bool {
	return c.Overall > threshold
}

// GreaterThanOrEqual checks if confidence is greater than or equal to a float64 value
func (c ConfidenceScore) GreaterThanOrEqual(threshold float64) bool {
	return c.Overall >= threshold
}

// PatternSignature identifies the correlation pattern
type PatternSignature struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Fingerprint string            `json:"fingerprint"`
	Attributes  map[string]string `json:"attributes"`
}

// CorrelationMetadata contains processing information
type CorrelationMetadata struct {
	Algorithm   string            `json:"algorithm"`
	ProcessedAt time.Time         `json:"processed_at"`
	ProcessedBy string            `json:"processed_by"`
	Annotations map[string]string `json:"annotations,omitempty"`
	// Additional fields for correlation engine compatibility
	SchemaVersion string `json:"schema_version"`
}

// CorrelationContext provides context for correlations
type CorrelationContext struct {
	Host      string            `json:"host,omitempty"`
	Cluster   string            `json:"cluster,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
}

// Finding represents a high-level conclusion from correlations
type Finding struct {
	ID           FindingID       `json:"id"`
	Type         FindingType     `json:"type"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	Severity     Severity        `json:"severity"`
	Confidence   ConfidenceScore `json:"confidence"`
	Correlations []CorrelationID `json:"correlations"`
	Evidence     []Evidence      `json:"evidence"`
	Impact       Impact          `json:"impact"`
	Timestamp    time.Time       `json:"timestamp"`
	TTL          *time.Duration  `json:"ttl,omitempty"`
	// Additional fields for correlation engine compatibility
	Category string          `json:"category,omitempty"`
	Metadata FindingMetadata `json:"metadata,omitempty"`
}

// FindingMetadata contains metadata about findings
type FindingMetadata struct {
	Algorithm     string            `json:"algorithm,omitempty"`
	ProcessedBy   string            `json:"processed_by,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	SchemaVersion string            `json:"schema_version,omitempty"`
	ProcessedAt   time.Time         `json:"processed_at,omitempty"`
}

// FindingType categorizes findings
type FindingType string

const (
	FindingMemoryLeak     FindingType = "memory_leak"
	FindingCascadeFailure FindingType = "cascade_failure"
	FindingOOMPrediction  FindingType = "oom_prediction"
	FindingNetworkIssue   FindingType = "network_issue"
	FindingAnomalous      FindingType = "anomalous_behavior"
)

// Evidence supports a finding with specific data
type Evidence struct {
	Type        string                 `json:"type"`
	Source      SourceType             `json:"source"`
	Description string                 `json:"description"`
	Data        interface{}            `json:"data"` // Specific evidence data
	Timestamp   time.Time              `json:"timestamp"`
	Weight      float64                `json:"weight"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Impact describes the business/operational impact
type Impact struct {
	Scope        []string      `json:"scope"`
	Affected     []ResourceRef `json:"affected"`
	Risk         string        `json:"risk"`
	Consequences []string      `json:"consequences"`
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// NewEvent creates a new event with proper defaults
func NewEvent(eventType EventType, source SourceType, payload EventPayload) *Event {
	return &Event{
		ID:        EventID(generateID()),
		Type:      eventType,
		Source:    source,
		Timestamp: time.Now(),
		Payload:   payload,
		Context:   EventContext{},
		Metadata: EventMetadata{
			SchemaVersion: "v1",
			ProcessedAt:   time.Now(),
		},
		Confidence: 1.0,
	}
}

// Helper function to generate IDs (implement based on your needs)
func generateID() string {
	// Implementation depends on your ID generation strategy
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// =============================================================================
// ADDITIONAL EVENT PAYLOAD IMPLEMENTATIONS
// =============================================================================

// MemoryEventPayload represents memory-related event data
type MemoryEventPayload struct {
	Usage     float64 `json:"usage"`     // Percentage (0-100)
	Available uint64  `json:"available"` // Bytes available
	Total     uint64  `json:"total"`     // Total bytes
}

func (p MemoryEventPayload) PayloadType() string { return "memory" }
func (p MemoryEventPayload) Validate() error     { return nil }

// NetworkEventPayload represents network-related event data
type NetworkEventPayload struct {
	Protocol          string `json:"protocol"`
	SourceIP          string `json:"source_ip"`
	DestinationIP     string `json:"destination_ip"`
	SourcePort        int    `json:"source_port"`
	DestinationPort   int    `json:"destination_port"`
	BytesSent         uint64 `json:"bytes_sent"`
	BytesReceived     uint64 `json:"bytes_received"`
	PacketsDropped    uint64 `json:"packets_dropped"`
	ConnectionsFailed uint64 `json:"connections_failed"`
	Errors            uint64 `json:"errors"`
}

func (p NetworkEventPayload) PayloadType() string { return "network" }
func (p NetworkEventPayload) Validate() error     { return nil }

// =============================================================================
// HELPER FUNCTIONS FOR COMPATIBILITY
// =============================================================================

// EventIDsToEventReferences converts event IDs to event references
func EventIDsToEventReferences(eventIDs []EventID) []EventReference {
	refs := make([]EventReference, len(eventIDs))
	for i, id := range eventIDs {
		refs[i] = EventReference{
			EventID:      id,
			Role:         "participant",
			Relationship: "related",
			Weight:       1.0,
		}
	}
	return refs
}

// FloatToConfidenceScore converts a float64 to a ConfidenceScore
func FloatToConfidenceScore(confidence float64) ConfidenceScore {
	return ConfidenceScore{
		Overall:     confidence,
		Temporal:    confidence,
		Causal:      confidence,
		Pattern:     confidence,
		Statistical: confidence,
	}
}

// EventContextToCorrelationContext converts EventContext to CorrelationContext
func EventContextToCorrelationContext(ctx EventContext) CorrelationContext {
	labels := make(map[string]string)
	for k, v := range ctx.Labels {
		labels[k] = v
	}

	tags := make([]string, len(ctx.Tags))
	copy(tags, ctx.Tags)

	return CorrelationContext{
		Host:      ctx.Host,
		Cluster:   ctx.Cluster,
		Namespace: ctx.Namespace,
		Labels:    labels,
		Tags:      tags,
	}
}

// =============================================================================
// CORRELATION COMPATIBILITY LAYER
// =============================================================================

// Entity maps to ResourceRef for correlation system compatibility
type Entity = ResourceRef

// Result maps to Finding for correlation system compatibility
type Result = Finding

// Filter maps to QueryCriteria for correlation system compatibility
type Filter = QueryCriteria

// Stats maps to SystemMetrics for correlation system compatibility
type Stats = SystemMetrics

// Category represents event categories for correlation
type Category string

const (
	CategoryReliability Category = "reliability"
	CategoryPerformance Category = "performance"
	CategorySecurity    Category = "security"
	CategoryResource    Category = "resource"
)

// Rule interface for correlation rules
type Rule interface {
	ID() string
	Name() string
	Evaluate(ctx context.Context, events []Event) (*Finding, error)
}

// =============================================================================
// MONSTER COMPATIBILITY TYPES
// =============================================================================

// Insight represents a correlation insight (same as Finding but different name)
type Insight = Finding

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

// CausalityContext represents causal relationship context
type CausalityContext struct {
    RootCause     *Event                 `json:"root_cause"`
    CausalChain   []*Event              `json:"causal_chain"`
    Confidence    float64               `json:"confidence"`
    TimeWindow    TimeWindow            `json:"time_window"`
    Metadata      map[string]interface{} `json:"metadata"`
}

// BehavioralContext represents behavioral analysis context
type BehavioralContext struct {
    EntityType      string                 `json:"entity_type"`
    BaselineBehavior map[string]interface{} `json:"baseline_behavior"`
    CurrentBehavior  map[string]interface{} `json:"current_behavior"`
    Anomalies       []string               `json:"anomalies"`
    Confidence      float64                `json:"confidence"`
    TimeWindow      TimeWindow             `json:"time_window"`
}

// Event extension for monster compatibility
type EventExtension struct {
    Anomaly bool `json:"anomaly,omitempty"`
}
