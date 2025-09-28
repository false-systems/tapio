//go:build linux

package containerruntime

import (
	"fmt"
	"time"
	"unsafe"
)

// BPFContainerExitEvent matches the C struct container_exit_event exactly
// This struct must be kept in sync with the eBPF C code
// WARNING: This struct is packed (no padding) to match kernel ABI
type BPFContainerExitEvent struct {
	Timestamp   uint64   // ns since boot (8 bytes)
	PID         uint32   // Process ID (4 bytes)
	TGID        uint32   // Thread Group ID (4 bytes)
	ExitCode    int32    // Exit code (4 bytes)
	CgroupID    uint64   // Cgroup ID (8 bytes)
	MemoryUsage uint64   // Memory usage in bytes (8 bytes)
	MemoryLimit uint64   // Memory limit in bytes (8 bytes)
	OOMKilled   uint8    // 1 if OOM killed, 0 otherwise (1 byte)
	Comm        [16]byte // Process command name (16 bytes)
	ContainerID [64]byte // Container ID string (64 bytes)
} // Total: 136 bytes (with alignment padding)

// BPFContainerMetadata matches the C struct container_metadata exactly
type BPFContainerMetadata struct {
	ContainerID [64]int8 // Container ID
	PodUID      [36]int8 // Kubernetes Pod UID
	PodName     [64]int8 // Kubernetes Pod name
	Namespace   [64]int8 // Kubernetes namespace
	_           [4]byte  // Padding for alignment
	MemoryLimit uint64   // Memory limit in bytes
	CgroupID    uint64   // Cgroup ID
}

// ContainerMetadata is the Go representation of container metadata
type ContainerMetadata struct {
	ContainerID   string            `json:"container_id"`
	ContainerName string            `json:"container_name,omitempty"`
	ImageName     string            `json:"image_name,omitempty"`
	PodUID        string            `json:"pod_uid,omitempty"`
	PodName       string            `json:"pod_name,omitempty"`
	Namespace     string            `json:"namespace,omitempty"`
	Runtime       string            `json:"runtime,omitempty"`
	MemoryLimit   uint64            `json:"memory_limit"`
	CgroupID      uint64            `json:"cgroup_id"`
	Labels        map[string]string `json:"labels,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	LastSeen      time.Time         `json:"last_seen"`
}

// ContainerExitEvent represents a processed container exit event
type ContainerExitEvent struct {
	Timestamp     time.Time          `json:"timestamp"`
	PID           uint32             `json:"pid"`
	TGID          uint32             `json:"tgid"`
	ExitCode      int32              `json:"exit_code"`
	CgroupID      uint64             `json:"cgroup_id"`
	MemoryUsage   uint64             `json:"memory_usage"`
	MemoryLimit   uint64             `json:"memory_limit"`
	OOMKilled     bool               `json:"oom_killed"`
	Command       string             `json:"command"`
	ContainerID   string             `json:"container_id"`
	ContainerMeta *ContainerMetadata `json:"container_metadata,omitempty"`
}

// EventType represents the type of eBPF event
type EventType uint8

const (
	EventTypeCreated EventType = 0
	EventTypeStarted EventType = 1
	EventTypeStopped EventType = 2
	EventTypeDied    EventType = 3
	EventTypeOOM     EventType = 4
)

// String returns the string representation of EventType
func (et EventType) String() string {
	switch et {
	case EventTypeCreated:
		return "created"
	case EventTypeStarted:
		return "started"
	case EventTypeStopped:
		return "stopped"
	case EventTypeDied:
		return "died"
	case EventTypeOOM:
		return "oom"
	default:
		return "unknown"
	}
}

// Statistics represents eBPF program statistics
type Statistics struct {
	OOMKills        uint64    `json:"oom_kills"`
	ProcessExits    uint64    `json:"process_exits"`
	ContainerStarts uint64    `json:"container_starts"`
	EventsDropped   uint64    `json:"events_dropped"`
	EventsProcessed uint64    `json:"events_processed"`
	LastUpdated     time.Time `json:"last_updated"`
}

// BPF map indices for statistics
const (
	StatOOMKills        = 0
	StatProcessExits    = 1
	StatContainerStarts = 2
	StatEventsDropped   = 3
)

// ContainerState represents the state of a tracked container
type ContainerState struct {
	ContainerID  string            `json:"container_id"`
	PID          uint32            `json:"pid"`
	CgroupID     uint64            `json:"cgroup_id"`
	State        string            `json:"state"`
	MemoryLimit  uint64            `json:"memory_limit"`
	MemoryUsage  uint64            `json:"memory_usage"`
	ProcessCount int               `json:"process_count"`
	Labels       map[string]string `json:"labels,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	LastSeen     time.Time         `json:"last_seen"`
	K8sMetadata  *K8sMetadata      `json:"k8s_metadata,omitempty"`
}

