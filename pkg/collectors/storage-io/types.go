//go:build linux

package storageio

import (
	"time"
)

// StorageIOEvent represents a raw storage I/O event from eBPF
type StorageIOEvent struct {
	// Core operation details
	Operation string    `json:"operation"` // read, write, fsync, iterate_dir
	Path      string    `json:"path"`      // file/directory path
	Timestamp time.Time `json:"timestamp"` // event timestamp

	// I/O metrics
	Size     int64         `json:"size"`             // bytes read/written
	Offset   int64         `json:"offset,omitempty"` // file offset
	Duration time.Duration `json:"duration"`         // operation latency

	// Performance classification
	SlowIO    bool `json:"slow_io"`    // >10ms threshold
	BlockedIO bool `json:"blocked_io"` // blocked operation

	// File system details
	Device     string `json:"device"`      // block device
	Inode      uint64 `json:"inode"`       // inode number
	FileSystem string `json:"filesystem"`  // fs type (ext4, xfs, etc.)
	MountPoint string `json:"mount_point"` // mount point

	// Kubernetes correlation
	K8sPath       string `json:"k8s_path,omitempty"`        // K8s-specific path type
	K8sVolumeType string `json:"k8s_volume_type,omitempty"` // pvc, configmap, secret, hostpath
	PVCName       string `json:"pvc_name,omitempty"`        // Human-readable PVC name
	StorageClass  string `json:"storage_class,omitempty"`   // Storage class (gp3, pd-ssd, etc.)
	ContainerID   string `json:"container_id,omitempty"`    // container correlation
	PodUID        string `json:"pod_uid,omitempty"`         // pod correlation

	// Process context
	PID      int32  `json:"pid"`                 // process ID
	PPID     int32  `json:"ppid"`                // parent process ID
	UID      int32  `json:"uid"`                 // user ID
	GID      int32  `json:"gid"`                 // group ID
	Command  string `json:"command"`             // process command
	CgroupID uint64 `json:"cgroup_id,omitempty"` // cgroup correlation

	// Error information
	ErrorCode    int32  `json:"error_code,omitempty"`    // syscall error code
	ErrorMessage string `json:"error_message,omitempty"` // error description

	// VFS operation details
	VFSLayer string `json:"vfs_layer"`       // vfs_read, vfs_write, vfs_fsync
	Flags    uint32 `json:"flags,omitempty"` // operation flags
	Mode     uint32 `json:"mode,omitempty"`  // file mode

	// Performance impact
	CPUTime   time.Duration `json:"cpu_time,omitempty"`   // CPU time consumed
	QueueTime time.Duration `json:"queue_time,omitempty"` // time in I/O queue
	BlockTime time.Duration `json:"block_time,omitempty"` // time blocked
}

// MountInfo represents a Kubernetes mount point
type MountInfo struct {
	Path          string `json:"path"`            // mount path
	Device        string `json:"device"`          // block device
	Type          string `json:"type"`            // filesystem type
	Options       string `json:"options"`         // mount options
	K8sVolumeType string `json:"k8s_volume_type"` // pvc, configmap, secret, hostpath
	PodUID        string `json:"pod_uid"`         // associated pod UID
	VolumeName    string `json:"volume_name"`     // volume name
	ReadOnly      bool   `json:"read_only"`       // read-only mount
}

// ContainerInfo represents container correlation information
type ContainerInfo struct {
	ContainerID string    `json:"container_id"`
	PodUID      string    `json:"pod_uid"`
	PodName     string    `json:"pod_name"`
	Namespace   string    `json:"namespace"`
	Image       string    `json:"image"`
	CgroupID    uint64    `json:"cgroup_id"`
	LastSeen    time.Time `json:"last_seen"`
}

// SlowIOEvent represents a slow I/O operation for tracking
type SlowIOEvent struct {
	Operation string        `json:"operation"`
	Path      string        `json:"path"`
	PID       int32         `json:"pid"`
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp"`
}

// VFSProbeType represents the type of VFS probe
type VFSProbeType uint8

const (
	VFSProbeRead       VFSProbeType = 1
	VFSProbeWrite      VFSProbeType = 2
	VFSProbeFsync      VFSProbeType = 3
	VFSProbeIterateDir VFSProbeType = 4
	VFSProbeOpen       VFSProbeType = 5
	VFSProbeClose      VFSProbeType = 6
)

// String returns the string representation of VFSProbeType
func (v VFSProbeType) String() string {
	switch v {
	case VFSProbeRead:
		return "vfs_read"
	case VFSProbeWrite:
		return "vfs_write"
	case VFSProbeFsync:
		return "vfs_fsync"
	case VFSProbeIterateDir:
		return "vfs_iterate_dir"
	case VFSProbeOpen:
		return "vfs_open"
	case VFSProbeClose:
		return "vfs_close"
	default:
		return "vfs_unknown"
	}
}

// K8sVolumeType represents Kubernetes volume types we monitor
type K8sVolumeType string

const (
	K8sVolumePVC         K8sVolumeType = "pvc"
	K8sVolumeConfigMap   K8sVolumeType = "configmap"
	K8sVolumeSecret      K8sVolumeType = "secret"
	K8sVolumeHostPath    K8sVolumeType = "hostpath"
	K8sVolumeEmptyDir    K8sVolumeType = "emptydir"
	K8sVolumeProjected   K8sVolumeType = "projected"
	K8sVolumeDownwardAPI K8sVolumeType = "downwardapi"
)

// OperationType represents the type of storage operation
type OperationType string

const (
	OperationRead       OperationType = "read"
	OperationWrite      OperationType = "write"
	OperationFsync      OperationType = "fsync"
	OperationIterateDir OperationType = "iterate_dir"
	OperationOpen       OperationType = "open"
	OperationClose      OperationType = "close"
)

