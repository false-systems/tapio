package domain

import (
	"time"
)

// CollectorEvent is the unified event type emitted by all collectors
// This replaces RawEvent to avoid multiple parsers and provide type safety
type CollectorEvent struct {
	// Core event identification
	EventID   string    `json:"event_id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"` // collector name: "kernel", "storage-io", "kube-api", etc.

	// Event classification
	Type     CollectorEventType `json:"type"`
	Severity EventSeverity      `json:"severity"`

	// Structured data based on collector type
	EventData EventDataContainer `json:"event_data"`

	// Metadata for correlation and enrichment
	Metadata EventMetadata `json:"metadata"`
}

// CollectorEventType defines the type of event
type CollectorEventType string

const (
	// Kernel events
	EventTypeKernelProcess CollectorEventType = "kernel.process"
	EventTypeKernelNetwork CollectorEventType = "kernel.network"
	EventTypeKernelCgroup  CollectorEventType = "kernel.cgroup"
	EventTypeKernelSyscall CollectorEventType = "kernel.syscall"

	// Storage I/O events
	EventTypeStorageIORead  CollectorEventType = "storage.io.read"
	EventTypeStorageIOWrite CollectorEventType = "storage.io.write"
	EventTypeStorageIOFsync CollectorEventType = "storage.io.fsync"
	EventTypeStorageIOSlow  CollectorEventType = "storage.io.slow"

	// Kubernetes API events
	EventTypeK8sPod        CollectorEventType = "k8s.pod"
	EventTypeK8sDeployment CollectorEventType = "k8s.deployment"
	EventTypeK8sService    CollectorEventType = "k8s.service"
	EventTypeK8sNode       CollectorEventType = "k8s.node"
	EventTypeK8sEvent      CollectorEventType = "k8s.event"

	// DNS events
	EventTypeDNSQuery    CollectorEventType = "dns.query"
	EventTypeDNSResponse CollectorEventType = "dns.response"
	EventTypeDNSTimeout  CollectorEventType = "dns.timeout"

	// CRI events
	EventTypeCRIContainer CollectorEventType = "cri.container"
	EventTypeCRIPod       CollectorEventType = "cri.pod"
	EventTypeCRIImage     CollectorEventType = "cri.image"

	// ETCD events
	EventTypeETCDOperation CollectorEventType = "etcd.operation"
	EventTypeETCDWatch     CollectorEventType = "etcd.watch"

	// Systemd events
	EventTypeSystemdService CollectorEventType = "systemd.service"
	EventTypeSystemdJournal CollectorEventType = "systemd.journal"
)

// EventSeverity is already defined in types.go

// EventDataContainer holds the actual event data
// Only one field should be populated based on the event source
type EventDataContainer struct {
	// Kernel collector data
	Kernel *KernelData `json:"kernel,omitempty"`

	// Storage I/O collector data
	StorageIO *StorageIOData `json:"storage_io,omitempty"`

	// Kubernetes API collector data
	K8sResource *K8sResourceData `json:"k8s_resource,omitempty"`

	// DNS collector data
	DNS *DNSData `json:"dns,omitempty"`

	// CRI collector data
	CRI *CRIData `json:"cri,omitempty"`

	// ETCD collector data
	ETCD *ETCDData `json:"etcd,omitempty"`

	// Systemd collector data
	Systemd *SystemdData `json:"systemd,omitempty"`

	// Generic data for custom collectors (string key-value pairs only)
	Custom map[string]string `json:"custom,omitempty"`
}

// EventMetadata contains metadata for correlation and enrichment
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

	// Additional labels
	Labels map[string]string `json:"labels,omitempty"`

	// Correlation hints
	CorrelationHints []string `json:"correlation_hints,omitempty"`
}

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

	// Owner references
	OwnerKind string `json:"owner_kind,omitempty"`
	OwnerName string `json:"owner_name,omitempty"`
	OwnerUID  string `json:"owner_uid,omitempty"`

	// Status info
	Phase      string   `json:"phase,omitempty"`
	Conditions []string `json:"conditions,omitempty"`
	Message    string   `json:"message,omitempty"`
	Reason     string   `json:"reason,omitempty"`

	// Labels and annotations
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`

	// Raw object (for complex processing)
	Object interface{} `json:"object,omitempty"`
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

// Helper methods for type-safe access

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
