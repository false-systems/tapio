package domain

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// CollectorEventType represents the type of event collected
type CollectorEventType string

const (
	// System Events
	EventTypeKernelSyscall CollectorEventType = "kernel.syscall"
	EventTypeKernelProcess CollectorEventType = "kernel.process"
	EventTypeKernelNetwork CollectorEventType = "kernel.network"
	EventTypeKernelCgroup  CollectorEventType = "kernel.cgroup"
	EventTypeKernelFS      CollectorEventType = "kernel.filesystem"

	// Container Events
	EventTypeContainerCreate  CollectorEventType = "container.create"
	EventTypeContainerStart   CollectorEventType = "container.start"
	EventTypeContainerStop    CollectorEventType = "container.stop"
	EventTypeContainerDestroy CollectorEventType = "container.destroy"
	EventTypeContainerOOM     CollectorEventType = "container.oom"
	EventTypeContainerExit    CollectorEventType = "container.exit"
	EventTypeMemoryPressure   CollectorEventType = "container.memory_pressure"

	// Kubernetes Events
	EventTypeK8sPod        CollectorEventType = "k8s.pod"
	EventTypeK8sService    CollectorEventType = "k8s.service"
	EventTypeK8sDeployment CollectorEventType = "k8s.deployment"
	EventTypeK8sConfigMap  CollectorEventType = "k8s.configmap"
	EventTypeK8sSecret     CollectorEventType = "k8s.secret"
	EventTypeK8sNode       CollectorEventType = "k8s.node"
	EventTypeK8sEvent      CollectorEventType = "k8s.event"

	// Network Events
	EventTypeDNS         CollectorEventType = "network.dns"
	EventTypeDNSQuery    CollectorEventType = "dns.query"
	EventTypeDNSResponse CollectorEventType = "dns.response"
	EventTypeDNSTimeout  CollectorEventType = "dns.timeout"
	EventTypeTCP         CollectorEventType = "network.tcp"
	EventTypeHTTP        CollectorEventType = "network.http"
	EventTypeGRPC        CollectorEventType = "network.grpc"

	// Storage Events
	EventTypeETCD           CollectorEventType = "storage.etcd"
	EventTypeETCDOperation  CollectorEventType = "etcd.operation"
	EventTypeETCDWatch      CollectorEventType = "etcd.watch"
	EventTypeVolume         CollectorEventType = "storage.volume"
	EventTypeConfigStorage  CollectorEventType = "storage.config"
	EventTypeStorageIO      CollectorEventType = "storage.io"
	EventTypeStorageIORead  CollectorEventType = "storage.io.read"
	EventTypeStorageIOWrite CollectorEventType = "storage.io.write"
	EventTypeStorageIOFsync CollectorEventType = "storage.io.fsync"
	EventTypeStorageIOSlow  CollectorEventType = "storage.io.slow"

	// CRI events
	EventTypeCRIContainer CollectorEventType = "cri.container"
	EventTypeCRIPod       CollectorEventType = "cri.pod"
	EventTypeCRIImage     CollectorEventType = "cri.image"

	// Systemd Events
	EventTypeSystemdService CollectorEventType = "systemd.service"
	EventTypeSystemdUnit    CollectorEventType = "systemd.unit"
	EventTypeSystemdJournal CollectorEventType = "systemd.journal"
	EventTypeSystemdSystem  CollectorEventType = "systemd.system"

	// OpenTelemetry Events
	EventTypeOTELSpan   CollectorEventType = "otel.span"   // Application traces
	EventTypeOTELMetric CollectorEventType = "otel.metric" // Application metrics

	// Kubelet Events
	EventTypeKubeletNodeCPU             CollectorEventType = "kubelet.node.cpu"
	EventTypeKubeletNodeMemory          CollectorEventType = "kubelet.node.memory"
	EventTypeKubeletCPUThrottling       CollectorEventType = "kubelet.cpu.throttling"
	EventTypeKubeletMemoryPressure      CollectorEventType = "kubelet.memory.pressure"
	EventTypeKubeletEphemeralStorage    CollectorEventType = "kubelet.ephemeral.storage"
	EventTypeKubeletContainerWaiting    CollectorEventType = "kubelet.container.waiting"
	EventTypeKubeletContainerTerminated CollectorEventType = "kubelet.container.terminated"
	EventTypeKubeletCrashLoop           CollectorEventType = "kubelet.container.crash_loop"
	EventTypeKubeletPodNotReady         CollectorEventType = "kubelet.pod.not_ready"
)

