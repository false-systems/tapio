package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// GenerateEventID generates a unique event ID
func GenerateEventID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID
		return time.Now().Format("20060102150405.999999999")
	}
	return hex.EncodeToString(bytes)
}

// UnifiedEvent represents any observability signal from any layer
// This is the primary event type for the Tapio platform
// Fields can be nil/empty depending on the event source
type UnifiedEvent struct {
	// Core Identity
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Type      EventType `json:"type"`
	Source    string    `json:"source"` // collector/agent that generated this

	// Classification (merged from Event type)
	Category   string        `json:"category,omitempty"`
	Severity   EventSeverity `json:"severity,omitempty"`
	Confidence float64       `json:"confidence,omitempty"`

	// Message and Tags (merged from Event type)
	Message string   `json:"message,omitempty"`
	Tags    []string `json:"tags,omitempty"`

	// OTEL Trace Context (can be empty)
	TraceContext *TraceContext `json:"trace_context,omitempty"`

	// Semantic Context (enhanced)
	Semantic *SemanticContext `json:"semantic,omitempty"`

	// Entity Context (what this event is about)
	Entity *EntityContext `json:"entity,omitempty"`

	// Layer-Specific Data (only relevant fields populated)
	Kernel      *KernelData      `json:"kernel,omitempty"`      // eBPF events
	Network     *NetworkData     `json:"network,omitempty"`     // Network events
	Application *ApplicationData `json:"application,omitempty"` // App logs/errors
	Kubernetes  *KubernetesData  `json:"kubernetes,omitempty"`  // K8s events
	Metrics     *MetricsData     `json:"metrics,omitempty"`     // Time-series data

	// Enrichment fields (merged from Event type)
	Attributes map[string]interface{} `json:"attributes,omitempty"`
	AiFeatures map[string]float32     `json:"ai_features,omitempty"`

	// Analysis contexts (merged from OpinionatedEvent)
	Anomaly    *AnomalyInfo       `json:"anomaly,omitempty"`
	Behavioral *BehavioralContext `json:"behavioral,omitempty"`
	Temporal   *TemporalContext   `json:"temporal,omitempty"`
	State      *StateInfo         `json:"state,omitempty"`
	Causality  *CausalityContext  `json:"causality,omitempty"`

	// Impact & Correlation (enhanced)
	Impact           *ImpactContext      `json:"impact,omitempty"`
	Correlation      *CorrelationContext `json:"correlation,omitempty"`
	CorrelationHints []string            `json:"correlation_hints,omitempty"`

	// ENHANCED: Rich K8s context and analysis
	K8sContext         *K8sContext         `json:"k8s_context,omitempty"`
	ResourceContext    *ResourceContext    `json:"resource_context,omitempty"`
	OperationalContext *OperationalContext `json:"operational_context,omitempty"`
	
	// ENHANCED: Analysis results
	Correlations []CorrelationRef `json:"correlations,omitempty"`
	Patterns     []PatternMatch   `json:"patterns,omitempty"`
	Anomalies    []AnomalyRef      `json:"anomalies,omitempty"`

	// Collector metadata
	CollectorMetadata *CollectorMetadata `json:"collector_metadata,omitempty"`

	// Original raw data (for debugging/replay)
	RawData []byte `json:"raw_data,omitempty"`
}

// TraceContext carries OTEL trace propagation
type TraceContext struct {
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	TraceState   string            `json:"trace_state,omitempty"`
	Baggage      map[string]string `json:"baggage,omitempty"`
	Sampled      bool              `json:"sampled"`
}

// SemanticContext describes what this event means (enhanced)
type SemanticContext struct {
	Intent           string             `json:"intent"`               // "user-login", "cache-miss", "oom-kill"
	Category         string             `json:"category"`             // "security", "performance", "availability"
	Tags             []string           `json:"tags"`                 // ["database", "critical-path", "customer-facing"]
	Narrative        string             `json:"narrative"`            // Human-readable description
	Confidence       float64            `json:"confidence"`           // How sure we are about the semantic meaning
	Domain           string             `json:"domain,omitempty"`     // Business domain
	Concepts         []string           `json:"concepts,omitempty"`   // Related concepts
	Embedding        []float32          `json:"embedding,omitempty"`  // Vector embedding for similarity
	EventType        string             `json:"event_type,omitempty"` // Semantic classification
	IntentConfidence float32            `json:"intent_confidence,omitempty"`
	SemanticFeatures map[string]float32 `json:"semantic_features,omitempty"`
	OntologyTags     []string           `json:"ontology_tags,omitempty"`
	Description      string             `json:"description,omitempty"`
}

