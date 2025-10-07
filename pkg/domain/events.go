package domain

import "time"

// ObserverEvent is emitted by observers (68 subtypes)
// Built on 5 months of learning, implemented with production standards
type ObserverEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`   // tcp_connect, oom_kill, dns_query, etc (68 subtypes)
	Source    string    `json:"source"` // Observer name
	Timestamp time.Time `json:"timestamp"`

	// Typed event data - NO map[string]interface{}
	NetworkData   *NetworkEventData   `json:"network_data,omitempty"`
	KernelData    *KernelEventData    `json:"kernel_data,omitempty"`
	ContainerData *ContainerEventData `json:"container_data,omitempty"`
	K8sData       *K8sEventData       `json:"k8s_data,omitempty"`
	ProcessData   *ProcessEventData   `json:"process_data,omitempty"`

	// Raw bytes for debugging
	RawData []byte `json:"raw_data,omitempty"`
}

// TapioEvent is enriched event for UKKO (12 base types)
type TapioEvent struct {
	ID        string    `json:"id"`
	Type      EventType `json:"type"`     // network, kernel, container, etc (12 base types)
	Subtype   string    `json:"subtype"`  // connection_established, oom_kill, etc
	Severity  Severity  `json:"severity"` // debug, info, warning, error, critical
	Outcome   Outcome   `json:"outcome"`  // success, failure, unknown
	Timestamp time.Time `json:"timestamp"`

	// Graph correlation - THE KEY INSIGHT FROM EXPLORATION
	Entities      []Entity       `json:"entities"`      // Nodes for graph
	Relationships []Relationship `json:"relationships"` // Edges for graph

	// Same typed data as ObserverEvent
	NetworkData   *NetworkEventData   `json:"network_data,omitempty"`
	KernelData    *KernelEventData    `json:"kernel_data,omitempty"`
	ContainerData *ContainerEventData `json:"container_data,omitempty"`
	K8sData       *K8sEventData       `json:"k8s_data,omitempty"`
	ProcessData   *ProcessEventData   `json:"process_data,omitempty"`

	// Multi-cluster support
	ClusterID string            `json:"cluster_id"`
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// EventType - 12 base types for UKKO
type EventType string

const (
	EventTypeNetwork     EventType = "network"
	EventTypeKernel      EventType = "kernel"
	EventTypeContainer   EventType = "container"
	EventTypeDeployment  EventType = "deployment"
	EventTypePod         EventType = "pod"
	EventTypeService     EventType = "service"
	EventTypeVolume      EventType = "volume"
	EventTypeConfig      EventType = "config"
	EventTypeHealth      EventType = "health"
	EventTypePerformance EventType = "performance"
	EventTypeResource    EventType = "resource"
	EventTypeCluster     EventType = "cluster"
)

// Severity levels
type Severity string

const (
	SeverityDebug    Severity = "debug"
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Outcome of event
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeUnknown Outcome = "unknown"
)

// Entity represents a graph node for correlation
type Entity struct {
	Type       EntityType        `json:"type"`
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	ClusterID  string            `json:"cluster_id"` // Multi-cluster support
	Namespace  string            `json:"namespace,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// EntityType - 12 entity types for graph
type EntityType string

const (
	EntityTypePod         EntityType = "pod"
	EntityTypeContainer   EntityType = "container"
	EntityTypeNode        EntityType = "node"
	EntityTypeDeployment  EntityType = "deployment"
	EntityTypeStatefulSet EntityType = "statefulset"
	EntityTypeDaemonSet   EntityType = "daemonset"
	EntityTypeService     EntityType = "service"
	EntityTypeEndpoint    EntityType = "endpoint"
	EntityTypeConfigMap   EntityType = "configmap"
	EntityTypeSecret      EntityType = "secret"
	EntityTypePVC         EntityType = "pvc"
	EntityTypeNamespace   EntityType = "namespace"
)

// Relationship represents a graph edge
type Relationship struct {
	Type   RelationshipType  `json:"type"`
	Source Entity            `json:"source"`
	Target Entity            `json:"target"`
	Labels map[string]string `json:"labels,omitempty"`
}

