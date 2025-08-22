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

	// Network Events
	EventTypeDNS  CollectorEventType = "network.dns"
	EventTypeTCP  CollectorEventType = "network.tcp"
	EventTypeHTTP CollectorEventType = "network.http"
	EventTypeGRPC CollectorEventType = "network.grpc"

	// Storage Events
	EventTypeETCD          CollectorEventType = "storage.etcd"
	EventTypeVolume        CollectorEventType = "storage.volume"
	EventTypeConfigStorage CollectorEventType = "storage.config"

	// Systemd Events
	EventTypeSystemdService CollectorEventType = "systemd.service"
	EventTypeSystemdUnit    CollectorEventType = "systemd.unit"
	EventTypeSystemdJournal CollectorEventType = "systemd.journal"
	EventTypeSystemdSystem  CollectorEventType = "systemd.system"
)

// CollectorEvent represents a fully contextualized event from any collector
// This replaces RawEvent with a type-safe, context-rich structure
type CollectorEvent struct {
	// Core Identity - REQUIRED
	EventID   string             `json:"event_id"`
	Timestamp time.Time          `json:"timestamp"`
	Type      CollectorEventType `json:"type"`
	Source    string             `json:"source"` // collector name

	// Structured Event Data - Type-safe replacement for []byte Data
	EventData EventDataContainer `json:"event_data"`

	// Rich Context for Correlation
	Metadata         EventMetadata    `json:"metadata"`
	CorrelationHints CorrelationHints `json:"correlation_hints"`
	K8sContext       *K8sContext      `json:"k8s_context,omitempty"`
	TraceContext     *TraceContext    `json:"trace_context,omitempty"`

	// Causality Chain
	CausalityContext *CausalityContext `json:"causality_context,omitempty"`

	// Collection Context
	CollectionContext CollectionContext `json:"collection_context"`
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

	// Kubernetes data
	KubernetesResource *K8sResourceData `json:"k8s_resource,omitempty"`
	KubernetesEvent    *K8sAPIEventData `json:"k8s_event,omitempty"`

	// Application data
	DNS  *DNSData  `json:"dns,omitempty"`
	HTTP *HTTPData `json:"http,omitempty"`
	GRPC *GRPCData `json:"grpc,omitempty"`

	// Storage data
	ETCD   *ETCDData   `json:"etcd,omitempty"`
	Volume *VolumeData `json:"volume,omitempty"`

	// Systemd data
	Systemd *SystemdData `json:"systemd,omitempty"`

	// Raw data for unknown/binary formats (last resort)
	RawData *RawData `json:"raw_data,omitempty"`
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

// K8sResourceData represents Kubernetes resource events
type K8sResourceData struct {
	APIVersion        string            `json:"api_version"`
	Kind              string            `json:"kind"`
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	UID               string            `json:"uid"`
	ResourceVersion   string            `json:"resource_version"`
	Generation        int64             `json:"generation"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
	Finalizers        []string          `json:"finalizers,omitempty"`
	OwnerReferences   []OwnerReference  `json:"owner_references,omitempty"`
	DeletionTimestamp *time.Time        `json:"deletion_timestamp,omitempty"`
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

// DNSData represents DNS query/response data
type DNSData struct {
	QueryType    string        `json:"query_type"` // A, AAAA, CNAME, MX
	QueryName    string        `json:"query_name"`
	ResponseCode int32         `json:"response_code"` // DNS response code
	Answers      []DNSAnswer   `json:"answers,omitempty"`
	Authorities  []DNSAnswer   `json:"authorities,omitempty"`
	Additional   []DNSAnswer   `json:"additional,omitempty"`
	Duration     time.Duration `json:"duration"`
	Cached       bool          `json:"cached"`
	ServerIP     string        `json:"server_ip"`
	ServerPort   int32         `json:"server_port"`
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

// ETCDData represents etcd operation data
type ETCDData struct {
	Operation    string        `json:"operation"` // get, put, delete, watch
	Key          string        `json:"key"`
	Value        string        `json:"value,omitempty"`
	Revision     int64         `json:"revision"`
	Duration     time.Duration `json:"duration"`
	ResponseCode int32         `json:"response_code"`
	LeaseID      int64         `json:"lease_id,omitempty"`
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

// SystemdData represents systemd journal and service data
type SystemdData struct {
	// Event classification
	EventType string `json:"event_type"` // service.start, service.stop, unit.failed, journal.entry
	Source    string `json:"source"`     // journal, dbus

	// Journal entry data
	Message   string    `json:"message"`
	Priority  int32     `json:"priority"`  // syslog priority level
	Timestamp time.Time `json:"timestamp"` // journal entry timestamp

	// Process information
	PID     int32  `json:"pid,omitempty"`
	Command string `json:"command,omitempty"`

	// Systemd unit information
	UnitName     string `json:"unit_name,omitempty"`
	UnitType     string `json:"unit_type,omitempty"`     // service, socket, timer, etc.
	UnitState    string `json:"unit_state,omitempty"`    // active, inactive, failed
	SubState     string `json:"sub_state,omitempty"`     // running, dead, failed
	UnitResult   string `json:"unit_result,omitempty"`   // success, failed, timeout
	InvocationID string `json:"invocation_id,omitempty"` // systemd invocation ID
	JobID        string `json:"job_id,omitempty"`        // systemd job ID
	JobType      string `json:"job_type,omitempty"`      // start, stop, restart
	JobResult    string `json:"job_result,omitempty"`    // done, failed, timeout

	// System information
	Hostname  string `json:"hostname,omitempty"`
	MachineID string `json:"machine_id,omitempty"`
	BootID    string `json:"boot_id,omitempty"`
	Transport string `json:"transport,omitempty"` // journal, stdout, syslog

	// Container/cgroup information for K8s correlation
	CgroupPath  string `json:"cgroup_path,omitempty"`
	ContainerID string `json:"container_id,omitempty"`

	// Error information
	ErrorCode    int32         `json:"error_code,omitempty"`
	ErrorMessage string        `json:"error_message,omitempty"`
	ExitCode     int32         `json:"exit_code,omitempty"`
	Signal       int32         `json:"signal,omitempty"`
	Duration     time.Duration `json:"duration,omitempty"`

	// Performance data
	MemoryUsage  int64   `json:"memory_usage,omitempty"`  // bytes
	CPUUsage     float64 `json:"cpu_usage,omitempty"`     // percentage
	RestartCount int32   `json:"restart_count,omitempty"` // service restart count

	// Additional journal fields for debugging
	SyslogID    string            `json:"syslog_id,omitempty"`
	Fields      map[string]string `json:"fields,omitempty"`
	ExtraFields map[string]string `json:"extra_fields,omitempty"`
}

// RawData holds raw binary/unknown data (fallback only)
type RawData struct {
	Format      string `json:"format"` // "protobuf", "json", "binary"
	ContentType string `json:"content_type,omitempty"`
	Data        []byte `json:"data"`
	Size        int64  `json:"size"`
}

// EventMetadata contains event metadata for correlation
type EventMetadata struct {
	Priority      EventPriority     `json:"priority"`
	Tags          []string          `json:"tags,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
	SchemaVersion string            `json:"schema_version"`
}

// EventPriority represents event priority levels
type EventPriority string

const (
	PriorityLow      EventPriority = "low"
	PriorityNormal   EventPriority = "normal"
	PriorityHigh     EventPriority = "high"
	PriorityCritical EventPriority = "critical"
)

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
		edc.KubernetesResource == nil &&
		edc.KubernetesEvent == nil &&
		edc.DNS == nil &&
		edc.HTTP == nil &&
		edc.GRPC == nil &&
		edc.ETCD == nil &&
		edc.Volume == nil &&
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

// HasTraceContext checks if trace context is available
func (e *CollectorEvent) HasTraceContext() bool {
	return e.TraceContext != nil && !e.TraceContext.TraceID.IsValid()
}

// GetCorrelationKey generates a correlation key for grouping related events
func (e *CollectorEvent) GetCorrelationKey() string {
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