// K8sMetadata represents Kubernetes-specific metadata
type K8sMetadata struct {
	PodUID      string            `json:"pod_uid"`
	PodName     string            `json:"pod_name"`
	Namespace   string            `json:"namespace"`
	NodeName    string            `json:"node_name"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// MemoryPressureEvent represents a memory pressure event
type MemoryPressureEvent struct {
	Timestamp      time.Time `json:"timestamp"`
	CgroupID       uint64    `json:"cgroup_id"`
	ContainerID    string    `json:"container_id"`
	MemoryUsage    uint64    `json:"memory_usage"`
	MemoryLimit    uint64    `json:"memory_limit"`
	UtilizationPct float64   `json:"utilization_percent"`
	Severity       string    `json:"severity"` // low, medium, high, critical
}

// ProcessForkEvent represents a process fork event in a container
type ProcessForkEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	ParentPID   uint32    `json:"parent_pid"`
	ChildPID    uint32    `json:"child_pid"`
	CgroupID    uint64    `json:"cgroup_id"`
	ContainerID string    `json:"container_id"`
	ParentComm  string    `json:"parent_command"`
	ChildComm   string    `json:"child_command"`
}

// Validate validates a BPFContainerExitEvent for size consistency
func ValidateBPFContainerExitEvent() error {
	// The actual struct size includes padding after OOMKilled to align Comm
	// C struct layout (with natural alignment):
	// Timestamp(8) + PID(4) + TGID(4) + ExitCode(4) + padding(4) +
	// CgroupID(8) + MemoryUsage(8) + MemoryLimit(8) +
	// OOMKilled(1) + padding(15) + Comm(16) + ContainerID(64) = 136 bytes
	expectedSize := 136 // Actual size with padding
	actualSize := int(unsafe.Sizeof(BPFContainerExitEvent{}))

	if actualSize != expectedSize {
		return fmt.Errorf("BPFContainerExitEvent size mismatch: expected %d, got %d", expectedSize, actualSize)
	}

	return nil
}

// Validate validates a BPFContainerMetadata for size consistency
func ValidateBPFContainerMetadata() error {
	expectedSize := 64 + 36 + 64 + 64 + 4 + 8 + 8 // Sum of all field sizes with padding
	actualSize := int(unsafe.Sizeof(BPFContainerMetadata{}))

	if actualSize != expectedSize {
		return fmt.Errorf("BPFContainerMetadata size mismatch: expected %d, got %d", expectedSize, actualSize)
	}

	return nil
}

// CStringToGo converts a C string (byte array) to Go string
func CStringToGo(cstr []byte) string {
	// Find the null terminator
	length := 0
	for i, b := range cstr {
		if b == 0 {
			length = i
			break
		}
	}

	if length == 0 {
		return ""
	}

	// Convert to []byte
	goBytes := make([]byte, length)
	for i := 0; i < length; i++ {
		goBytes[i] = byte(cstr[i])
	}

	return string(goBytes)
}

// GoStringToC converts a Go string to C string format (byte array)
func GoStringToC(s string, maxLen int) []int8 {
	cstr := make([]int8, maxLen)

	// Copy string bytes, ensuring we don't exceed maxLen-1 (leave room for null terminator)
	copyLen := len(s)
	if copyLen >= maxLen {
		copyLen = maxLen - 1
	}

	for i := 0; i < copyLen; i++ {
		cstr[i] = int8(s[i])
	}

	// Null terminate
	if copyLen < maxLen {
		cstr[copyLen] = 0
	}

	return cstr
}