// CollectorEvent represents a fully contextualized event from any collector
// This replaces RawEvent with a type-safe, context-rich structure
type CollectorEvent struct {
	// Core Identity - REQUIRED
	EventID   string             `json:"event_id"`
	Timestamp time.Time          `json:"timestamp"`
	Type      CollectorEventType `json:"type"`
	Source    string             `json:"source"` // collector name
	Severity  EventSeverity      `json:"severity"`

	// Structured Event Data - Type-safe replacement for []byte Data
	EventData EventDataContainer `json:"event_data"`

	// Rich Context for Correlation
	Metadata         EventMetadata     `json:"metadata"`
	CorrelationHints *CorrelationHints `json:"correlation_hints,omitempty"`
	K8sContext       *K8sContext       `json:"k8s_context,omitempty"`
	TraceContext     *TraceContext     `json:"trace_context,omitempty"`

	// Causality Chain
	CausalityContext *CausalityContext `json:"causality_context,omitempty"`

	// Collection Context
	CollectionContext *CollectionContext `json:"collection_context,omitempty"`
}

// EventDataContainer holds type-safe event data instead of raw bytes
// This eliminates map[string]interface{} abuse
type EventDataContainer struct {
	// System-level data
	SystemCall *SystemCallData `json:"syscall,omitempty"`
	Process    *ProcessData    `json:"process,omitempty"`
	Network    *NetworkData    `json:"network,omitempty"`
	Container  *ContainerData  `json:"container,omitempty"`
	FileSystem *FileSystemData `json:"filesystem,omitempty"`
	Kernel     *KernelData     `json:"kernel,omitempty"`

	// Kubernetes data
	KubernetesResource *K8sResourceData `json:"k8s_resource,omitempty"`
	KubernetesEvent    *K8sAPIEventData `json:"k8s_event,omitempty"`
	K8sResource        *K8sResourceData `json:"k8s_resource_compat,omitempty"` // Compatibility

	// Application data
	DNS  *DNSData  `json:"dns,omitempty"`
	HTTP *HTTPData `json:"http,omitempty"`
	GRPC *GRPCData `json:"grpc,omitempty"`

	// Storage data
	ETCD      *ETCDData      `json:"etcd,omitempty"`
	Volume    *VolumeData    `json:"volume,omitempty"`
	StorageIO *StorageIOData `json:"storage_io,omitempty"`

	// Container runtime data
	CRI *CRIData `json:"cri,omitempty"`

	// Systemd data
	Systemd *SystemdData `json:"systemd,omitempty"`

	// OpenTelemetry data
	OTELSpan   *OTELSpanData   `json:"otel_span,omitempty"`
	OTELMetric *OTELMetricData `json:"otel_metric,omitempty"`

	// Generic data for custom collectors (string key-value pairs only)
	Custom map[string]string `json:"custom,omitempty"`

	// Raw data for unknown/binary formats (last resort)
	RawData *RawData `json:"raw_data,omitempty"`
}

// EventMetadata contains event metadata for correlation
type EventMetadata struct {
	// Correlation IDs
	TraceID      string `json:"trace_id,omitempty"`
	SpanID       string `json:"span_id,omitempty"`
	ParentSpanID string `json:"parent_span_id,omitempty"`

	// Kubernetes context
	PodName       string `json:"pod_name,omitempty"`
	PodNamespace  string `json:"pod_namespace,omitempty"`
	PodUID        string `json:"pod_uid,omitempty"`
	ContainerID   string `json:"container_id,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
	NodeName      string `json:"node_name,omitempty"`

	// Process context
	PID      int32  `json:"pid,omitempty"`
	PPID     int32  `json:"ppid,omitempty"`
	UID      int32  `json:"uid,omitempty"`
	GID      int32  `json:"gid,omitempty"`
	CgroupID uint64 `json:"cgroup_id,omitempty"`
	Command  string `json:"command,omitempty"`

	// Additional metadata
	Priority      EventPriority     `json:"priority,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
	SchemaVersion string            `json:"schema_version,omitempty"`

	// Correlation hints
	CorrelationHints []string `json:"correlation_hints,omitempty"`
}

// EventPriority represents event priority levels
type EventPriority string

const (
	PriorityLow      EventPriority = "low"
	PriorityNormal   EventPriority = "normal"
	PriorityHigh     EventPriority = "high"
	PriorityCritical EventPriority = "critical"
)