// EntityContext identifies what entity this event relates to
type EntityContext struct {
	Type       string            `json:"type"` // "pod", "service", "node", "user", "database"
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace,omitempty"`
	UID        string            `json:"uid,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// KernelData for eBPF/kernel events (enhanced with BPF metadata)
type KernelData struct {
	Syscall         string            `json:"syscall,omitempty"`
	PID             uint32            `json:"pid,omitempty"`
	TID             uint32            `json:"tid,omitempty"`
	UID             uint32            `json:"uid,omitempty"`
	GID             uint32            `json:"gid,omitempty"`
	Comm            string            `json:"comm,omitempty"` // Process name
	ReturnCode      int32             `json:"return_code,omitempty"`
	Args            map[string]string `json:"args,omitempty"` // Syscall arguments
	StackTrace      []string          `json:"stack_trace,omitempty"`
	CPUCore         int               `json:"cpu_core,omitempty"`
	BPFProgram      string            `json:"bpf_program,omitempty"`
	BPFMapStats     map[string]int    `json:"bpf_map_stats,omitempty"`
	KprobeDetails   *KprobeDetails    `json:"kprobe_details,omitempty"`
	SecurityContext *SecurityContext  `json:"security_context,omitempty"`
}

// NetworkData for network events (enhanced with CNI/network policy context)
type NetworkData struct {
	Protocol       string             `json:"protocol,omitempty"` // TCP, UDP, HTTP, gRPC
	SourceIP       string             `json:"source_ip,omitempty"`
	SourcePort     uint16             `json:"source_port,omitempty"`
	DestIP         string             `json:"dest_ip,omitempty"`
	DestPort       uint16             `json:"dest_port,omitempty"`
	Direction      string             `json:"direction,omitempty"` // ingress, egress
	BytesSent      uint64             `json:"bytes_sent,omitempty"`
	BytesRecv      uint64             `json:"bytes_recv,omitempty"`
	Latency        int64              `json:"latency_ns,omitempty"`
	StatusCode     int                `json:"status_code,omitempty"` // HTTP/gRPC status
	Method         string             `json:"method,omitempty"`      // HTTP/gRPC method
	Path           string             `json:"path,omitempty"`        // HTTP/gRPC path
	Headers        map[string]string  `json:"headers,omitempty"`
	NetworkPolicy  *NetworkPolicyInfo `json:"network_policy,omitempty"`
	IPTablesRules  []IPTablesRule     `json:"iptables_rules,omitempty"`
	ContainerID    string             `json:"container_id,omitempty"`
	InterfaceName  string             `json:"interface_name,omitempty"`
	VirtualNetwork string             `json:"virtual_network,omitempty"`
}

// ApplicationData for application-level events
type ApplicationData struct {
	Level      string                 `json:"level,omitempty"` // error, warn, info, debug
	Message    string                 `json:"message,omitempty"`
	Logger     string                 `json:"logger,omitempty"`
	ErrorType  string                 `json:"error_type,omitempty"`
	StackTrace string                 `json:"stack_trace,omitempty"`
	UserID     string                 `json:"user_id,omitempty"`
	SessionID  string                 `json:"session_id,omitempty"`
	RequestID  string                 `json:"request_id,omitempty"`
	Custom     map[string]interface{} `json:"custom,omitempty"`
}

// KubernetesData for Kubernetes events (enhanced with CRD and admission webhook support)
type KubernetesData struct {
	EventType        string              `json:"event_type,omitempty"`  // Normal, Warning
	Reason           string              `json:"reason,omitempty"`      // BackOff, Killing, etc.
	Object           string              `json:"object,omitempty"`      // pod/foo, deployment/bar
	ObjectKind       string              `json:"object_kind,omitempty"` // Pod, Service, etc.
	Message          string              `json:"message,omitempty"`
	Action           string              `json:"action,omitempty"` // ADDED, MODIFIED, DELETED
	APIVersion       string              `json:"api_version,omitempty"`
	ResourceVersion  string              `json:"resource_version,omitempty"`
	Labels           map[string]string   `json:"labels,omitempty"`
	Annotations      map[string]string   `json:"annotations,omitempty"`
	ClusterName      string              `json:"cluster_name,omitempty"`
	CustomResource   *CustomResourceInfo `json:"custom_resource,omitempty"`
	AdmissionWebhook *WebhookInfo        `json:"admission_webhook,omitempty"`
}

// MetricsData for time-series metrics
type MetricsData struct {
	MetricName  string            `json:"metric_name,omitempty"`
	Value       float64           `json:"value"`
	Unit        string            `json:"unit,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Aggregation string            `json:"aggregation,omitempty"` // sum, avg, max, min, p99
	Period      int64             `json:"period_ms,omitempty"`
}

