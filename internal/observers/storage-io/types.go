package storageio

import "time"

// StorageEventType represents the type of storage I/O event
type StorageEventType uint8

const (
	StorageEventRead        StorageEventType = 1
	StorageEventWrite       StorageEventType = 2
	StorageEventFsync       StorageEventType = 3
	StorageEventOpen        StorageEventType = 4
	StorageEventClose       StorageEventType = 5
	StorageEventBlockIO     StorageEventType = 6
	StorageEventAIOSubmit   StorageEventType = 7
	StorageEventAIOComplete StorageEventType = 8
)

// String returns string representation of event type
func (t StorageEventType) String() string {
	switch t {
	case StorageEventRead:
		return "READ"
	case StorageEventWrite:
		return "WRITE"
	case StorageEventFsync:
		return "FSYNC"
	case StorageEventOpen:
		return "OPEN"
	case StorageEventClose:
		return "CLOSE"
	case StorageEventBlockIO:
		return "BLOCK_IO"
	case StorageEventAIOSubmit:
		return "AIO_SUBMIT"
	case StorageEventAIOComplete:
		return "AIO_COMPLETE"
	default:
		return "UNKNOWN"
	}
}

// StorageEvent represents a storage I/O event from eBPF
type StorageEvent struct {
	Timestamp uint64           `json:"timestamp"`
	PID       uint32           `json:"pid"`
	TID       uint32           `json:"tid"`
	UID       uint32           `json:"uid"`
	GID       uint32           `json:"gid"`
	CgroupID  uint64           `json:"cgroup_id"`
	EventType StorageEventType `json:"event_type"`
	Pad       [3]uint8         `json:"-"`

	// I/O details
	Inode     uint64 `json:"inode"`
	Offset    uint64 `json:"offset"`
	Size      uint64 `json:"size"`
	LatencyNs uint64 `json:"latency_ns"`
	ErrorCode int32  `json:"error_code"`

	// File info
	Flags    uint32 `json:"flags"`
	Mode     uint32 `json:"mode"`
	FileSize uint64 `json:"file_size"`

	// Block layer details
	Major      uint32 `json:"major"`
	Minor      uint32 `json:"minor"`
	Sector     uint64 `json:"sector"`
	QueueDepth uint32 `json:"queue_depth"`
	BioFlags   uint32 `json:"bio_flags"`

	// Async I/O details
	AIOCtxID    uint64 `json:"aio_ctx_id"`
	AIONrEvents uint32 `json:"aio_nr_events"`
	AIOFlags    uint32 `json:"aio_flags"`

	// Process info
	Comm     [16]byte  `json:"-"`
	FullPath [256]byte `json:"-"`
}

// GetComm returns the command name as string
func (e *StorageEvent) GetComm() string {
	return string(e.Comm[:cStringLen(e.Comm[:])])
}

// GetFullPath returns the full path as string
func (e *StorageEvent) GetFullPath() string {
	return string(e.FullPath[:cStringLen(e.FullPath[:])])
}

// GetLatencyMs returns latency in milliseconds
func (e *StorageEvent) GetLatencyMs() float64 {
	return float64(e.LatencyNs) / 1_000_000.0
}

// IsError returns true if the operation resulted in an error
func (e *StorageEvent) IsError() bool {
	return e.ErrorCode < 0
}

// IsSlow returns true if the operation was slow (>100ms by default)
func (e *StorageEvent) IsSlow(thresholdMs float64) bool {
	return e.GetLatencyMs() > thresholdMs
}

// MountInfo represents Kubernetes mount point information
type MountInfo struct {
	Path         string
	Device       string
	FSType       string
	VolumeType   string
	PodUID       string
	PodName      string
	Namespace    string
	PVCName      string
	StorageClass string
	LastSeen     time.Time
}

// ContainerInfo represents container metadata
type ContainerInfo struct {
	ContainerID string
	PodName     string
	Namespace   string
	CgroupPath  string
}

// SlowIOEvent represents aggregated slow I/O information
type SlowIOEvent struct {
	FirstSeen    time.Time
	LastSeen     time.Time
	Count        int
	TotalLatency time.Duration
	MaxLatency   time.Duration
	Path         string
	Operation    string
}

// RuntimeEnvironment represents runtime environment detection
type RuntimeEnvironment struct {
	IsKubernetes       bool
	IsDocker           bool
	IsContainerd       bool
	IsCRI              bool
	HasEBPF            bool
	KernelVersion      string
	VolumePathPatterns map[string]string
}

// GetMonitoredPaths returns paths to monitor based on runtime
func (r *RuntimeEnvironment) GetMonitoredPaths() []string {
	var paths []string
	for path := range r.VolumePathPatterns {
		paths = append(paths, path)
	}
	return paths
}

// IOStats represents I/O statistics from eBPF
type IOStats struct {
	TotalReads    uint64
	TotalWrites   uint64
	TotalFsyncs   uint64
	SlowIOs       uint64
	Errors        uint64
	EventsDropped uint64
}

// Helper function to find C string length
func cStringLen(b []byte) int {
	for i, v := range b {
		if v == 0 {
			return i
		}
	}
	return len(b)
}
