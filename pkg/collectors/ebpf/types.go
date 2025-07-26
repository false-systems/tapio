package ebpf

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// EventType represents the type of eBPF event
type EventType uint32

const (
	EventTypeUnknown EventType = iota
	EventTypeSyscall
	EventTypeNetwork
	EventTypeFile
	EventTypeProcess
	EventTypeMemory
	EventTypeCPU
	EventTypeSecurity
	EventTypeContainer
	EventTypeKprobe
	EventTypeUprobe
	EventTypeTracepoint
)

// NetworkEventType represents specific network event types
type NetworkEventType uint8

const (
	NetworkEventConnect NetworkEventType = iota
	NetworkEventAccept
	NetworkEventClose
	NetworkEventSend
	NetworkEventRecv
	NetworkEventDNS
	NetworkEventHTTP
	NetworkEventTLS
)

// ProcessEventType represents specific process event types
type ProcessEventType uint8

const (
	ProcessEventExec ProcessEventType = iota
	ProcessEventExit
	ProcessEventFork
	ProcessEventSignal
	ProcessEventSetuid
	ProcessEventSetgid
)

// FileEventType represents specific file event types
type FileEventType uint8

const (
	FileEventOpen FileEventType = iota
	FileEventClose
	FileEventRead
	FileEventWrite
	FileEventCreate
	FileEventDelete
	FileEventRename
	FileEventChmod
	FileEventChown
)

// RawEvent represents a raw eBPF event as captured from kernel
// This preserves detailed kernel-level data for specialized analysis
type RawEvent struct {
	// Core fields
	Type      EventType `json:"type"`
	Timestamp uint64    `json:"timestamp"` // Kernel timestamp in nanoseconds
	CPU       uint32    `json:"cpu"`
	PID       uint32    `json:"pid"`
	TID       uint32    `json:"tid"`
	UID       uint32    `json:"uid"`
	GID       uint32    `json:"gid"`
	Comm      string    `json:"comm"` // Process name

	// Event-specific data (raw from kernel)
	Data []byte `json:"data,omitempty"`

	// Parsed event details (populated based on Type)
	Details interface{} `json:"details,omitempty"`
}

// NetworkEvent represents network-specific event data
type NetworkEvent struct {
	SubType    NetworkEventType `json:"subtype"`
	Family     uint16           `json:"family"`   // AF_INET, AF_INET6
	Protocol   uint16           `json:"protocol"` // TCP, UDP, etc
	SourceIP   string           `json:"source_ip"`
	DestIP     string           `json:"dest_ip"`
	SourcePort uint16           `json:"source_port"`
	DestPort   uint16           `json:"dest_port"`
	Size       uint32           `json:"size,omitempty"`
	Latency    uint64           `json:"latency,omitempty"` // in nanoseconds
	Direction  string           `json:"direction"`         // "ingress" or "egress"
	Interface  string           `json:"interface,omitempty"`

	// Container context if available
	ContainerID string `json:"container_id,omitempty"`
	PodName     string `json:"pod_name,omitempty"`
	Namespace   string `json:"namespace,omitempty"`

	// L7 details if captured
	L7Protocol string                 `json:"l7_protocol,omitempty"` // "http", "grpc", "dns"
	L7Details  map[string]interface{} `json:"l7_details,omitempty"`
}

// ProcessEvent represents process-specific event data
type ProcessEvent struct {
	SubType       ProcessEventType `json:"subtype"`
	ParentPID     uint32           `json:"parent_pid"`
	ParentComm    string           `json:"parent_comm"`
	Args          []string         `json:"args,omitempty"`
	Env           []string         `json:"env,omitempty"`
	Cwd           string           `json:"cwd,omitempty"`
	ExitCode      int32            `json:"exit_code,omitempty"`
	Signal        int32            `json:"signal,omitempty"`
	Cgroup        string           `json:"cgroup,omitempty"`
	ContainerID   string           `json:"container_id,omitempty"`
	ContainerName string           `json:"container_name,omitempty"`
}

// FileEvent represents file-specific event data
type FileEvent struct {
	SubType  FileEventType `json:"subtype"`
	Path     string        `json:"path"`
	Flags    uint32        `json:"flags"`
	Mode     uint32        `json:"mode"`
	Size     int64         `json:"size,omitempty"`
	Inode    uint64        `json:"inode"`
	Device   uint32        `json:"device"`
	NewPath  string        `json:"new_path,omitempty"` // For rename
	NewUID   uint32        `json:"new_uid,omitempty"`  // For chown
	NewGID   uint32        `json:"new_gid,omitempty"`  // For chown
	NewMode  uint32        `json:"new_mode,omitempty"` // For chmod
	ReadSize int32         `json:"read_size,omitempty"`
}