// ImpactContext describes the impact of this event
type ImpactContext struct {
	Severity         string   `json:"severity"`        // critical, high, medium, low
	BusinessImpact   float64  `json:"business_impact"` // 0.0-1.0
	AffectedServices []string `json:"affected_services"`
	AffectedUsers    int      `json:"affected_users,omitempty"`
	SLOImpact        bool     `json:"slo_impact"`
	CustomerFacing   bool     `json:"customer_facing"`
	RevenueImpacting bool     `json:"revenue_impacting"`
}

// CorrelationContext helps group related events
type CorrelationContext struct {
	CorrelationID string   `json:"correlation_id,omitempty"`
	GroupID       string   `json:"group_id,omitempty"`
	ParentEventID string   `json:"parent_event_id,omitempty"`
	CausalChain   []string `json:"causal_chain,omitempty"` // Event IDs in causal order
	RelatedEvents []string `json:"related_events,omitempty"`
	Pattern       string   `json:"pattern,omitempty"` // Detected pattern name
	Stage         string   `json:"stage,omitempty"`   // Which stage in a sequence
}

// Helper methods

// HasTraceContext returns true if event has OTEL trace context
func (e *UnifiedEvent) HasTraceContext() bool {
	return e.TraceContext != nil && e.TraceContext.TraceID != ""
}

// GetSeverity returns the event severity, defaulting to "info"
func (e *UnifiedEvent) GetSeverity() string {
	// Check the direct severity field first
	if e.Severity != "" {
		return string(e.Severity)
	}
	// Fall back to impact severity
	if e.Impact != nil && e.Impact.Severity != "" {
		return e.Impact.Severity
	}
	// Check application level
	if e.Application != nil && e.Application.Level != "" {
		return e.Application.Level
	}
	// Check Kubernetes event type
	if e.Kubernetes != nil && e.Kubernetes.EventType == "Warning" {
		return "warning"
	}
	return "info"
}

// GetEntityID returns a unique identifier for the entity
func (e *UnifiedEvent) GetEntityID() string {
	if e.Entity == nil {
		return ""
	}
	if e.Entity.UID != "" {
		return e.Entity.UID
	}
	if e.Entity.Namespace != "" {
		return e.Entity.Namespace + "/" + e.Entity.Name
	}
	return e.Entity.Name
}

// IsKernelEvent returns true if this is a kernel/eBPF event
func (e *UnifiedEvent) IsKernelEvent() bool {
	return e.Kernel != nil
}

// IsNetworkEvent returns true if this is a network event
func (e *UnifiedEvent) IsNetworkEvent() bool {
	return e.Network != nil
}

// IsApplicationEvent returns true if this is an application event
func (e *UnifiedEvent) IsApplicationEvent() bool {
	return e.Application != nil
}

// IsKubernetesEvent returns true if this is a Kubernetes event
func (e *UnifiedEvent) IsKubernetesEvent() bool {
	return e.Kubernetes != nil
}

// IsMetricEvent returns true if this is a metrics event
func (e *UnifiedEvent) IsMetricEvent() bool {
	return e.Metrics != nil
}

// GetSemanticIntent returns the semantic intent or empty string
func (e *UnifiedEvent) GetSemanticIntent() string {
	if e.Semantic != nil {
		return e.Semantic.Intent
	}
	return ""
}

// Builder pattern for creating events

type UnifiedEventBuilder struct {
	event *UnifiedEvent
}

func NewUnifiedEvent() *UnifiedEventBuilder {
	return &UnifiedEventBuilder{
		event: &UnifiedEvent{
			ID:        GenerateEventID(),
			Timestamp: time.Now(),
		},
	}
}

func (b *UnifiedEventBuilder) WithType(t EventType) *UnifiedEventBuilder {
	b.event.Type = t
	return b
}

func (b *UnifiedEventBuilder) WithSource(source string) *UnifiedEventBuilder {
	b.event.Source = source
	return b
}

func (b *UnifiedEventBuilder) WithTraceContext(traceID, spanID string) *UnifiedEventBuilder {
	b.event.TraceContext = &TraceContext{
		TraceID: traceID,
		SpanID:  spanID,
		Sampled: true,
	}
	return b
}

