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

	// Enrichment fields
	Attributes map[string]interface{} `json:"attributes,omitempty"`

	// Impact & Correlation (enhanced)
	Impact           *ImpactContext      `json:"impact,omitempty"`
	Correlation      *CorrelationContext `json:"correlation,omitempty"`
	CorrelationHints []string            `json:"correlation_hints,omitempty"`

	// K8s context (restored - needed for correlation)
	K8sContext *K8sContext `json:"k8s_context,omitempty"`

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

// SemanticContext describes what this event means
type SemanticContext struct {
	Intent     string   `json:"intent"`     // "user-login", "cache-miss", "oom-kill"
	Category   string   `json:"category"`   // "security", "performance", "availability"
	Tags       []string `json:"tags"`       // ["database", "critical-path", "customer-facing"]
	Narrative  string   `json:"narrative"`  // Human-readable description
	Confidence float64  `json:"confidence"` // How sure we are about the semantic meaning
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

// KernelData for eBPF/kernel events
type KernelData struct {
	Syscall    string            `json:"syscall,omitempty"`
	PID        uint32            `json:"pid,omitempty"`
	TID        uint32            `json:"tid,omitempty"`
	UID        uint32            `json:"uid,omitempty"`
	GID        uint32            `json:"gid,omitempty"`
	Comm       string            `json:"comm,omitempty"` // Process name
	ReturnCode int32             `json:"return_code,omitempty"`
	Args       map[string]string `json:"args,omitempty"` // Syscall arguments
	StackTrace []string          `json:"stack_trace,omitempty"`
}

// NetworkData for network events
type NetworkData struct {
	Protocol      string            `json:"protocol,omitempty"` // TCP, UDP, HTTP, gRPC
	SourceIP      string            `json:"source_ip,omitempty"`
	SourcePort    uint16            `json:"source_port,omitempty"`
	DestIP        string            `json:"dest_ip,omitempty"`
	DestPort      uint16            `json:"dest_port,omitempty"`
	Direction     string            `json:"direction,omitempty"` // ingress, egress
	BytesSent     uint64            `json:"bytes_sent,omitempty"`
	BytesRecv     uint64            `json:"bytes_recv,omitempty"`
	Latency       int64             `json:"latency_ns,omitempty"`
	StatusCode    int               `json:"status_code,omitempty"` // HTTP/gRPC status
	Method        string            `json:"method,omitempty"`      // HTTP/gRPC method
	Path          string            `json:"path,omitempty"`        // HTTP/gRPC path
	Headers       map[string]string `json:"headers,omitempty"`
	InterfaceName string            `json:"interface_name,omitempty"` // Network interface
}

// ApplicationCustomData represents custom application data
type ApplicationCustomData struct {
	// Request/Response data
	HTTPMethod     string            `json:"http_method,omitempty"`
	HTTPStatusCode int               `json:"http_status_code,omitempty"`
	HTTPPath       string            `json:"http_path,omitempty"`
	HTTPHeaders    map[string]string `json:"http_headers,omitempty"`
	RequestSize    int64             `json:"request_size,omitempty"`
	ResponseSize   int64             `json:"response_size,omitempty"`

	// Business context
	BusinessUnit string `json:"business_unit,omitempty"`
	FeatureFlag  string `json:"feature_flag,omitempty"`
	Experiment   string `json:"experiment,omitempty"`
	Cohort       string `json:"cohort,omitempty"`

	// Performance data
	DatabaseQueries int `json:"database_queries,omitempty"`
	CacheHits       int `json:"cache_hits,omitempty"`
	CacheMisses     int `json:"cache_misses,omitempty"`
	ExternalCalls   int `json:"external_calls,omitempty"`

	// Error context
	ErrorCategory  string `json:"error_category,omitempty"`
	ErrorRetryable bool   `json:"error_retryable,omitempty"`
	ErrorRecovery  string `json:"error_recovery,omitempty"`

	// Additional fields
	Tags    []string           `json:"tags,omitempty"`
	Labels  map[string]string  `json:"labels,omitempty"`
	Metrics map[string]float64 `json:"metrics,omitempty"`

	// Flexible data for specific use cases
	Payload interface{} `json:"payload,omitempty"`
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
	Custom     *ApplicationCustomData `json:"custom,omitempty"`
}

// KubernetesData for Kubernetes events
type KubernetesData struct {
	EventType       string            `json:"event_type,omitempty"`  // Normal, Warning
	Reason          string            `json:"reason,omitempty"`      // BackOff, Killing, etc.
	Object          string            `json:"object,omitempty"`      // pod/foo, deployment/bar
	ObjectKind      string            `json:"object_kind,omitempty"` // Pod, Service, etc.
	Message         string            `json:"message,omitempty"`
	Action          string            `json:"action,omitempty"` // ADDED, MODIFIED, DELETED
	APIVersion      string            `json:"api_version,omitempty"`
	ResourceVersion string            `json:"resource_version,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
	ClusterName     string            `json:"cluster_name,omitempty"`
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
	Severity             string   `json:"severity"`              // critical, high, medium, low
	InfrastructureImpact float64  `json:"infrastructure_impact"` // 0.0-1.0
	AffectedServices     []string `json:"affected_services"`
	AffectedComponents   int      `json:"affected_components,omitempty"`
	SLOImpact            bool     `json:"slo_impact"`
	SystemCritical       bool     `json:"system_critical"`
	CascadeRisk          bool     `json:"cascade_risk"`
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

func (b *UnifiedEventBuilder) WithImpact(severity string, infrastructureImpact float64) *UnifiedEventBuilder {
	b.event.Impact = &ImpactContext{
		Severity:             severity,
		InfrastructureImpact: infrastructureImpact,
	}
	return b
}

func (b *UnifiedEventBuilder) Build() *UnifiedEvent {
	return b.event
}

// CausalityContext represents causality information for events (minimal version)
type CausalityContext struct {
	RootCause   string   `json:"root_cause,omitempty"`
	CausalChain []string `json:"causal_chain,omitempty"`
	Confidence  float64  `json:"confidence,omitempty"`
}
