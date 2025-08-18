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
	EventTypeMetric     EventType = "metric"
)

// SourceType represents the source of an event
type SourceType string

// Alias for backward compatibility
type Source = SourceType

const (
	SourceEBPF    SourceType = "ebpf"
	SourceK8s     SourceType = "kubernetes"
	SourceSystemd SourceType = "systemd"
	SourceCNI     SourceType = "cni"
	SourceCustom  SourceType = "custom"
)

// Core domain types - only the ones NOT already in interfaces.go

// EventData represents structured event data
type EventData struct {
	// Core metrics
	Metrics    map[string]float64 `json:"metrics,omitempty"`
	Counters   map[string]int64   `json:"counters,omitempty"`
	Dimensions map[string]string  `json:"dimensions,omitempty"`

	// Structured data
	Resource    *ResourceInfo    `json:"resource,omitempty"`
	Process     *ProcessInfo     `json:"process,omitempty"`
	Network     *NetworkInfo     `json:"network,omitempty"`
	Error       *ErrorInfo       `json:"error,omitempty"`
	Performance *PerformanceInfo `json:"performance,omitempty"`

	// Flexible typed data
	CustomData interface{} `json:"custom_data,omitempty"`
}

// EventAttributes represents event attributes
type EventAttributes struct {
	Environment string            `json:"environment,omitempty"`
	Version     string            `json:"version,omitempty"`
	Region      string            `json:"region,omitempty"`
	Datacenter  string            `json:"datacenter,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
}

// AiFeatures represents AI/ML derived features
type AiFeatures struct {
	AnomalyScore     float64            `json:"anomaly_score,omitempty"`
	PredictionScore  float64            `json:"prediction_score,omitempty"`
	Classification   []string           `json:"classification,omitempty"`
	FeatureVector    []float64          `json:"feature_vector,omitempty"`
	Embedding        []float64          `json:"embedding,omitempty"`
	ModelVersion     string             `json:"model_version,omitempty"`
	ModelMetadata    map[string]string  `json:"model_metadata,omitempty"`
	ConfidenceScores map[string]float64 `json:"confidence_scores,omitempty"`
}

// SemanticData represents semantic event information
type SemanticData struct {
	Intent         string            `json:"intent,omitempty"`
	Category       string            `json:"category,omitempty"`
	Subcategory    string            `json:"subcategory,omitempty"`
	BusinessImpact string            `json:"business_impact,omitempty"`
	SeverityLevel  string            `json:"severity_level,omitempty"`
	Keywords       []string          `json:"keywords,omitempty"`
	Concepts       []string          `json:"concepts,omitempty"`
	Entities       []string          `json:"entities,omitempty"`
	Relationships  map[string]string `json:"relationships,omitempty"`
}

// AnomalyData represents anomaly detection results
type AnomalyData struct {
	Score        float64            `json:"score"`
	Type         string             `json:"type,omitempty"`
	Description  string             `json:"description,omitempty"`
	Threshold    float64            `json:"threshold,omitempty"`
	Baseline     float64            `json:"baseline,omitempty"`
	DeviationStd float64            `json:"deviation_std,omitempty"`
	DetectorID   string             `json:"detector_id,omitempty"`
	Features     map[string]float64 `json:"features,omitempty"`
	Timestamp    time.Time          `json:"timestamp"`
}

// BehavioralData represents behavioral analysis results
type BehavioralData struct {
	PatternID       string             `json:"pattern_id,omitempty"`
	PatternName     string             `json:"pattern_name,omitempty"`
	Frequency       float64            `json:"frequency,omitempty"`
	Periodicity     *time.Duration     `json:"periodicity,omitempty"`
	Trend           string             `json:"trend,omitempty"`
	SimilarityScore float64            `json:"similarity_score,omitempty"`
	Metrics         map[string]float64 `json:"metrics,omitempty"`
	Classification  []string           `json:"classification,omitempty"`
}

// ProcessInfo represents process information
type ProcessInfo struct {
	PID        int32     `json:"pid"`
	PPID       int32     `json:"ppid,omitempty"`
	Name       string    `json:"name"`
	Cmdline    string    `json:"cmdline,omitempty"`
	Executable string    `json:"executable,omitempty"`
	WorkingDir string    `json:"working_dir,omitempty"`
	Username   string    `json:"username,omitempty"`
	UID        int32     `json:"uid,omitempty"`
	GID        int32     `json:"gid,omitempty"`
	StartTime  time.Time `json:"start_time,omitempty"`
}

// NetworkInfo represents network information
type NetworkInfo struct {
	Protocol   string `json:"protocol,omitempty"`
	SourceAddr string `json:"source_addr,omitempty"`
	SourcePort int32  `json:"source_port,omitempty"`
	DestAddr   string `json:"dest_addr,omitempty"`
	DestPort   int32  `json:"dest_port,omitempty"`
	Direction  string `json:"direction,omitempty"`
	Interface  string `json:"interface,omitempty"`
}

// ErrorInfo represents error information
type ErrorInfo struct {
	Code       string `json:"code,omitempty"`
	Type       string `json:"type,omitempty"`
	Message    string `json:"message"`
	StackTrace string `json:"stack_trace,omitempty"`
	File       string `json:"file,omitempty"`
	Line       int32  `json:"line,omitempty"`
	Function   string `json:"function,omitempty"`
}

// PerformanceInfo represents performance metrics
type PerformanceInfo struct {
	LatencyMs   float64 `json:"latency_ms,omitempty"`
	Throughput  float64 `json:"throughput,omitempty"`
	CPUUsage    float64 `json:"cpu_usage,omitempty"`
	MemoryUsage int64   `json:"memory_usage,omitempty"`
	DiskIO      int64   `json:"disk_io,omitempty"`
	NetworkIO   int64   `json:"network_io,omitempty"`
}

// Event represents a comprehensive system event supporting multiple use cases
// DEPRECATED: Use ObservationEvent instead. This type will be removed in a future version.
type Event struct {
	// Core fields (backward compatible)
	ID        EventID    `json:"id"`
	Timestamp time.Time  `json:"timestamp"`
	Type      EventType  `json:"type"`
	Source    SourceType `json:"source"`
	Data      *EventData `json:"data,omitempty"`

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
	Attributes *EventAttributes  `json:"attributes,omitempty"`
	AiFeatures *AiFeatures       `json:"ai_features,omitempty"`
	Semantic   *SemanticData     `json:"semantic,omitempty"`
	Anomaly    *AnomalyData      `json:"anomaly,omitempty"`
	Behavioral *BehavioralData   `json:"behavioral,omitempty"`
	Causality  *CausalityContext `json:"causality,omitempty"`

	// Payload for specific event types
	Payload EventPayload `json:"payload,omitempty"`
}

// ContextMetadata represents structured context metadata
type ContextMetadata struct {
	// Resource metadata
	ResourceVersion string           `json:"resource_version,omitempty"`
	Generation      int64            `json:"generation,omitempty"`
	FinalizerList   []string         `json:"finalizer_list,omitempty"`
	OwnerReferences []OwnerReference `json:"owner_references,omitempty"`

	// Runtime metadata
	StartTime  time.Time     `json:"start_time,omitempty"`
	Uptime     time.Duration `json:"uptime,omitempty"`
	LastUpdate time.Time     `json:"last_update,omitempty"`

	// Correlation metadata
	CorrelationID string `json:"correlation_id,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	UserID        string `json:"user_id,omitempty"`

	// Environment metadata
	Environment string `json:"environment,omitempty"`
	Cluster     string `json:"cluster,omitempty"`
	Region      string `json:"region,omitempty"`
	Zone        string `json:"zone,omitempty"`

	// Additional structured data
	CustomFields map[string]string `json:"custom_fields,omitempty"`
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
	Labels   map[string]string `json:"labels,omitempty"`
	Metadata *ContextMetadata  `json:"metadata,omitempty"`

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

// SystemDetails represents system event details
type SystemDetails struct {
	// System identification
	Hostname      string        `json:"hostname,omitempty"`
	KernelVersion string        `json:"kernel_version,omitempty"`
	Architecture  string        `json:"architecture,omitempty"`
	Uptime        time.Duration `json:"uptime,omitempty"`

	// Resource details
	CPUCores    int32 `json:"cpu_cores,omitempty"`
	MemoryTotal int64 `json:"memory_total,omitempty"`
	DiskTotal   int64 `json:"disk_total,omitempty"`

	// Process details
	ProcessCount int32     `json:"process_count,omitempty"`
	LoadAverage  []float64 `json:"load_average,omitempty"`

	// Error details
	ErrorCode  string `json:"error_code,omitempty"`
	ExitStatus int32  `json:"exit_status,omitempty"`
	Signal     string `json:"signal,omitempty"`

	// Configuration details
	ConfigPath string            `json:"config_path,omitempty"`
	ConfigHash string            `json:"config_hash,omitempty"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

// SystemEventPayload for system-level events
type SystemEventPayload struct {
	Subsystem string         `json:"subsystem"`
	EventType string         `json:"event_type"`
	Message   string         `json:"message"`
	Details   *SystemDetails `json:"details,omitempty"`
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

// GenericEventPayload for general events with flexible data
// Data can be any type that implements json.Marshaler if needed
type GenericEventPayload struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
}

func (g GenericEventPayload) GetType() string { return g.Type }

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

// InsightData represents structured insight data
type InsightData struct {
	// Analysis results
	Score      float64            `json:"score,omitempty"`
	Metrics    map[string]float64 `json:"metrics,omitempty"`
	Dimensions []string           `json:"dimensions,omitempty"`
	Features   []string           `json:"features,omitempty"`

	// Correlation data
	RelatedEvents []string `json:"related_events,omitempty"`
	Causality     []string `json:"causality,omitempty"`
	Patterns      []string `json:"patterns,omitempty"`

	// Recommendations
	Actions         []string `json:"actions,omitempty"`
	Recommendations []string `json:"recommendations,omitempty"`

	// Evidence
	Evidence   []string `json:"evidence,omitempty"`
	Supporting []string `json:"supporting,omitempty"`
}

// InsightMetadata represents insight metadata
type InsightMetadata struct {
	// Analysis metadata
	AnalysisTime time.Duration `json:"analysis_time,omitempty"`
	DataSources  []string      `json:"data_sources,omitempty"`
	Algorithm    string        `json:"algorithm,omitempty"`
	ModelVersion string        `json:"model_version,omitempty"`

	// Quality metadata
	Accuracy  float64 `json:"accuracy,omitempty"`
	Precision float64 `json:"precision,omitempty"`
	Recall    float64 `json:"recall,omitempty"`

	// Context metadata
	TimeWindow TimeWindow `json:"time_window,omitempty"`
	Scope      []string   `json:"scope,omitempty"`
	Filters    []string   `json:"filters,omitempty"`

	// Processing metadata
	ProcessedAt time.Time         `json:"processed_at,omitempty"`
	ProcessedBy string            `json:"processed_by,omitempty"`
	Version     string            `json:"version,omitempty"`
	Parameters  map[string]string `json:"parameters,omitempty"`
}

// Insight represents an analytical insight
type Insight struct {
	ID          string           `json:"id"`
	Type        string           `json:"type"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Severity    SeverityLevel    `json:"severity"`
	Confidence  float64          `json:"confidence"`
	Source      string           `json:"source,omitempty"`
	Data        *InsightData     `json:"data,omitempty"`
	Metadata    *InsightMetadata `json:"metadata,omitempty"`
	Timestamp   time.Time        `json:"timestamp"`
}

// EvidenceData represents structured evidence data
type EvidenceData struct {
	// Metrics and measurements
	Metrics    map[string]float64 `json:"metrics,omitempty"`
	Counters   map[string]int64   `json:"counters,omitempty"`
	Thresholds map[string]float64 `json:"thresholds,omitempty"`

	// References and identifiers
	EventIDs     []string         `json:"event_ids,omitempty"`
	ResourceRefs []K8sResourceRef `json:"resource_refs,omitempty"`
	Correlations []string         `json:"correlations,omitempty"`

	// Analysis results
	Score          float64  `json:"score,omitempty"`
	Confidence     float64  `json:"confidence,omitempty"`
	Classification []string `json:"classification,omitempty"`

	// Contextual information
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Tags        []string          `json:"tags,omitempty"`

	// Supporting details
	StackTrace string   `json:"stack_trace,omitempty"`
	ErrorCode  string   `json:"error_code,omitempty"`
	LogEntries []string `json:"log_entries,omitempty"`
}

// Evidence represents supporting evidence for a finding
type Evidence struct {
	Type        string        `json:"type"`
	Source      string        `json:"source"`
	Description string        `json:"description"`
	Data        *EvidenceData `json:"data,omitempty"`
	Timestamp   time.Time     `json:"timestamp"`
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

// CausalityContext is defined in unified_event.go to avoid circular imports

// ProblemMetadata represents problem metadata
type ProblemMetadata struct {
	// Detection metadata
	DetectedAt    time.Time `json:"detected_at,omitempty"`
	DetectedBy    string    `json:"detected_by,omitempty"`
	DetectionRule string    `json:"detection_rule,omitempty"`
	RuleVersion   string    `json:"rule_version,omitempty"`

	// Impact metadata
	BusinessImpact string `json:"business_impact,omitempty"`
	UserImpact     string `json:"user_impact,omitempty"`
	SystemImpact   string `json:"system_impact,omitempty"`
	SLOImpact      bool   `json:"slo_impact,omitempty"`

	// Resolution metadata
	ResolutionTime time.Duration `json:"resolution_time,omitempty"`
	ResolvedBy     string        `json:"resolved_by,omitempty"`
	ResolvedAt     *time.Time    `json:"resolved_at,omitempty"`
	Resolution     string        `json:"resolution,omitempty"`

	// Correlation metadata
	RelatedProblems []string `json:"related_problems,omitempty"`
	RootCause       string   `json:"root_cause,omitempty"`
	CausedBy        []string `json:"caused_by,omitempty"`

	// Classification
	Category    string            `json:"category,omitempty"`
	Subcategory string            `json:"subcategory,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// Problem represents an issue detected in the system
type Problem struct {
	Resource    ResourceRef      `json:"resource"`
	Severity    SeverityLevel    `json:"severity"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Evidence    []Evidence       `json:"evidence,omitempty"`
	Metadata    *ProblemMetadata `json:"metadata,omitempty"`
}

// ResourceRef represents a reference to a Kubernetes resource
type ResourceRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// SystemEventData represents system event data
type SystemEventData struct {
	// Process information
	Process   *ProcessInfo `json:"process,omitempty"`
	ParentPID int32        `json:"parent_pid,omitempty"`
	ThreadID  int32        `json:"thread_id,omitempty"`

	// System call information
	Syscall     string   `json:"syscall,omitempty"`
	SyscallArgs []string `json:"syscall_args,omitempty"`
	ReturnValue int64    `json:"return_value,omitempty"`
	ErrorCode   int32    `json:"error_code,omitempty"`

	// File system information
	Filename string `json:"filename,omitempty"`
	FileMode string `json:"file_mode,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
	FileType string `json:"file_type,omitempty"`

	// Network information
	Network *NetworkInfo `json:"network,omitempty"`

	// Resource usage
	CPUTime     int64 `json:"cpu_time,omitempty"`
	MemoryUsage int64 `json:"memory_usage,omitempty"`
	DiskUsage   int64 `json:"disk_usage,omitempty"`

	// Security context
	UserID         int32    `json:"user_id,omitempty"`
	GroupID        int32    `json:"group_id,omitempty"`
	Capabilities   []string `json:"capabilities,omitempty"`
	SelinuxContext string   `json:"selinux_context,omitempty"`

	// Container information
	ContainerID string `json:"container_id,omitempty"`
	PodName     string `json:"pod_name,omitempty"`
	Namespace   string `json:"namespace,omitempty"`

	// Additional context
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// SystemEvent represents a system-level event from eBPF
type SystemEvent struct {
	ID        string           `json:"id"`
	Timestamp time.Time        `json:"timestamp"`
	Type      string           `json:"type"`
	PID       int              `json:"pid"`
	Data      *SystemEventData `json:"data,omitempty"`
}

// BehavioralContext is defined in unified_event.go to avoid circular imports

// ActionItemMetadata represents action item metadata
type ActionItemMetadata struct {
	// Execution metadata
	EstimatedTime       time.Duration `json:"estimated_time,omitempty"`
	RequiredPermissions []string      `json:"required_permissions,omitempty"`
	Prerequisites       []string      `json:"prerequisites,omitempty"`
	Risks               []string      `json:"risks,omitempty"`

	// Context metadata
	Context         string   `json:"context,omitempty"`
	Scope           []string `json:"scope,omitempty"`
	TargetResource  string   `json:"target_resource,omitempty"`
	TargetNamespace string   `json:"target_namespace,omitempty"`

	// Tracking metadata
	CreatedAt  time.Time  `json:"created_at,omitempty"`
	CreatedBy  string     `json:"created_by,omitempty"`
	AssignedTo string     `json:"assigned_to,omitempty"`
	DueDate    *time.Time `json:"due_date,omitempty"`

	// Status metadata
	Status     string     `json:"status,omitempty"`
	Progress   float64    `json:"progress,omitempty"`
	ExecutedAt *time.Time `json:"executed_at,omitempty"`
	ExecutedBy string     `json:"executed_by,omitempty"`

	// Results metadata
	Result       string `json:"result,omitempty"`
	Output       string `json:"output,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`

	// Classification
	Category string            `json:"category,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// ActionItem represents a recommended action
type ActionItem struct {
	ID          string              `json:"id"`
	Type        string              `json:"type"`
	Description string              `json:"description"`
	Priority    string              `json:"priority"`
	Command     string              `json:"command,omitempty"`
	Metadata    *ActionItemMetadata `json:"metadata,omitempty"`
}

// AnomalyDimensions represents dimensions of an anomaly
type AnomalyDimensions struct {
	Temporal   float64 `json:"temporal"`
	Spatial    float64 `json:"spatial"`
	Behavioral float64 `json:"behavioral"`
	Contextual float64 `json:"contextual"`
}

// ServiceProperties represents service properties
type ServiceProperties struct {
	// Service configuration
	ServiceType   string        `json:"service_type,omitempty"`
	ExecStart     string        `json:"exec_start,omitempty"`
	ExecStop      string        `json:"exec_stop,omitempty"`
	ExecReload    string        `json:"exec_reload,omitempty"`
	RestartPolicy string        `json:"restart_policy,omitempty"`
	TimeoutStart  time.Duration `json:"timeout_start,omitempty"`
	TimeoutStop   time.Duration `json:"timeout_stop,omitempty"`

	// Runtime properties
	PID           int32   `json:"pid,omitempty"`
	MainPID       int32   `json:"main_pid,omitempty"`
	ControlPID    int32   `json:"control_pid,omitempty"`
	MemoryCurrent int64   `json:"memory_current,omitempty"`
	CPUUsage      float64 `json:"cpu_usage,omitempty"`

	// Dependencies
	After     []string `json:"after,omitempty"`
	Before    []string `json:"before,omitempty"`
	Requires  []string `json:"requires,omitempty"`
	Wants     []string `json:"wants,omitempty"`
	Conflicts []string `json:"conflicts,omitempty"`

	// Environment
	Environment      map[string]string `json:"environment,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	User             string            `json:"user,omitempty"`
	Group            string            `json:"group,omitempty"`
}

// ServiceEventData represents service event data
type ServiceEventData struct {
	// State transition
	FromState      string        `json:"from_state,omitempty"`
	ToState        string        `json:"to_state,omitempty"`
	TransitionTime time.Duration `json:"transition_time,omitempty"`

	// Process information
	ProcessInfo *ProcessInfo `json:"process_info,omitempty"`
	ExitCode    int32        `json:"exit_code,omitempty"`
	Signal      string       `json:"signal,omitempty"`

	// Resource usage at event time
	CPUTime    int64 `json:"cpu_time,omitempty"`
	MemoryPeak int64 `json:"memory_peak,omitempty"`
	DiskIO     int64 `json:"disk_io,omitempty"`

	// Error information
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	StackTrace   string `json:"stack_trace,omitempty"`

	// Context
	TriggeredBy   string            `json:"triggered_by,omitempty"`
	CorrelationID string            `json:"correlation_id,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// ServiceEvent represents a systemd service event
type ServiceEvent struct {
	ServiceName string             `json:"service_name"`
	EventType   string             `json:"event_type"`
	Timestamp   time.Time          `json:"timestamp"`
	Message     string             `json:"message"`
	OldState    string             `json:"old_state,omitempty"`
	NewState    string             `json:"new_state,omitempty"`
	Reason      string             `json:"reason,omitempty"`
	Properties  *ServiceProperties `json:"properties,omitempty"`
	Data        *ServiceEventData  `json:"data,omitempty"`
}

// LogEventFields represents log event fields
type LogEventFields struct {
	// Standard systemd journal fields
	SystemdUnit     string `json:"systemd_unit,omitempty"`
	SystemdSlice    string `json:"systemd_slice,omitempty"`
	SystemdCGroup   string `json:"systemd_cgroup,omitempty"`
	SystemdUserUnit string `json:"systemd_user_unit,omitempty"`
	SystemdSession  string `json:"systemd_session,omitempty"`

	// Process fields
	ProcessComm    string `json:"process_comm,omitempty"`
	ProcessExec    string `json:"process_exec,omitempty"`
	ProcessCmdline string `json:"process_cmdline,omitempty"`

	// Boot and system fields
	BootID          string `json:"boot_id,omitempty"`
	MachineID       string `json:"machine_id,omitempty"`
	KernelDevice    string `json:"kernel_device,omitempty"`
	KernelSubsystem string `json:"kernel_subsystem,omitempty"`

	// Transport and source fields
	Transport       string `json:"transport,omitempty"`
	SourceRealtime  string `json:"source_realtime,omitempty"`
	SourceMonotonic string `json:"source_monotonic,omitempty"`

	// Error and code fields
	Errno    int32  `json:"errno,omitempty"`
	Code     string `json:"code,omitempty"`
	CodeFile string `json:"code_file,omitempty"`
	CodeLine int32  `json:"code_line,omitempty"`
	CodeFunc string `json:"code_func,omitempty"`

	// Additional structured fields
	CustomFields map[string]string `json:"custom_fields,omitempty"`
}

// LogEvent represents a journald log event
type LogEvent struct {
	Timestamp  time.Time       `json:"timestamp"`
	Unit       string          `json:"unit"`
	Message    string          `json:"message"`
	Priority   int             `json:"priority"`
	Hostname   string          `json:"hostname,omitempty"`
	Identifier string          `json:"identifier,omitempty"`
	PID        int             `json:"pid,omitempty"`
	UID        int             `json:"uid,omitempty"`
	Fields     *LogEventFields `json:"fields,omitempty"`
}

// PatternCondition represents a pattern matching condition
type PatternCondition struct {
	Type       string            `json:"type"` // "event_type", "field_match", "threshold", "sequence"
	Field      string            `json:"field,omitempty"`
	Operator   string            `json:"operator,omitempty"` // "equals", "contains", "greater_than", "less_than"
	Value      interface{}       `json:"value,omitempty"`
	Required   bool              `json:"required"`
	TimeWindow *time.Duration    `json:"time_window,omitempty"`
	Options    map[string]string `json:"options,omitempty"`
}

// PatternAction represents an action to take when pattern matches
type PatternAction struct {
	Type        string            `json:"type"` // "alert", "correlation", "enrichment", "webhook"
	Target      string            `json:"target,omitempty"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	Enabled     bool              `json:"enabled"`
	Priority    int               `json:"priority,omitempty"`
	RetryPolicy *RetryPolicy      `json:"retry_policy,omitempty"`
}

// RetryPolicy defines retry behavior for pattern actions
type RetryPolicy struct {
	MaxAttempts       int           `json:"max_attempts"`
	InitialDelay      time.Duration `json:"initial_delay"`
	MaxDelay          time.Duration `json:"max_delay"`
	BackoffMultiplier float64       `json:"backoff_multiplier"`
}

// PatternMetadata represents pattern metadata
type PatternMetadata struct {
	// Pattern lifecycle
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	CreatedBy string    `json:"created_by,omitempty"`
	Version   string    `json:"version,omitempty"`

	// Pattern performance
	MatchCount     int64         `json:"match_count,omitempty"`
	LastMatched    *time.Time    `json:"last_matched,omitempty"`
	SuccessRate    float64       `json:"success_rate,omitempty"`
	AvgProcessTime time.Duration `json:"avg_process_time,omitempty"`

	// Pattern classification
	Category       string   `json:"category,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	Severity       string   `json:"severity,omitempty"`
	BusinessImpact string   `json:"business_impact,omitempty"`

	// Pattern configuration
	Enabled      bool    `json:"enabled"`
	DebugMode    bool    `json:"debug_mode,omitempty"`
	SampleRate   float64 `json:"sample_rate,omitempty"`
	ThrottleRate float64 `json:"throttle_rate,omitempty"`

	// Documentation
	Documentation string   `json:"documentation,omitempty"`
	Examples      []string `json:"examples,omitempty"`
	Runbooks      []string `json:"runbooks,omitempty"`

	// Additional labels
	Labels map[string]string `json:"labels,omitempty"`
}

// Pattern represents a correlation pattern
type Pattern struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Type        string             `json:"type"`
	Description string             `json:"description"`
	Conditions  []PatternCondition `json:"conditions"`
	Actions     []PatternAction    `json:"actions"`
	Metadata    *PatternMetadata   `json:"metadata,omitempty"`
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

// CorrelationParameters represents correlation algorithm parameters
type CorrelationParameters struct {
	// Algorithm configuration
	TimeWindow          time.Duration `json:"time_window,omitempty"`
	SimilarityThreshold float64       `json:"similarity_threshold,omitempty"`
	ConfidenceThreshold float64       `json:"confidence_threshold,omitempty"`
	MaxDistance         float64       `json:"max_distance,omitempty"`

	// Feature weights
	TemporalWeight float64 `json:"temporal_weight,omitempty"`
	SpatialWeight  float64 `json:"spatial_weight,omitempty"`
	SemanticWeight float64 `json:"semantic_weight,omitempty"`
	CausalWeight   float64 `json:"causal_weight,omitempty"`

	// Processing options
	UseCache       bool    `json:"use_cache,omitempty"`
	Parallelize    bool    `json:"parallelize,omitempty"`
	MaxConcurrency int     `json:"max_concurrency,omitempty"`
	SampleRate     float64 `json:"sample_rate,omitempty"`

	// Filtering options
	MinEventCount   int      `json:"min_event_count,omitempty"`
	MaxEventCount   int      `json:"max_event_count,omitempty"`
	EventTypeFilter []string `json:"event_type_filter,omitempty"`
	SeverityFilter  []string `json:"severity_filter,omitempty"`

	// Additional parameters as key-value pairs for flexibility
	CustomParams map[string]string `json:"custom_params,omitempty"`
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
	Parameters    *CorrelationParameters
}

// Filter provides filtering criteria for event queries
type Filter struct {
	Since      time.Time `json:"since,omitempty"`
	Until      time.Time `json:"until,omitempty"`
	Type       string    `json:"type,omitempty"`
	Severity   string    `json:"severity,omitempty"`
	Namespace  string    `json:"namespace,omitempty"`
	EntityName string    `json:"entity_name,omitempty"`
	EntityType string    `json:"entity_type,omitempty"`
	Limit      int       `json:"limit,omitempty"`
}