func (b *UnifiedEventBuilder) WithSemantic(intent, category string, tags ...string) *UnifiedEventBuilder {
	b.event.Semantic = &SemanticContext{
		Intent:     intent,
		Category:   category,
		Tags:       tags,
		Confidence: 1.0,
	}
	return b
}

func (b *UnifiedEventBuilder) WithEntity(entityType, name, namespace string) *UnifiedEventBuilder {
	b.event.Entity = &EntityContext{
		Type:      entityType,
		Name:      name,
		Namespace: namespace,
	}
	return b
}

func (b *UnifiedEventBuilder) WithKernelData(syscall string, pid uint32) *UnifiedEventBuilder {
	b.event.Kernel = &KernelData{
		Syscall: syscall,
		PID:     pid,
	}
	return b
}

func (b *UnifiedEventBuilder) WithNetworkData(protocol, srcIP string, srcPort uint16, dstIP string, dstPort uint16) *UnifiedEventBuilder {
	b.event.Network = &NetworkData{
		Protocol:   protocol,
		SourceIP:   srcIP,
		SourcePort: srcPort,
		DestIP:     dstIP,
		DestPort:   dstPort,
	}
	return b
}

func (b *UnifiedEventBuilder) WithApplicationData(level, message string) *UnifiedEventBuilder {
	b.event.Application = &ApplicationData{
		Level:   level,
		Message: message,
	}
	return b
}

func (b *UnifiedEventBuilder) WithImpact(severity string, businessImpact float64) *UnifiedEventBuilder {
	b.event.Impact = &ImpactContext{
		Severity:       severity,
		BusinessImpact: businessImpact,
	}
	return b
}

func (b *UnifiedEventBuilder) Build() *UnifiedEvent {
	return b.event
}

// =============================================================================
// ANALYSIS CONTEXT TYPES (only the ones not already in types.go)
// =============================================================================

// BehavioralContext provides behavioral analysis context
type BehavioralContext struct {
	Pattern    string                 `json:"pattern"`
	Frequency  float64                `json:"frequency"`
	Confidence float64                `json:"confidence"`
	TimeWindow TimeWindow             `json:"time_window"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// CausalityContext represents causality information for events
type CausalityContext struct {
	RootCause   string                 `json:"root_cause"`
	CausalChain []string               `json:"causal_chain"`
	Confidence  float64                `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// AnomalyInfo provides detailed anomaly information for events
type AnomalyInfo struct {
	Score              float64             `json:"score"`
	Type               string              `json:"type"`
	Description        string              `json:"description"`
	Confidence         float64             `json:"confidence"`
	AnomalyScore       float32             `json:"anomaly_score"`
	Dimensions         *AnomalyDimensions  `json:"dimensions"`
	BaselineComparison *BaselineComparison `json:"baseline_comparison"`
}

// BaselineComparison provides comparison with baseline behavior
type BaselineComparison struct {
	Deviation    float32 `json:"deviation"`
	Significance float32 `json:"significance"`
	Confidence   float32 `json:"confidence"`
	Percentile   float32 `json:"percentile"`
	ZScore       float32 `json:"z_score"`
}

// TemporalContext provides temporal analysis context
type TemporalContext struct {
	Period      time.Duration      `json:"period"`
	Frequency   float64            `json:"frequency"`
	Patterns    []TemporalPattern  `json:"patterns"`
	Seasonality map[string]float64 `json:"seasonality"`
	Duration    time.Duration      `json:"duration"`
	Periodicity float64            `json:"periodicity"`
}

// TemporalPattern represents a time-based pattern in events
type TemporalPattern struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Pattern     []PatternStep `json:"pattern"`
	Window      time.Duration `json:"window"`
	Confidence  float64       `json:"confidence"`
}

// PatternStep represents a step in a temporal pattern
type PatternStep struct {
	EventType string        `json:"event_type"`
	Condition string        `json:"condition"`
	Delay     time.Duration `json:"delay"`
	Optional  bool          `json:"optional"`
}

// StateInfo provides state tracking information
type StateInfo struct {
	Current    string            `json:"current"`
	Previous   string            `json:"previous"`
	Transition string            `json:"transition"`
	Duration   time.Duration     `json:"duration"`
	Metadata   map[string]string `json:"metadata"`
	TimeSeries *TimeSeriesData   `json:"time_series,omitempty"`
}

// TimeSeriesData provides time series context
type TimeSeriesData struct {
	Values     []float64     `json:"values"`
	Timestamps []time.Time   `json:"timestamps"`
	Window     time.Duration `json:"window"`
	Trend      string        `json:"trend"`
}