// KernelData represents kernel-level event data
type KernelData struct {
	EventType    string `json:"event_type"`
	Syscall      string `json:"syscall,omitempty"`
	ReturnCode   int32  `json:"return_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`

	// Process info
	PID      int32  `json:"pid"`
	PPID     int32  `json:"ppid"`
	UID      int32  `json:"uid"`
	GID      int32  `json:"gid"`
	Command  string `json:"command"`
	CgroupID uint64 `json:"cgroup_id"`

	// Network info (if applicable)
	SrcIP   string `json:"src_ip,omitempty"`
	DstIP   string `json:"dst_ip,omitempty"`
	SrcPort uint16 `json:"src_port,omitempty"`
	DstPort uint16 `json:"dst_port,omitempty"`

	// File info (if applicable)
	Filename string `json:"filename,omitempty"`
	Flags    uint32 `json:"flags,omitempty"`
	Mode     uint32 `json:"mode,omitempty"`
}

// StorageIOData represents storage I/O event data
type StorageIOData struct {
	Operation string        `json:"operation"` // read, write, fsync, opendir
	Path      string        `json:"path"`
	Size      int64         `json:"size"`
	Offset    int64         `json:"offset"`
	Duration  time.Duration `json:"duration"`
	SlowIO    bool          `json:"slow_io"`
	BlockedIO bool          `json:"blocked_io"` // blocked operation

	// File system info
	Device     string `json:"device"`
	Inode      uint64 `json:"inode"`
	MountPoint string `json:"mount_point"`
	FileSystem string `json:"file_system"`

	// Kubernetes context
	K8sPath      string `json:"k8s_path,omitempty"`    // K8s-specific path type
	VolumeType   string `json:"volume_type,omitempty"` // PVC, ConfigMap, Secret, etc.
	PVCName      string `json:"pvc_name,omitempty"`
	StorageClass string `json:"storage_class,omitempty"`

	// VFS operation details
	VFSLayer string `json:"vfs_layer,omitempty"` // vfs_read, vfs_write, vfs_fsync
	Flags    uint32 `json:"flags,omitempty"`     // operation flags
	Mode     uint32 `json:"mode,omitempty"`      // file mode

	// Performance metrics
	LatencyMS  float64       `json:"latency_ms"`
	CPUTime    time.Duration `json:"cpu_time,omitempty"`   // CPU time consumed
	QueueTime  time.Duration `json:"queue_time,omitempty"` // time in I/O queue
	BlockTime  time.Duration `json:"block_time,omitempty"` // time blocked
	IOPS       int           `json:"iops,omitempty"`
	Throughput int           `json:"throughput_mb_s,omitempty"`

	// Error info
	ErrorCode    int32  `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// SystemCallData represents system call information
type SystemCallData struct {
	Number    int64             `json:"number"`
	Name      string            `json:"name"`
	PID       int32             `json:"pid"`
	TID       int32             `json:"tid"`
	UID       int32             `json:"uid"`
	GID       int32             `json:"gid"`
	Arguments []SystemCallArg   `json:"arguments,omitempty"`
	RetValue  int64             `json:"return_value"`
	Duration  time.Duration     `json:"duration"`
	ErrorCode int32             `json:"error_code,omitempty"`
	Flags     map[string]string `json:"flags,omitempty"`
}

// SystemCallArg represents a system call argument
type SystemCallArg struct {
	Index int64  `json:"index"`
	Type  string `json:"type"`
	Value string `json:"value"`
	Size  int64  `json:"size,omitempty"`
}

// ProcessData represents process information
type ProcessData struct {
	PID         int32             `json:"pid"`
	PPID        int32             `json:"ppid"`
	TID         int32             `json:"tid"`
	Command     string            `json:"command"`
	Arguments   []string          `json:"arguments,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Executable  string            `json:"executable"`
	StartTime   time.Time         `json:"start_time"`
	UID         int32             `json:"uid"`
	GID         int32             `json:"gid"`
	CgroupPath  string            `json:"cgroup_path,omitempty"`
	ContainerID string            `json:"container_id,omitempty"`
}

// NetworkData represents network activity
type NetworkData struct {
	Protocol    string        `json:"protocol"`  // tcp, udp, icmp
	Direction   string        `json:"direction"` // inbound, outbound
	SourceIP    string        `json:"source_ip"`
	SourcePort  int32         `json:"source_port"`
	DestIP      string        `json:"dest_ip"`
	DestPort    int32         `json:"dest_port"`
	BytesSent   int64         `json:"bytes_sent"`
	BytesRecv   int64         `json:"bytes_recv"`
	PacketsSent int64         `json:"packets_sent"`
	PacketsRecv int64         `json:"packets_recv"`
	Latency     time.Duration `json:"latency,omitempty"`
	TCPFlags    []string      `json:"tcp_flags,omitempty"`
	Interface   string        `json:"interface,omitempty"`
}

