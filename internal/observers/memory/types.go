package memory

import (
	"fmt"
)

// EventType represents memory event types
type EventType uint32

const (
	EventTypeMmap         EventType = 1 // Large allocation via mmap
	EventTypeMunmap       EventType = 2 // Memory freed
	EventTypeRSSGrowth    EventType = 3 // RSS increase detected
	EventTypeUnfreed      EventType = 4 // Long-lived allocation
	EventTypeStackTrace   EventType = 5 // Stack trace captured
	EventTypeMalloc       EventType = 6 // malloc() allocation
	EventTypeFree         EventType = 7 // free() deallocation
	EventTypeLeakDetected EventType = 8 // Memory leak detected
)

// String returns string representation of event type
func (e EventType) String() string {
	switch e {
	case EventTypeMmap:
		return "mmap"
	case EventTypeMunmap:
		return "munmap"
	case EventTypeRSSGrowth:
		return "rss_growth"
	case EventTypeUnfreed:
		return "unfreed"
	case EventTypeStackTrace:
		return "stack_trace"
	case EventTypeMalloc:
		return "malloc"
	case EventTypeFree:
		return "free"
	case EventTypeLeakDetected:
		return "leak_detected"
	default:
		return fmt.Sprintf("unknown_%d", e)
	}
}

// StackTrace represents a captured stack trace
type StackTrace struct {
	StackID   int32    `json:"stack_id"`
	Addresses []uint64 `json:"addresses"`
	Symbols   []string `json:"symbols,omitempty"`
	Resolve   bool     `json:"resolve"`
}

