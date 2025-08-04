package ebpf

import "time"

// NetworkInfo represents network connection information
type NetworkInfo struct {
	SAddr     uint32 // Source IP (IPv4)
	DAddr     uint32 // Destination IP (IPv4)
	SPort     uint16 // Source port
	DPort     uint16 // Destination port
	Protocol  uint8  // IPPROTO_TCP or IPPROTO_UDP
	State     uint8  // Connection state
	Direction uint8  // 0=outgoing, 1=incoming
	_         uint8  // Padding
}

// KernelEvent represents a kernel event from eBPF
type KernelEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	EventType uint32
	Size      uint64
	Comm      [16]byte
	CgroupID  uint64   // Add cgroup ID for pod correlation
	PodUID    [36]byte // Add pod UID
	Data      [64]byte // Can contain NetworkInfo for network events
}

// PodInfo represents pod information for correlation
type PodInfo struct {
	PodUID    [36]byte
	Namespace [64]byte
	PodName   [128]byte
	CreatedAt uint64
}

// ContainerInfo represents container information for PID correlation
type ContainerInfo struct {
	ContainerID [64]byte  // Docker/containerd ID
	PodUID      [36]byte  // Associated pod
	Image       [128]byte // Container image
	StartedAt   uint64    // Container start time
}

// ServiceEndpoint represents K8s service endpoint information
type ServiceEndpoint struct {
	ServiceName [64]byte // K8s service name
	Namespace   [64]byte // K8s namespace
	ClusterIP   [16]byte // Service cluster IP
	Port        uint16   // Service port
	_           [2]byte  // Padding
}

// FileInfo represents file operation information
type FileInfo struct {
	Filename [56]byte // File path (truncated)
	Flags    uint32   // Open flags
	Mode     uint32   // File mode
}

// MountInfo represents ConfigMap/Secret mount information
type MountInfo struct {
	Name      [64]byte  // ConfigMap/Secret name
	Namespace [64]byte  // K8s namespace
	MountPath [128]byte // Mount path in container
	IsSecret  uint8     // 1 if secret, 0 if configmap
	_         [7]byte   // Padding
}

// CollectorStats tracks collector statistics
type CollectorStats struct {
	EventsCollected uint64
	EventsDropped   uint64
	ErrorCount      uint64
	LastEventTime   time.Time
}

// DNSQueryInfo represents DNS query information for service discovery
type DNSQueryInfo struct {
	ServiceName [64]byte // K8s service name from DNS
	Namespace   [64]byte // K8s namespace
	ResolvedIP  uint32   // Resolved IP address
	Port        uint16   // Service port
	_           [2]byte  // Padding
}

// VolumeInfo represents PVC mount information
type VolumeInfo struct {
	PVCName   [64]byte  // PersistentVolumeClaim name
	Namespace [64]byte  // K8s namespace
	MountPath [128]byte // Mount path in container
	VolumeID  [64]byte  // Cloud volume ID (e.g., AWS EBS vol-xxx)
}

// ProcessLineage represents process parent-child relationships
type ProcessLineage struct {
	PID       uint32   // Process ID
	PPID      uint32   // Parent process ID
	TGID      uint32   // Thread group ID
	StartTime uint64   // Process start time
	JobName   [64]byte // K8s Job/CronJob name if applicable
}

// Event types
const (
	EventTypeProcess uint32 = iota
	EventTypeFile
	EventTypeNetwork
	EventTypeContainer
	EventTypeMount
)