// ContainerData represents container runtime events
type ContainerData struct {
	ContainerID string            `json:"container_id"`
	ImageID     string            `json:"image_id"`
	ImageName   string            `json:"image_name"`
	Runtime     string            `json:"runtime"` // docker, containerd, cri-o
	State       string            `json:"state"`   // created, running, stopped
	Action      string            `json:"action"`  // create, start, stop, destroy
	ExitCode    *int32            `json:"exit_code,omitempty"`
	Signal      *int32            `json:"signal,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Mounts      []MountInfo       `json:"mounts,omitempty"`
	NetworkMode string            `json:"network_mode,omitempty"`
	PID         int32             `json:"pid,omitempty"`
}

// MountInfo represents mount information
type MountInfo struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Type        string `json:"type"`
	Options     string `json:"options,omitempty"`
	ReadOnly    bool   `json:"read_only"`
}

// FileSystemData represents filesystem operations
type FileSystemData struct {
	Operation string        `json:"operation"` // read, write, open, close, create, delete
	Path      string        `json:"path"`
	Mode      string        `json:"mode,omitempty"`
	Size      int64         `json:"size,omitempty"`
	Offset    int64         `json:"offset,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
	ErrorCode int32         `json:"error_code,omitempty"`
	Inode     uint64        `json:"inode,omitempty"`
	Device    string        `json:"device,omitempty"`
	Flags     []string      `json:"flags,omitempty"`
}

// K8sResourceData represents Kubernetes resource data
type K8sResourceData struct {
	APIVersion string `json:"api_version"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	UID        string `json:"uid"`

	// Operation that triggered the event
	Operation string `json:"operation"` // create, update, delete, patch

	// Resource version for ordering
	ResourceVersion string `json:"resource_version"`
	Generation      int64  `json:"generation,omitempty"`

	// Owner references
	OwnerKind       string           `json:"owner_kind,omitempty"`
	OwnerName       string           `json:"owner_name,omitempty"`
	OwnerUID        string           `json:"owner_uid,omitempty"`
	OwnerReferences []OwnerReference `json:"owner_references,omitempty"`

	// Status info
	Phase      string   `json:"phase,omitempty"`
	Conditions []string `json:"conditions,omitempty"`
	Message    string   `json:"message,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Finalizers []string `json:"finalizers,omitempty"`

	// Labels and annotations
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
	DeletionTimestamp *time.Time        `json:"deletion_timestamp,omitempty"`

	// Raw object (for complex processing)
	Object interface{} `json:"object,omitempty"`
}

// K8sAPIEventData represents Kubernetes API events
type K8sAPIEventData struct {
	Action         string          `json:"action"` // ADDED, MODIFIED, DELETED
	Reason         string          `json:"reason"`
	Message        string          `json:"message"`
	Type           string          `json:"type"` // Normal, Warning
	Count          int32           `json:"count"`
	FirstTime      time.Time       `json:"first_time"`
	LastTime       time.Time       `json:"last_time"`
	InvolvedObject K8sResourceData `json:"involved_object"`
	Source         EventSource     `json:"source"`
}

// EventSource represents the source of a K8s event
type EventSource struct {
	Component string `json:"component"`
	Host      string `json:"host,omitempty"`
}

// DNSData represents DNS event data
type DNSData struct {
	QueryType    string        `json:"query_type"` // A, AAAA, CNAME, etc.
	QueryName    string        `json:"query_name"`
	ResponseCode int           `json:"response_code"`
	Answers      []string      `json:"answers,omitempty"`
	Duration     time.Duration `json:"duration"`

	// Network context
	ClientIP   string `json:"client_ip"`
	ServerIP   string `json:"server_ip"`
	ClientPort uint16 `json:"client_port"`
	ServerPort uint16 `json:"server_port"`

	// Error info
	Error        bool   `json:"error"`
	ErrorMessage string `json:"error_message,omitempty"`

	// Extended DNS fields from main
	Authorities []DNSAnswer `json:"authorities,omitempty"`
	Additional  []DNSAnswer `json:"additional,omitempty"`
	Cached      bool        `json:"cached,omitempty"`
}