// IOClassification represents the performance classification of an I/O operation
type IOClassification struct {
	IsSlowIO       bool   `json:"is_slow_io"`
	IsBlockedIO    bool   `json:"is_blocked_io"`
	IsK8sVolume    bool   `json:"is_k8s_volume"`
	IsCriticalPath bool   `json:"is_critical_path"`
	LatencyClass   string `json:"latency_class"` // fast, normal, slow, critical
	SizeClass      string `json:"size_class"`    // small, medium, large, huge
}

// ClassifyIO classifies an I/O operation based on performance characteristics
func ClassifyIO(event *StorageIOEvent, slowThresholdMs int) IOClassification {
	classification := IOClassification{
		IsK8sVolume: event.K8sVolumeType != "",
	}

	// Latency classification
	latencyMs := float64(event.Duration.Nanoseconds()) / 1e6
	if latencyMs > float64(slowThresholdMs) {
		classification.IsSlowIO = true
		if latencyMs > 100 {
			classification.LatencyClass = "critical"
		} else {
			classification.LatencyClass = "slow"
		}
	} else if latencyMs > 1 {
		classification.LatencyClass = "normal"
	} else {
		classification.LatencyClass = "fast"
	}

	// Size classification
	if event.Size > 1024*1024 { // 1MB
		classification.SizeClass = "huge"
	} else if event.Size > 64*1024 { // 64KB
		classification.SizeClass = "large"
	} else if event.Size > 4096 { // 4KB
		classification.SizeClass = "medium"
	} else {
		classification.SizeClass = "small"
	}

	// Critical path detection
	classification.IsCriticalPath = isK8sCriticalPath(event.Path) ||
		event.K8sVolumeType == string(K8sVolumeConfigMap) ||
		event.K8sVolumeType == string(K8sVolumeSecret)

	classification.IsBlockedIO = event.BlockedIO

	return classification
}

// isK8sCriticalPath determines if a path is part of Kubernetes critical paths
func isK8sCriticalPath(path string) bool {
	criticalPaths := []string{
		"/var/lib/kubelet/pods/",
		"/etc/kubernetes/",
		"/var/lib/etcd/",
		"/var/log/containers/",
		"/var/lib/docker/containers/",
		"/var/lib/containerd/",
	}

	for _, criticalPath := range criticalPaths {
		if len(path) >= len(criticalPath) && path[:len(criticalPath)] == criticalPath {
			return true
		}
	}

	return false
}

// PerformanceMetrics represents aggregated performance metrics for I/O operations
type PerformanceMetrics struct {
	TotalOperations     int64         `json:"total_operations"`
	SlowOperations      int64         `json:"slow_operations"`
	BlockedOperations   int64         `json:"blocked_operations"`
	ErrorOperations     int64         `json:"error_operations"`
	AverageLatency      time.Duration `json:"average_latency"`
	MaxLatency          time.Duration `json:"max_latency"`
	TotalBytes          int64         `json:"total_bytes"`
	OperationsPerSecond float64       `json:"operations_per_second"`
	ThroughputBPS       float64       `json:"throughput_bps"`
}

// IOPattern represents detected I/O patterns
type IOPattern struct {
	PatternType     string        `json:"pattern_type"`      // sequential, random, mixed
	Confidence      float64       `json:"confidence"`        // 0.0 to 1.0
	WindowDuration  time.Duration `json:"window_duration"`   // analysis window
	SampleCount     int           `json:"sample_count"`      // number of samples
	ReadWriteRatio  float64       `json:"read_write_ratio"`  // ratio of reads to writes
	AverageIOSize   int64         `json:"average_io_size"`   // average I/O size
	IOSizeVariation float64       `json:"io_size_variation"` // coefficient of variation
}

// eBPF Event Structure (matches C struct in eBPF program)
// This must be kept in sync with the C struct in storage_monitor.c
type StorageIOEventRaw struct {
	// Event header
	EventType uint8 // VFSProbeType
	PID       uint32
	PPID      uint32
	UID       uint32
	GID       uint32
	CgroupID  uint64

	// Timing information
	StartTimeNs uint64 // ktime_get_ns() at start
	EndTimeNs   uint64 // ktime_get_ns() at end

	// File operation details
	Inode  uint64
	Size   int64
	Offset int64
	Flags  uint32
	Mode   uint32

	// Error information
	ErrorCode int32

	// Path and command (fixed size for eBPF)
	Path [256]byte
	Comm [16]byte

	// Device information
	DevMajor uint32
	DevMinor uint32
} // Note: This struct must be packed and aligned for eBPF compatibility

// Constants for eBPF program configuration
const (
	MaxPathLength = 256
	MaxCommLength = 16

	// Map sizes for eBPF
	MaxActiveEvents = 10240
	MaxSlowEvents   = 1024
	MaxMountPoints  = 512

	// Event filtering constants
	MinIOSize = 4096 // Minimum I/O size to track (4KB)

	// Performance thresholds
	DefaultSlowIOThresholdMs = 10
	CriticalIOThresholdMs    = 100

	// Sampling rates
	DefaultSamplingRate = 0.1 // 10% sampling for non-critical paths
	K8sSamplingRate     = 1.0 // 100% sampling for K8s paths
	SlowIOSamplingRate  = 1.0 // 100% sampling for slow I/O
)

// Error codes for storage I/O monitoring
const (
	ErrEBPFProgramLoad   = "failed to load eBPF program"
	ErrEBPFProgramAttach = "failed to attach eBPF program"
	ErrEventParsing      = "failed to parse storage I/O event"
	ErrK8sEnrichment     = "failed to enrich with K8s context"
	ErrMountDiscovery    = "failed to discover mount points"
	ErrCgroupResolution  = "failed to resolve cgroup information"
)
