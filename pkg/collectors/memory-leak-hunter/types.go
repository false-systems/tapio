package memory_leak_hunter

import "fmt"

// EventType represents memory event types
type EventType uint32

const (
	EventTypeMmap      EventType = 1 // Large allocation via mmap
	EventTypeMunmap    EventType = 2 // Memory freed
	EventTypeRSSGrowth EventType = 3 // RSS increase detected
	EventTypeUnfreed   EventType = 4 // Long-lived allocation
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
	default:
		return fmt.Sprintf("unknown_%d", e)
	}
}

// MemoryEvent represents a memory allocation/deallocation event from eBPF
type MemoryEvent struct {
	Timestamp uint64    `json:"timestamp"`
	EventType EventType `json:"event_type"`
	PID       uint32    `json:"pid"`
	Address   uint64    `json:"address"`
	Size      uint64    `json:"size"`
	CGroupID  uint64    `json:"cgroup_id"`
	Comm      [16]byte  `json:"comm"`
	CallerIP  uint64    `json:"caller_ip"`
	// RSS tracking
	RSSPages  uint64 `json:"rss_pages"`
	RSSGrowth uint64 `json:"rss_growth"`
}

// AllocationInfo tracks active allocations
type AllocationInfo struct {
	Size      uint64   `json:"size"`
	Timestamp uint64   `json:"timestamp"`
	PID       uint32   `json:"pid"`
	TID       uint32   `json:"tid"`
	CGroupID  uint64   `json:"cgroup_id"`
	Comm      [16]byte `json:"comm"`
	CallerIP  uint64   `json:"caller_ip"`
}

// LeakCandidate represents a potential memory leak
type LeakCandidate struct {
	Address         uint64  `json:"address"`
	Size            uint64  `json:"size"`
	Age             uint64  `json:"age_ns"`
	PID             uint32  `json:"pid"`
	AllocationCount uint32  `json:"allocation_count"`
	StackID         int64   `json:"stack_id"`
	GrowthRate      float64 `json:"growth_rate_bytes_per_sec"`
	Container       string  `json:"container,omitempty"`
	ProcessName     string  `json:"process_name"`
}

// CollectorStats tracks memory collector metrics
type CollectorStats struct {
	EventsGenerated      uint64 `json:"events_generated"`
	EventsDropped        uint64 `json:"events_dropped"`
	AllocationsTracked   uint64 `json:"allocations_tracked"`
	DeallocationsTracked uint64 `json:"deallocations_tracked"`
	RSSGrowthEvents      uint64 `json:"rss_growth_events"`
	UnfreedAllocations   uint64 `json:"unfreed_allocations"`
	LargestAllocation    uint64 `json:"largest_allocation"`
	TotalUnfreedBytes    uint64 `json:"total_unfreed_bytes"`
}

// ProcessMemoryInfo contains memory statistics for a process
type ProcessMemoryInfo struct {
	PID               uint32 `json:"pid"`
	ProcessName       string `json:"process_name"`
	RSSPages          uint64 `json:"rss_pages"`
	RSSBytes          uint64 `json:"rss_bytes"`
	ActiveAllocations uint32 `json:"active_allocations"`
	TotalAllocated    uint64 `json:"total_allocated"`
	TotalFreed        uint64 `json:"total_freed"`
	NetAllocated      int64  `json:"net_allocated"` // allocated - freed
	Container         string `json:"container,omitempty"`
	CGroupID          uint64 `json:"cgroup_id"`
}