// DNSAnswer represents a DNS answer record
type DNSAnswer struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Class string `json:"class"`
	TTL   int32  `json:"ttl"`
	Data  string `json:"data"`
}

// HTTPData represents HTTP request/response data
type HTTPData struct {
	Method       string            `json:"method"`
	URL          string            `json:"url"`
	StatusCode   int32             `json:"status_code"`
	RequestSize  int64             `json:"request_size"`
	ResponseSize int64             `json:"response_size"`
	Duration     time.Duration     `json:"duration"`
	UserAgent    string            `json:"user_agent,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	ContentType  string            `json:"content_type,omitempty"`
}

// GRPCData represents gRPC call data
type GRPCData struct {
	Service      string            `json:"service"`
	Method       string            `json:"method"`
	StatusCode   int32             `json:"status_code"`
	Duration     time.Duration     `json:"duration"`
	RequestSize  int64             `json:"request_size"`
	ResponseSize int64             `json:"response_size"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// CRIData represents container runtime event data
type CRIData struct {
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	PodName       string `json:"pod_name"`
	PodNamespace  string `json:"pod_namespace"`
	PodUID        string `json:"pod_uid"`

	// Container lifecycle
	Action string `json:"action"` // create, start, stop, remove
	State  string `json:"state"`  // created, running, exited, unknown

	// Image info
	ImageName string `json:"image_name"`
	ImageID   string `json:"image_id"`

	// Runtime info
	Runtime        string `json:"runtime"` // containerd, cri-o, docker
	RuntimeVersion string `json:"runtime_version"`

	// Exit info
	ExitCode   int32  `json:"exit_code,omitempty"`
	ExitReason string `json:"exit_reason,omitempty"`
	OOMKilled  bool   `json:"oom_killed"`

	// Resource info
	CPURequest    string `json:"cpu_request,omitempty"`
	CPULimit      string `json:"cpu_limit,omitempty"`
	MemoryRequest string `json:"memory_request,omitempty"`
	MemoryLimit   string `json:"memory_limit,omitempty"`
}

// ETCDData represents ETCD operation data
type ETCDData struct {
	Operation    string        `json:"operation"` // get, put, delete, watch
	Key          string        `json:"key"`
	Value        string        `json:"value,omitempty"`
	Revision     int64         `json:"revision"`
	LeaseID      int64         `json:"lease_id,omitempty"`
	Duration     time.Duration `json:"duration"`
	ResponseCode int           `json:"response_code"`

	// Kubernetes context (if applicable)
	ResourceType string `json:"resource_type,omitempty"`
	ResourceName string `json:"resource_name,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
}

// VolumeData represents volume operation data
type VolumeData struct {
	Operation  string        `json:"operation"` // mount, unmount, create, delete
	VolumeName string        `json:"volume_name"`
	VolumeType string        `json:"volume_type"` // pvc, configmap, secret, hostpath
	MountPath  string        `json:"mount_path,omitempty"`
	ReadOnly   bool          `json:"read_only"`
	Size       int64         `json:"size,omitempty"`
	Duration   time.Duration `json:"duration,omitempty"`
	ErrorCode  int32         `json:"error_code,omitempty"`
}

// SystemdData represents systemd event data
type SystemdData struct {
	Unit     string `json:"unit"`
	Message  string `json:"message"`
	Priority string `json:"priority"`

	// Service state
	ActiveState string `json:"active_state,omitempty"`
	SubState    string `json:"sub_state,omitempty"`
	Result      string `json:"result,omitempty"`

	// Process info
	MainPID    int32 `json:"main_pid,omitempty"`
	ControlPID int32 `json:"control_pid,omitempty"`

	// Resource usage
	MemoryCurrent uint64 `json:"memory_current,omitempty"`
	CPUUsageNSec  uint64 `json:"cpu_usage_nsec,omitempty"`

	// Journal fields
	Hostname  string `json:"hostname,omitempty"`
	MachineID string `json:"machine_id,omitempty"`
	BootID    string `json:"boot_id,omitempty"`
}

// RawData holds raw binary/unknown data (fallback only)
type RawData struct {
	Format      string `json:"format"` // "protobuf", "json", "binary"
	ContentType string `json:"content_type,omitempty"`
	Data        []byte `json:"data"`
	Size        int64  `json:"size"`
}

// CorrelationHints provides hints for correlation algorithms
type CorrelationHints struct {
	// Resource correlation keys
	PodUID      string `json:"pod_uid,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	ProcessID   int32  `json:"process_id,omitempty"`
	CgroupPath  string `json:"cgroup_path,omitempty"`
	NodeName    string `json:"node_name,omitempty"`

	// Network correlation keys
	ConnectionID string `json:"connection_id,omitempty"`
	FlowID       string `json:"flow_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`

	// Application correlation keys
	RequestID     string `json:"request_id,omitempty"`
	TransactionID string `json:"transaction_id,omitempty"`
	UserID        string `json:"user_id,omitempty"`

	// Temporal correlation hints
	WindowStart   time.Time     `json:"window_start,omitempty"`
	WindowEnd     time.Time     `json:"window_end,omitempty"`
	ExpectedDelay time.Duration `json:"expected_delay,omitempty"`

	// Custom correlation dimensions
	CorrelationTags map[string]string `json:"correlation_tags,omitempty"`
}

// TraceContext contains distributed tracing context
type TraceContext struct {
	TraceID    trace.TraceID     `json:"trace_id"`
	SpanID     trace.SpanID      `json:"span_id"`
	TraceFlags trace.TraceFlags  `json:"trace_flags"`
	TraceState trace.TraceState  `json:"trace_state,omitempty"`
	Baggage    map[string]string `json:"baggage,omitempty"`
}

// CollectionContext contains context about the collection process
type CollectionContext struct {
	CollectorVersion string           `json:"collector_version"`
	HostInfo         HostInfo         `json:"host_info"`
	CollectionConfig CollectionConfig `json:"collection_config"`
	BufferStats      BufferStats      `json:"buffer_stats"`
}

// HostInfo contains information about the collection host
type HostInfo struct {
	Hostname         string `json:"hostname"`
	KernelVersion    string `json:"kernel_version"`
	OSVersion        string `json:"os_version"`
	Architecture     string `json:"architecture"`
	ContainerRuntime string `json:"container_runtime,omitempty"`
	K8sVersion       string `json:"k8s_version,omitempty"`
}

// CollectionConfig contains collection configuration
type CollectionConfig struct {
	SamplingRate    float64           `json:"sampling_rate"`
	BufferSize      int               `json:"buffer_size"`
	FlushInterval   time.Duration     `json:"flush_interval"`
	EnabledFeatures []string          `json:"enabled_features"`
	FilterRules     map[string]string `json:"filter_rules,omitempty"`
}

// BufferStats contains buffer utilization statistics
type BufferStats struct {
	TotalCapacity   int64   `json:"total_capacity"`
	CurrentUsage    int64   `json:"current_usage"`
	UtilizationRate float64 `json:"utilization_rate"`
	DroppedEvents   int64   `json:"dropped_events"`
	ProcessedEvents int64   `json:"processed_events"`
}

// CollectorEventProcessor processes CollectorEvents (replaces RawEventProcessor)
type CollectorEventProcessor interface {
	ProcessCollectorEvent(ctx context.Context, event *CollectorEvent) error
}

// CollectorEventParser parses raw data into CollectorEvents
type CollectorEventParser interface {
	Parse(rawData []byte, source string) (*CollectorEvent, error)
	Source() string
	SupportedTypes() []CollectorEventType
}

// Validation methods

// Validate performs comprehensive validation of the CollectorEvent
func (e *CollectorEvent) Validate() error {
	if e.EventID == "" {
		return fmt.Errorf("event_id is required")
	}

	if e.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is required")
	}

	if e.Type == "" {
		return fmt.Errorf("event type is required")
	}

	if e.Source == "" {
		return fmt.Errorf("source is required")
	}

	if err := e.EventData.Validate(); err != nil {
		return fmt.Errorf("event data validation failed: %w", err)
	}

	return nil
}

