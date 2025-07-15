package unified

import (
	"time"
)

// Event represents the unified event model combining legacy pkg/collector and modern pkg/collectors
type Event struct {
	// Core identification
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`

	// Event classification
	Type     string   `json:"type"`     // e.g., "memory_oom", "network_timeout", "pod_crash"
	Category Category `json:"category"` // Network, Memory, CPU, Disk, Process, Kubernetes, etc.
	Severity Severity `json:"severity"` // Critical, High, Medium, Low

	// Source information
	Source EventSource `json:"source"`

	// Event content
	Message    string                 `json:"message"`              // Human-readable description
	Data       map[string]interface{} `json:"data"`                 // Raw event data
	Attributes map[string]interface{} `json:"attributes,omitempty"` // Additional attributes
	Labels     map[string]string      `json:"labels,omitempty"`     // Key-value labels

	// Context and correlation
	Context  *EventContext `json:"context,omitempty"` // Kubernetes/system context
	Metadata EventMetadata `json:"metadata"`          // Processing metadata

	// Actionability
	Actionable *ActionableItem `json:"actionable,omitempty"` // Fix suggestions
}

// EventSource identifies where the event originated
type EventSource struct {
	Collector string `json:"collector"` // e.g., "ebpf", "k8s", "systemd", "journald"
	Component string `json:"component"` // e.g., "memory_tracker", "pod_watcher"
	Node      string `json:"node"`      // Node where event originated
	Version   string `json:"version"`   // Collector version
}

// EventContext provides correlation information
type EventContext struct {
	// Kubernetes context
	Pod         string            `json:"pod,omitempty"`
	Namespace   string            `json:"namespace,omitempty"`
	Container   string            `json:"container,omitempty"`
	Node        string            `json:"node,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`

	// Process context
	PID         uint32 `json:"pid,omitempty"`
	ProcessName string `json:"process_name,omitempty"`
	PPID        uint32 `json:"ppid,omitempty"`
	UID         uint32 `json:"uid,omitempty"`
	GID         uint32 `json:"gid,omitempty"`
	Command     string `json:"command,omitempty"`

	// Network context
	SrcIP   string `json:"src_ip,omitempty"`
	DstIP   string `json:"dst_ip,omitempty"`
	SrcPort uint16 `json:"src_port,omitempty"`
	DstPort uint16 `json:"dst_port,omitempty"`
	Proto   string `json:"proto,omitempty"`

	// Service mesh context
	ServiceName      string `json:"service_name,omitempty"`
	ServiceNamespace string `json:"service_namespace,omitempty"`
	MeshType         string `json:"mesh_type,omitempty"` // istio, linkerd, consul, cilium

	// CNI context
	CNIPlugin     string `json:"cni_plugin,omitempty"` // calico, flannel, cilium, weave
	NetworkPolicy string `json:"network_policy,omitempty"`
	IPAMPool      string `json:"ipam_pool,omitempty"`
}

// EventMetadata contains processing information
type EventMetadata struct {
	CollectedAt  time.Time         `json:"collected_at"`
	ProcessedAt  time.Time         `json:"processed_at"`
	ProcessingMS int64             `json:"processing_ms"`
	Tags         map[string]string `json:"tags,omitempty"`
	Correlation  *CorrelationInfo  `json:"correlation,omitempty"`
}

// CorrelationInfo tracks event relationships
type CorrelationInfo struct {
	TraceID    string   `json:"trace_id,omitempty"`
	SpanID     string   `json:"span_id,omitempty"`
	ParentID   string   `json:"parent_id,omitempty"`
	RelatedIDs []string `json:"related_ids,omitempty"`
	RootCause  bool     `json:"root_cause,omitempty"`
}

// ActionableItem provides specific fix recommendations
type ActionableItem struct {
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	Commands        []string `json:"commands"`
	Risk            Risk     `json:"risk"`
	EstimatedImpact string   `json:"estimated_impact"`
	AutoFixable     bool     `json:"auto_fixable"`
	Documentation   string   `json:"documentation,omitempty"`
}

// EventFilter provides filtering capabilities
type EventFilter struct {
	Categories   []Category             `json:"categories,omitempty"`
	Severities   []Severity             `json:"severities,omitempty"`
	Sources      []string               `json:"sources,omitempty"`
	Namespaces   []string               `json:"namespaces,omitempty"`
	Nodes        []string               `json:"nodes,omitempty"`
	TimeRange    *TimeRange             `json:"time_range,omitempty"`
	Labels       map[string]string      `json:"labels,omitempty"`
	CustomFilter map[string]interface{} `json:"custom_filter,omitempty"`
}

// TimeRange defines a time range for filtering
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// EventStats provides statistics about events
type EventStats struct {
	Total            int64              `json:"total"`
	BySeverity       map[Severity]int64 `json:"by_severity"`
	ByCategory       map[Category]int64 `json:"by_category"`
	BySource         map[string]int64   `json:"by_source"`
	ActionableCount  int64              `json:"actionable_count"`
	AutoFixableCount int64              `json:"auto_fixable_count"`
	TimeRange        TimeRange          `json:"time_range"`
}