// KubernetesMetadata contains Kubernetes context information
type KubernetesMetadata struct {
	PodName       string            `json:"pod_name,omitempty"`
	PodNamespace  string            `json:"pod_namespace,omitempty"`
	PodUID        string            `json:"pod_uid,omitempty"`
	ContainerID   string            `json:"container_id,omitempty"`
	ContainerName string            `json:"container_name,omitempty"`
	NodeName      string            `json:"node_name,omitempty"`
	ServiceName   string            `json:"service_name,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	WorkloadKind  string            `json:"workload_kind,omitempty"` // Deployment, StatefulSet, etc.
	WorkloadName  string            `json:"workload_name,omitempty"`
}

// MemoryEvent represents a memory allocation/deallocation event from eBPF
type MemoryEvent struct {
	Timestamp       uint64              `json:"timestamp"`
	EventType       EventType           `json:"event_type"`
	PID             uint32              `json:"pid"`
	TID             uint32              `json:"tid"`
	UID             uint32              `json:"uid"`
	GID             uint32              `json:"gid"`
	Address         uint64              `json:"address"`
	Size            uint64              `json:"size"`
	CGroupID        uint64              `json:"cgroup_id"`
	Comm            [16]byte            `json:"comm"`
	StackTrace      *StackTrace         `json:"stack_trace,omitempty"`
	AllocationAgeNs uint64              `json:"allocation_age_ns"`
	AllocationCount uint32              `json:"allocation_count"`
	Flags           uint8               `json:"flags"`
	IsLeak          bool                `json:"is_leak"`
	Kubernetes      *KubernetesMetadata `json:"kubernetes,omitempty"`
	// RSS tracking (for RSS events)
	RSSPages  uint64 `json:"rss_pages,omitempty"`
	RSSGrowth int64  `json:"rss_growth,omitempty"`
}

// AllocationInfo tracks active allocations
type AllocationInfo struct {
	Size       uint64              `json:"size"`
	Timestamp  uint64              `json:"timestamp"`
	PID        uint32              `json:"pid"`
	TID        uint32              `json:"tid"`
	CGroupID   uint64              `json:"cgroup_id"`
	Comm       [16]byte            `json:"comm"`
	StackTrace *StackTrace         `json:"stack_trace,omitempty"`
	Kubernetes *KubernetesMetadata `json:"kubernetes,omitempty"`
	Flags      uint8               `json:"flags"`
}

// LeakCandidate represents a potential memory leak
type LeakCandidate struct {
	Address         uint64              `json:"address"`
	Size            uint64              `json:"size"`
	Age             uint64              `json:"age_ns"`
	PID             uint32              `json:"pid"`
	AllocationCount uint32              `json:"allocation_count"`
	StackTrace      *StackTrace         `json:"stack_trace,omitempty"`
	GrowthRate      float64             `json:"growth_rate_bytes_per_sec"`
	ProcessName     string              `json:"process_name"`
	Kubernetes      *KubernetesMetadata `json:"kubernetes,omitempty"`
	Severity        string              `json:"severity"`
	Confidence      float64             `json:"confidence"`
}

// ProcessAllocationStats tracks per-process allocation statistics
type ProcessAllocationStats struct {
	PID                uint32              `json:"pid"`
	ProcessName        string              `json:"process_name"`
	TotalAllocated     uint64              `json:"total_allocated"`
	TotalFreed         uint64              `json:"total_freed"`
	CurrentAllocations uint64              `json:"current_allocations"`
	PeakAllocations    uint64              `json:"peak_allocations"`
	AllocationCount    uint64              `json:"allocation_count"`
	DeallocationCount  uint64              `json:"deallocation_count"`
	LastAllocationNs   uint64              `json:"last_allocation_ns"`
	LeakCount          uint32              `json:"leak_count"`
	Kubernetes         *KubernetesMetadata `json:"kubernetes,omitempty"`
}

// CollectorStats tracks memory collector metrics
type CollectorStats struct {
	EventsGenerated      uint64                             `json:"events_generated"`
	EventsDropped        uint64                             `json:"events_dropped"`
	AllocationsTracked   uint64                             `json:"allocations_tracked"`
	DeallocationsTracked uint64                             `json:"deallocations_tracked"`
	RSSGrowthEvents      uint64                             `json:"rss_growth_events"`
	UnfreedAllocations   uint64                             `json:"unfreed_allocations"`
	LargestAllocation    uint64                             `json:"largest_allocation"`
	TotalUnfreedBytes    uint64                             `json:"total_unfreed_bytes"`
	LeakDetections       uint64                             `json:"leak_detections"`
	StackTracesCollected uint64                             `json:"stack_traces_collected"`
	ProcessStats         map[uint32]*ProcessAllocationStats `json:"process_stats,omitempty"`
	K8sEnrichmentRate    float64                            `json:"k8s_enrichment_rate"`
}

// ProcessMemoryInfo contains memory statistics for a process
type ProcessMemoryInfo struct {
	PID               uint32              `json:"pid"`
	ProcessName       string              `json:"process_name"`
	RSSPages          uint64              `json:"rss_pages"`
	RSSBytes          uint64              `json:"rss_bytes"`
	ActiveAllocations uint32              `json:"active_allocations"`
	TotalAllocated    uint64              `json:"total_allocated"`
	TotalFreed        uint64              `json:"total_freed"`
	NetAllocated      int64               `json:"net_allocated"` // allocated - freed
	CGroupID          uint64              `json:"cgroup_id"`
	Kubernetes        *KubernetesMetadata `json:"kubernetes,omitempty"`
	LeakCandidates    []LeakCandidate     `json:"leak_candidates,omitempty"`
	MemoryGrowthRate  float64             `json:"memory_growth_rate_bytes_per_sec"`
	LastUpdate        uint64              `json:"last_update_ns"`
}

// MemoryLeakReport represents a comprehensive memory leak analysis
type MemoryLeakReport struct {
	Timestamp          uint64              `json:"timestamp"`
	ReportID           string              `json:"report_id"`
	PID                uint32              `json:"pid"`
	ProcessName        string              `json:"process_name"`
	TotalLeakBytes     uint64              `json:"total_leak_bytes"`
	LeakCount          uint32              `json:"leak_count"`
	ConfidenceScore    float64             `json:"confidence_score"`
	SeverityLevel      string              `json:"severity_level"`
	LeakCandidates     []LeakCandidate     `json:"leak_candidates"`
	StackTraces        []StackTrace        `json:"stack_traces"`
	Kubernetes         *KubernetesMetadata `json:"kubernetes,omitempty"`
	Recommendations    []string            `json:"recommendations,omitempty"`
	AffectedContainers []string            `json:"affected_containers,omitempty"`
}