// Validate validates EventDataContainer
func (edc *EventDataContainer) Validate() error {
	// Check if at least one data field is present
	if edc.SystemCall == nil &&
		edc.Process == nil &&
		edc.Network == nil &&
		edc.Container == nil &&
		edc.FileSystem == nil &&
		edc.Kernel == nil &&
		edc.KubernetesResource == nil &&
		edc.KubernetesEvent == nil &&
		edc.K8sResource == nil &&
		edc.DNS == nil &&
		edc.HTTP == nil &&
		edc.GRPC == nil &&
		edc.ETCD == nil &&
		edc.Volume == nil &&
		edc.StorageIO == nil &&
		edc.CRI == nil &&
		edc.Systemd == nil &&
		edc.RawData == nil {
		return fmt.Errorf("at least one event data field must be present")
	}

	return nil
}

// Helper methods for type-safe data extraction

// GetSystemCallData safely extracts system call data
func (e *CollectorEvent) GetSystemCallData() (*SystemCallData, bool) {
	if e.EventData.SystemCall != nil {
		return e.EventData.SystemCall, true
	}
	return nil, false
}

// GetProcessData safely extracts process data
func (e *CollectorEvent) GetProcessData() (*ProcessData, bool) {
	if e.EventData.Process != nil {
		return e.EventData.Process, true
	}
	return nil, false
}