// RelationshipType - graph edge types
type RelationshipType string

const (
	RelationshipConnectsTo RelationshipType = "connects_to"
	RelationshipManages    RelationshipType = "manages"
	RelationshipDependsOn  RelationshipType = "depends_on"
	RelationshipContains   RelationshipType = "contains"
)

// NetworkEventData - network events (L3-L7)
type NetworkEventData struct {
	Protocol string `json:"protocol,omitempty"` // TCP, UDP, HTTP, DNS, gRPC
	SrcIP    string `json:"src_ip,omitempty"`
	DstIP    string `json:"dst_ip,omitempty"`
	SrcPort  uint16 `json:"src_port,omitempty"`
	DstPort  uint16 `json:"dst_port,omitempty"`

	// L7 protocol fields
	HTTPMethod      string `json:"http_method,omitempty"`
	HTTPPath        string `json:"http_path,omitempty"`
	HTTPStatusCode  int    `json:"http_status_code,omitempty"`
	DNSQuery        string `json:"dns_query,omitempty"`
	DNSResponseTime int64  `json:"dns_response_time,omitempty"`

	// Connection metadata
	Duration      int64  `json:"duration,omitempty"` // nanoseconds
	BytesSent     uint64 `json:"bytes_sent,omitempty"`
	BytesReceived uint64 `json:"bytes_received,omitempty"`
}

// KernelEventData - kernel events (syscalls, signals, OOM)
type KernelEventData struct {
	SyscallName string   `json:"syscall_name,omitempty"`
	SyscallArgs []uint64 `json:"syscall_args,omitempty"`
	SignalType  int      `json:"signal_type,omitempty"`
	ExitCode    int      `json:"exit_code,omitempty"`
	PID         int32    `json:"pid,omitempty"`
	TID         int32    `json:"tid,omitempty"`
	UID         int32    `json:"uid,omitempty"`
	GID         int32    `json:"gid,omitempty"`

	// OOM specific
	OOMKilledPID   int32  `json:"oom_killed_pid,omitempty"`
	OOMMemoryUsage uint64 `json:"oom_memory_usage,omitempty"`
	OOMMemoryLimit uint64 `json:"oom_memory_limit,omitempty"`
}

// ContainerEventData - container lifecycle events
type ContainerEventData struct {
	ContainerID   string `json:"container_id,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
	ImageName     string `json:"image_name,omitempty"`
	ImageTag      string `json:"image_tag,omitempty"`
	ExitCode      int32  `json:"exit_code,omitempty"`
	State         string `json:"state,omitempty"` // running, stopped, paused
	RestartCount  int32  `json:"restart_count,omitempty"`
}

// K8sEventData - Kubernetes API events
type K8sEventData struct {
	ResourceKind string `json:"resource_kind,omitempty"` // Deployment, Pod, Service, etc
	ResourceName string `json:"resource_name,omitempty"`
	Action       string `json:"action,omitempty"` // created, updated, deleted
	Reason       string `json:"reason,omitempty"`
	Message      string `json:"message,omitempty"`

	// Deployment specific
	ImageChanged    bool   `json:"image_changed,omitempty"`
	ReplicasChanged bool   `json:"replicas_changed,omitempty"`
	OldImage        string `json:"old_image,omitempty"`
	NewImage        string `json:"new_image,omitempty"`
	OldReplicas     int32  `json:"old_replicas,omitempty"`
	NewReplicas     int32  `json:"new_replicas,omitempty"`
}

// ProcessEventData - process events
type ProcessEventData struct {
	PID         int32  `json:"pid,omitempty"`
	PPID        int32  `json:"ppid,omitempty"`
	ProcessName string `json:"process_name,omitempty"`
	CommandLine string `json:"command_line,omitempty"`
	UID         int32  `json:"uid,omitempty"`
	GID         int32  `json:"gid,omitempty"`
	ExecTime    int64  `json:"exec_time,omitempty"` // nanoseconds
}