// SyscallEvent represents syscall-specific event data
type SyscallEvent struct {
	ID     uint32   `json:"id"`     // Syscall number
	Name   string   `json:"name"`   // Syscall name
	Args   []uint64 `json:"args"`   // Up to 6 arguments
	Return int64    `json:"return"` // Return value
	Error  int32    `json:"error"`  // Error code if any
	Entry  bool     `json:"entry"`  // true for entry, false for exit
	Stack  string   `json:"stack,omitempty"`
}

// SecurityEvent represents security-specific event data
type SecurityEvent struct {
	Type        string                 `json:"type"` // "selinux", "apparmor", "capability", etc
	Action      string                 `json:"action"`
	Subject     string                 `json:"subject"`
	Object      string                 `json:"object"`
	Result      string                 `json:"result"` // "allowed", "denied"
	Context     map[string]string      `json:"context,omitempty"`
	PolicyData  map[string]interface{} `json:"policy_data,omitempty"`
	ThreatLevel string                 `json:"threat_level,omitempty"`
}

// ContainerEvent represents container-specific event data
type ContainerEvent struct {
	Action        string            `json:"action"` // "create", "start", "stop", "destroy"
	ContainerID   string            `json:"container_id"`
	ContainerName string            `json:"container_name"`
	Image         string            `json:"image"`
	Runtime       string            `json:"runtime"` // "docker", "containerd", "cri-o"
	Namespace     string            `json:"namespace,omitempty"`
	PodName       string            `json:"pod_name,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// EnrichedEvent represents an eBPF event enriched with additional context
// This is what gets sent to the semantic layer after filtering
type EnrichedEvent struct {
	Raw         *RawEvent              `json:"raw"`
	EventID     string                 `json:"event_id"`
	Timestamp   time.Time              `json:"timestamp"`
	Hostname    string                 `json:"hostname"`
	ProcessInfo *ProcessInfo           `json:"process_info"`
	Container   *ContainerInfo         `json:"container,omitempty"`
	Kubernetes  *KubernetesInfo        `json:"kubernetes,omitempty"`
	Network     *NetworkContext        `json:"network,omitempty"`
	Security    *SecurityContext       `json:"security,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Tags        []string               `json:"tags,omitempty"`

	// Semantic hints for correlation
	SemanticType  string  `json:"semantic_type,omitempty"`  // e.g., "service_call", "file_access"
	SemanticGroup string  `json:"semantic_group,omitempty"` // e.g., "database_operation"
	TraceID       string  `json:"trace_id,omitempty"`
	SpanID        string  `json:"span_id,omitempty"`
	ParentSpanID  string  `json:"parent_span_id,omitempty"`
	ServiceName   string  `json:"service_name,omitempty"`
	OperationName string  `json:"operation_name,omitempty"`
	Importance    float64 `json:"importance"`  // 0.0 to 1.0
	Interesting   bool    `json:"interesting"` // Flag for semantic processing
}

// ProcessInfo contains enriched process information
type ProcessInfo struct {
	PID         uint32   `json:"pid"`
	TID         uint32   `json:"tid"`
	PPID        uint32   `json:"ppid"`
	UID         uint32   `json:"uid"`
	GID         uint32   `json:"gid"`
	Comm        string   `json:"comm"`
	Exe         string   `json:"exe"`
	Cmdline     []string `json:"cmdline"`
	Cgroup      string   `json:"cgroup"`
	StartTime   uint64   `json:"start_time"`
	ProcessTree []string `json:"process_tree,omitempty"`
}

// ContainerInfo contains container context
type ContainerInfo struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Runtime     string            `json:"runtime"`
	State       string            `json:"state"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// KubernetesInfo contains Kubernetes context
type KubernetesInfo struct {
	Namespace     string            `json:"namespace"`
	PodName       string            `json:"pod_name"`
	PodUID        string            `json:"pod_uid"`
	ContainerName string            `json:"container_name"`
	NodeName      string            `json:"node_name"`
	ServiceName   string            `json:"service_name,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// NetworkContext contains network enrichment data
type NetworkContext struct {
	Protocol        string                 `json:"protocol"`
	Direction       string                 `json:"direction"`
	Interface       string                 `json:"interface"`
	LocalService    string                 `json:"local_service,omitempty"`
	RemoteService   string                 `json:"remote_service,omitempty"`
	DNSNames        []string               `json:"dns_names,omitempty"`
	TLSInfo         *TLSInfo               `json:"tls_info,omitempty"`
	HTTPInfo        *HTTPInfo              `json:"http_info,omitempty"`
	LatencyMetrics  map[string]interface{} `json:"latency_metrics,omitempty"`
	BytesTransmit   uint64                 `json:"bytes_transmit"`
	BytesReceive    uint64                 `json:"bytes_receive"`
	PacketsTransmit uint64                 `json:"packets_transmit"`
	PacketsReceive  uint64                 `json:"packets_receive"`
}