// GetNetworkData safely extracts network data
func (e *CollectorEvent) GetNetworkData() (*NetworkData, bool) {
	if e.EventData.Network != nil {
		return e.EventData.Network, true
	}
	return nil, false
}

// GetContainerData safely extracts container data
func (e *CollectorEvent) GetContainerData() (*ContainerData, bool) {
	if e.EventData.Container != nil {
		return e.EventData.Container, true
	}
	return nil, false
}

// GetK8sResourceData safely extracts Kubernetes resource data
func (e *CollectorEvent) GetK8sResourceData() (*K8sResourceData, bool) {
	if e.EventData.KubernetesResource != nil {
		return e.EventData.KubernetesResource, true
	}
	// Fallback for compatibility
	if e.EventData.K8sResource != nil {
		return e.EventData.K8sResource, true
	}
	return nil, false
}

// GetDNSData safely extracts DNS data
func (e *CollectorEvent) GetDNSData() (*DNSData, bool) {
	if e.EventData.DNS != nil {
		return e.EventData.DNS, true
	}
	return nil, false
}

// GetSystemdData safely extracts systemd data
func (e *CollectorEvent) GetSystemdData() (*SystemdData, bool) {
	if e.EventData.Systemd != nil {
		return e.EventData.Systemd, true
	}
	return nil, false
}

// GetStorageIOData safely extracts storage I/O data
func (e *CollectorEvent) GetStorageIOData() (*StorageIOData, bool) {
	if e.EventData.StorageIO != nil {
		return e.EventData.StorageIO, true
	}
	return nil, false
}

// GetETCDData safely extracts ETCD data
func (e *CollectorEvent) GetETCDData() (*ETCDData, bool) {
	if e.EventData.ETCD != nil {
		return e.EventData.ETCD, true
	}
	return nil, false
}

// GetCRIData safely extracts CRI data
func (e *CollectorEvent) GetCRIData() (*CRIData, bool) {
	if e.EventData.CRI != nil {
		return e.EventData.CRI, true
	}
	return nil, false
}

// HasTraceContext checks if trace context is available
func (e *CollectorEvent) HasTraceContext() bool {
	return e.TraceContext != nil && e.TraceContext.TraceID.IsValid()
}

// GetCorrelationKey generates a correlation key for grouping related events
func (e *CollectorEvent) GetCorrelationKey() string {
	if e.CorrelationHints == nil {
		return fmt.Sprintf("source:%s", e.Source)
	}

	hints := e.CorrelationHints

	// Prioritize container-level correlation
	if hints.ContainerID != "" {
		return fmt.Sprintf("container:%s", hints.ContainerID)
	}

	// Pod-level correlation
	if hints.PodUID != "" {
		return fmt.Sprintf("pod:%s", hints.PodUID)
	}

	// Process-level correlation
	if hints.ProcessID != 0 {
		return fmt.Sprintf("process:%d", hints.ProcessID)
	}

	// Node-level correlation
	if hints.NodeName != "" {
		return fmt.Sprintf("node:%s", hints.NodeName)
	}

	// Fallback to source
	return fmt.Sprintf("source:%s", e.Source)
}

// IsHighPriority checks if the event is high priority
func (e *CollectorEvent) IsHighPriority() bool {
	return e.Metadata.Priority == PriorityHigh || e.Metadata.Priority == PriorityCritical
}

// AddCorrelationTag adds a correlation tag
func (e *CollectorEvent) AddCorrelationTag(key, value string) {
	if e.CorrelationHints == nil {
		e.CorrelationHints = &CorrelationHints{}
	}
	if e.CorrelationHints.CorrelationTags == nil {
		e.CorrelationHints.CorrelationTags = make(map[string]string)
	}
	e.CorrelationHints.CorrelationTags[key] = value
}

// AddMetadataLabel adds a metadata label
func (e *CollectorEvent) AddMetadataLabel(key, value string) {
	if e.Metadata.Labels == nil {
		e.Metadata.Labels = make(map[string]string)
	}
	e.Metadata.Labels[key] = value
}

// Helper methods for EventDataContainer type-safe access

