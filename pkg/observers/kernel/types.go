package kernel

// Event type constants - must match C definitions
const (
	EventTypeConfigMapAccess    = uint8(1) // ConfigMap access events
	EventTypeSecretAccess       = uint8(2) // Secret access events
	EventTypePodSyscall         = uint8(3) // Pod syscall events for correlation
	EventTypeConfigAccessFailed = uint8(4) // Failed config access with errno

	// Legacy event types - kept for test compatibility only
	// These are no longer monitored but tests still reference them
	EventTypeProcess = uint32(10) // Deprecated - use syscall-errors observer
	EventTypeFile    = uint32(11) // Deprecated - covered by ConfigMap/Secret access
	EventTypeNetwork = uint32(12) // Deprecated - use network observer
)

// KernelEvent represents a kernel event from eBPF - must match C struct kernel_event
type KernelEvent struct {
	Timestamp uint64   // Event timestamp
	PID       uint32   // Process ID
	TID       uint32   // Thread ID
	EventType uint32   // Event type (ConfigMap/Secret access, etc.)
	Size      uint64   // Size field (unused but kept for alignment)
	Comm      [16]byte // Process command
	CgroupID  uint64   // Cgroup ID for pod correlation
	PodUID    [36]byte // Pod UID
	// Union data - either raw bytes or structured config info
	Data [64]byte // Raw data or config_info struct
}

// ConfigInfo represents ConfigMap/Secret access information
type ConfigInfo struct {
	MountPath [60]byte // Mount path in container (reduced to fit error_code)
	ErrorCode int32    // Error code for failed access (0 = success, positive = errno)
}

// KernelEventData represents processed kernel event data for domain layer
type KernelEventData struct {
	PID        uint32 `json:"pid"`
	TID        uint32 `json:"tid"`
	CgroupID   uint64 `json:"cgroup_id"`
	EventType  uint32 `json:"event_type"`
	Comm       string `json:"comm"`
	PodUID     string `json:"pod_uid,omitempty"`
	MountPath  string `json:"mount_path,omitempty"`
	ConfigType string `json:"config_type,omitempty"` // "configmap", "secret", or "failed"
	ConfigName string `json:"config_name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	ErrorCode  int32  `json:"error_code,omitempty"` // Error code for failed access (ENOENT, EACCES, etc.)
	ErrorDesc  string `json:"error_desc,omitempty"` // Human-readable error description
}

// PodInfo represents pod information for correlation
type PodInfo struct {
	PodUID    string `json:"pod_uid"`
	Namespace string `json:"namespace"`
	PodName   string `json:"pod_name"`
	CreatedAt int64  `json:"created_at"`
}

// ContainerInfo represents container information for PID correlation
type ContainerInfo struct {
	ContainerID string `json:"container_id"`
	PodUID      string `json:"pod_uid"`
	Image       string `json:"image"`
	StartedAt   int64  `json:"started_at"`
}

// ServiceEndpoint represents service endpoint information
type ServiceEndpoint struct {
	ServiceName string `json:"service_name"`
	Namespace   string `json:"namespace"`
	ClusterIP   string `json:"cluster_ip"`
	Port        uint16 `json:"port"`
}

// MountInfo represents ConfigMap/Secret mount information
type MountInfo struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	MountPath string `json:"mount_path"`
	IsSecret  bool   `json:"is_secret"`
}

// VolumeInfo represents PVC volume mount information
type VolumeInfo struct {
	PVCName   string `json:"pvc_name"`
	Namespace string `json:"namespace"`
	MountPath string `json:"mount_path"`
	VolumeID  string `json:"volume_id"`
}

// ProcessLineage represents process parent-child relationships
type ProcessLineage struct {
	PID       uint32 `json:"pid"`
	PPID      uint32 `json:"ppid"`
	TGID      uint32 `json:"tgid"`
	StartTime uint64 `json:"start_time"`
	JobName   string `json:"job_name,omitempty"`
}