// SecurityContext contains security enrichment data
type SecurityContext struct {
	SELinuxContext  string   `json:"selinux_context,omitempty"`
	AppArmorProfile string   `json:"apparmor_profile,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	Seccomp         string   `json:"seccomp,omitempty"`
	UserNamespace   bool     `json:"user_namespace"`
	Privileged      bool     `json:"privileged"`
	ThreatIndicator string   `json:"threat_indicator,omitempty"`
	RiskScore       float64  `json:"risk_score,omitempty"`
}

// TLSInfo contains TLS connection details
type TLSInfo struct {
	Version     string `json:"version"`
	CipherSuite string `json:"cipher_suite"`
	ServerName  string `json:"server_name"`
	ALPN        string `json:"alpn,omitempty"`
}

// HTTPInfo contains HTTP request/response details
type HTTPInfo struct {
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	StatusCode  int               `json:"status_code,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	UserAgent   string            `json:"user_agent,omitempty"`
}

// EventFilter defines criteria for filtering eBPF events
type EventFilter struct {
	EventTypes    []EventType            `json:"event_types,omitempty"`
	PIDs          []uint32               `json:"pids,omitempty"`
	Comms         []string               `json:"comms,omitempty"`
	UIDs          []uint32               `json:"uids,omitempty"`
	Containers    []string               `json:"containers,omitempty"`
	Namespaces    []string               `json:"namespaces,omitempty"`
	MinImportance float64                `json:"min_importance,omitempty"`
	IncludeRaw    bool                   `json:"include_raw"`
	SamplingRate  float64                `json:"sampling_rate,omitempty"` // 0.0 to 1.0
	CustomFilters map[string]interface{} `json:"custom_filters,omitempty"`
}

// Helper methods

// String returns string representation of EventType
func (e EventType) String() string {
	switch e {
	case EventTypeSyscall:
		return "syscall"
	case EventTypeNetwork:
		return "network"
	case EventTypeFile:
		return "file"
	case EventTypeProcess:
		return "process"
	case EventTypeMemory:
		return "memory"
	case EventTypeCPU:
		return "cpu"
	case EventTypeSecurity:
		return "security"
	case EventTypeContainer:
		return "container"
	case EventTypeKprobe:
		return "kprobe"
	case EventTypeUprobe:
		return "uprobe"
	case EventTypeTracepoint:
		return "tracepoint"
	default:
		return "unknown"
	}
}

// ToUnifiedEvent converts EnrichedEvent to domain.UnifiedEvent for Tapio integration
func (e *EnrichedEvent) ToUnifiedEvent() *domain.UnifiedEvent {
	// Create builder
	builder := domain.NewUnifiedEvent().
		WithSource(string(domain.SourceEBPF)).
		WithType(e.mapEventType())

	// Add trace context if available
	if e.TraceID != "" && e.SpanID != "" {
		builder = builder.WithTraceContext(e.TraceID, e.SpanID)
	}

	// Add semantic context
	builder = builder.WithSemantic(
		e.getSemanticIntent(),
		e.getSemanticCategory(),
		e.Tags...,
	)

	// Add entity context
	if e.Container != nil {
		builder = builder.WithEntity("container", e.Container.Name, e.Container.ID)
	} else if e.Kubernetes != nil {
		builder = builder.WithEntity("pod", e.Kubernetes.PodName, e.Kubernetes.Namespace)
	} else {
		builder = builder.WithEntity("process", e.Raw.Comm, "")
	}

	// Add kernel data
	builder = builder.WithKernelData(string(e.Raw.Type), e.Raw.PID)

	// Add impact based on importance
	severity := "low"
	if e.Importance > 0.8 {
		severity = "high"
	} else if e.Importance > 0.5 {
		severity = "medium"
	}
	builder = builder.WithImpact(severity, e.Importance)

	// Build the event
	event := builder.Build()

	// Add additional kernel data
	if event.Kernel != nil {
		event.Kernel.TID = e.Raw.TID
		event.Kernel.UID = e.Raw.UID
		event.Kernel.GID = e.Raw.GID
		event.Kernel.Comm = e.Raw.Comm
	}

	// Add raw data if available
	if e.Raw.Data != nil {
		event.RawData = e.Raw.Data
	}

	return event
}