// GetKernelData returns kernel data if present
func (e *EventDataContainer) GetKernelData() (*KernelData, bool) {
	if e.Kernel != nil {
		return e.Kernel, true
	}
	return nil, false
}

// GetStorageIOData returns storage I/O data if present
func (e *EventDataContainer) GetStorageIOData() (*StorageIOData, bool) {
	if e.StorageIO != nil {
		return e.StorageIO, true
	}
	return nil, false
}

// GetK8sResourceData returns K8s resource data if present
func (e *EventDataContainer) GetK8sResourceData() (*K8sResourceData, bool) {
	if e.KubernetesResource != nil {
		return e.KubernetesResource, true
	}
	// Fallback for compatibility
	if e.K8sResource != nil {
		return e.K8sResource, true
	}
	return nil, false
}

// GetDNSData returns DNS data if present
func (e *EventDataContainer) GetDNSData() (*DNSData, bool) {
	if e.DNS != nil {
		return e.DNS, true
	}
	return nil, false
}

// GetCRIData returns CRI data if present
func (e *EventDataContainer) GetCRIData() (*CRIData, bool) {
	if e.CRI != nil {
		return e.CRI, true
	}
	return nil, false
}

// GetETCDData returns ETCD data if present
func (e *EventDataContainer) GetETCDData() (*ETCDData, bool) {
	if e.ETCD != nil {
		return e.ETCD, true
	}
	return nil, false
}

// GetSystemdData returns systemd data if present
func (e *EventDataContainer) GetSystemdData() (*SystemdData, bool) {
	if e.Systemd != nil {
		return e.Systemd, true
	}
	return nil, false
}

// OTELSpanData represents OpenTelemetry span/trace data
// This is the core of APM - shows what the application is doing
type OTELSpanData struct {
	// Identity
	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ParentSpanID string `json:"parent_span_id,omitempty"`

	// Span details
	Name          string    `json:"name"`         // Operation name e.g. "GET /api/users"
	Kind          string    `json:"kind"`         // SERVER, CLIENT, PRODUCER, CONSUMER
	ServiceName   string    `json:"service_name"` // Service that generated this span
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
	DurationNanos int64     `json:"duration_nanos"` // Nanoseconds for precision

	// Status
	StatusCode    string `json:"status_code"` // OK, ERROR, UNSET
	StatusMessage string `json:"status_message,omitempty"`

	// Key attributes that help correlation
	HTTPMethod     string `json:"http_method,omitempty"`
	HTTPStatusCode int    `json:"http_status_code,omitempty"`
	HTTPURL        string `json:"http_url,omitempty"`
	DBStatement    string `json:"db_statement,omitempty"`
	DBSystem       string `json:"db_system,omitempty"`
	RPCService     string `json:"rpc_service,omitempty"`
	RPCMethod      string `json:"rpc_method,omitempty"`

	// Resource attributes (where this ran)
	K8sPodName    string `json:"k8s_pod_name,omitempty"`
	K8sNamespace  string `json:"k8s_namespace,omitempty"`
	K8sDeployment string `json:"k8s_deployment,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
	ProcessPID    int32  `json:"process_pid,omitempty"`

	// Custom attributes (limited set for important data)
	Attributes map[string]string `json:"attributes,omitempty"`

	// Events that occurred during the span
	Events []OTELSpanEvent `json:"events,omitempty"`
}

// OTELSpanEvent represents an event that occurred during a span
type OTELSpanEvent struct {
	Timestamp  time.Time         `json:"timestamp"`
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// OTELMetricData represents OpenTelemetry metric data
type OTELMetricData struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Unit        string    `json:"unit,omitempty"`
	Type        string    `json:"type"` // GAUGE, COUNTER, HISTOGRAM
	Timestamp   time.Time `json:"timestamp"`

	// Metric value (use appropriate field based on type)
	GaugeValue   float64 `json:"gauge_value,omitempty"`
	CounterValue int64   `json:"counter_value,omitempty"`
	Sum          float64 `json:"sum,omitempty"`
	Count        uint64  `json:"count,omitempty"`
	Min          float64 `json:"min,omitempty"`
	Max          float64 `json:"max,omitempty"`

	// Labels/attributes
	Labels map[string]string `json:"labels,omitempty"`

	// Resource context
	ServiceName  string `json:"service_name,omitempty"`
	K8sPodName   string `json:"k8s_pod_name,omitempty"`
	K8sNamespace string `json:"k8s_namespace,omitempty"`
}