func (e *EnrichedEvent) mapEventType() domain.EventType {
	switch e.Raw.Type {
	case EventTypeNetwork:
		return domain.EventTypeNetwork
	case EventTypeProcess:
		return domain.EventTypeProcess
	case EventTypeMemory:
		return domain.EventTypeMemory
	case EventTypeCPU:
		return domain.EventTypeCPU
	default:
		return domain.EventTypeSystem
	}
}

func (e *EnrichedEvent) getSemanticIntent() string {
	switch e.Raw.Type {
	case EventTypeNetwork:
		return "network-activity"
	case EventTypeProcess:
		return "process-lifecycle"
	case EventTypeMemory:
		return "memory-usage"
	case EventTypeCPU:
		return "cpu-usage"
	case EventTypeSecurity:
		return "security-event"
	default:
		return "system-event"
	}
}

func (e *EnrichedEvent) getSemanticCategory() string {
	switch e.Raw.Type {
	case EventTypeSecurity:
		return "security"
	case EventTypeMemory, EventTypeCPU:
		return "performance"
	default:
		return "system"
	}
}

// ToDomainEvent converts EnrichedEvent to domain.Event for Tapio integration
func (e *EnrichedEvent) ToDomainEvent() *domain.Event {
	eventType := domain.EventTypeSystem
	severity := domain.EventSeverityInfo

	// Map eBPF event type to domain event type
	switch e.Raw.Type {
	case EventTypeNetwork:
		eventType = domain.EventTypeNetwork
	case EventTypeProcess:
		eventType = domain.EventTypeProcess
	case EventTypeMemory:
		eventType = domain.EventTypeMemory
	case EventTypeCPU:
		eventType = domain.EventTypeCPU
	case EventTypeSecurity:
		severity = domain.EventSeverityWarning
	}

	// Build attributes
	attrs := make(map[string]interface{})
	attrs["ebpf_type"] = e.Raw.Type.String()
	attrs["pid"] = e.Raw.PID
	attrs["comm"] = e.Raw.Comm
	attrs["importance"] = e.Importance

	if e.Container != nil {
		attrs["container_id"] = e.Container.ID
		attrs["container_name"] = e.Container.Name
	}

	if e.Kubernetes != nil {
		attrs["k8s_namespace"] = e.Kubernetes.Namespace
		attrs["k8s_pod"] = e.Kubernetes.PodName
	}

	// Add raw event data if needed
	if e.Raw.Details != nil {
		if data, err := json.Marshal(e.Raw.Details); err == nil {
			attrs["raw_details"] = string(data)
		}
	}

	return &domain.Event{
		ID:        domain.EventID(e.EventID),
		Type:      eventType,
		Severity:  severity,
		Source:    domain.SourceEBPF,
		Message:   e.formatMessage(),
		Timestamp: e.Timestamp,
		Context: domain.EventContext{
			TraceID: e.TraceID,
			SpanID:  e.SpanID,
			Labels:  e.buildLabels(),
		},
		Attributes: attrs,
		Confidence: e.Importance,
		Tags:       e.Tags,
	}
}

func (e *EnrichedEvent) formatMessage() string {
	switch e.Raw.Type {
	case EventTypeNetwork:
		if net, ok := e.Raw.Details.(*NetworkEvent); ok {
			return fmt.Sprintf("Network %s: %s:%d -> %s:%d",
				net.SubType, net.SourceIP, net.SourcePort, net.DestIP, net.DestPort)
		}
	case EventTypeProcess:
		if proc, ok := e.Raw.Details.(*ProcessEvent); ok {
			return fmt.Sprintf("Process %s: %s (PID: %d)",
				proc.SubType, e.Raw.Comm, e.Raw.PID)
		}
	case EventTypeFile:
		if file, ok := e.Raw.Details.(*FileEvent); ok {
			return fmt.Sprintf("File %s: %s", file.SubType, file.Path)
		}
	}
	return fmt.Sprintf("%s event from %s (PID: %d)", e.Raw.Type, e.Raw.Comm, e.Raw.PID)
}

func (e *EnrichedEvent) buildLabels() map[string]string {
	labels := make(map[string]string)

	if e.ServiceName != "" {
		labels["service"] = e.ServiceName
	}
	if e.SemanticType != "" {
		labels["semantic_type"] = e.SemanticType
	}
	if e.Container != nil {
		labels["container"] = e.Container.Name
	}
	if e.Kubernetes != nil {
		labels["namespace"] = e.Kubernetes.Namespace
		labels["pod"] = e.Kubernetes.PodName
	}

	return labels
}
