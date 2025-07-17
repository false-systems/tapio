package core

import (
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Config holds the configuration for the eBPF collector
type Config struct {
	// Name identifies this collector instance
	Name string `json:"name" validate:"required"`
	
	// Enabled determines if the collector should be active
	Enabled bool `json:"enabled"`
	
	// EventBufferSize sets the size of the internal event buffer
	EventBufferSize int `json:"event_buffer_size" validate:"min=100,max=100000"`
	
	// Programs specifies which eBPF programs to load
	Programs []ProgramSpec `json:"programs" validate:"required,min=1"`
	
	// Filter configures event filtering
	Filter Filter `json:"filter"`
	
	// RingBufferSize sets the size of eBPF ring buffers (must be power of 2)
	RingBufferSize int `json:"ring_buffer_size" validate:"min=4096"`
	
	// BatchSize for reading events from ring buffer
	BatchSize int `json:"batch_size" validate:"min=1,max=1000"`
	
	// CollectionInterval for periodic event collection
	CollectionInterval time.Duration `json:"collection_interval" validate:"min=10ms,max=1m"`
	
	// MaxEventsPerSecond for rate limiting
	MaxEventsPerSecond int `json:"max_events_per_second" validate:"min=0"`
	
	// Timeout for eBPF operations
	Timeout time.Duration `json:"timeout" validate:"min=1s,max=5m"`
}

// Validate checks if the configuration is valid
func (c Config) Validate() error {
	if c.Name == "" {
		return ValidationError{Field: "name", Message: "name is required"}
	}
	
	if c.EventBufferSize < 100 || c.EventBufferSize > 100000 {
		return ValidationError{
			Field:   "event_buffer_size",
			Value:   c.EventBufferSize,
			Message: "must be between 100 and 100000",
		}
	}
	
	if len(c.Programs) == 0 {
		return ValidationError{Field: "programs", Message: "at least one program must be specified"}
	}
	
	// Validate ring buffer size is power of 2
	if c.RingBufferSize < 4096 || (c.RingBufferSize&(c.RingBufferSize-1)) != 0 {
		return ValidationError{
			Field:   "ring_buffer_size",
			Value:   c.RingBufferSize,
			Message: "must be a power of 2 and at least 4096",
		}
	}
	
	if c.BatchSize < 1 || c.BatchSize > 1000 {
		return ValidationError{
			Field:   "batch_size",
			Value:   c.BatchSize,
			Message: "must be between 1 and 1000",
		}
	}
	
	if c.CollectionInterval < 10*time.Millisecond || c.CollectionInterval > time.Minute {
		return ValidationError{
			Field:   "collection_interval",
			Value:   c.CollectionInterval,
			Message: "must be between 10ms and 1m",
		}
	}
	
	if c.Timeout < time.Second || c.Timeout > 5*time.Minute {
		return ValidationError{
			Field:   "timeout",
			Value:   c.Timeout,
			Message: "must be between 1s and 5m",
		}
	}
	
	// Validate each program spec
	for i, prog := range c.Programs {
		if err := prog.Validate(); err != nil {
			return ValidationError{
				Field:   "programs",
				Value:   i,
				Message: err.Error(),
			}
		}
	}
	
	return nil
}

// ProgramSpec defines an eBPF program to load
type ProgramSpec struct {
	// Name identifies the program
	Name string `json:"name" validate:"required"`
	
	// Type of eBPF program (e.g., "kprobe", "tracepoint", "raw_tracepoint")
	Type ProgramType `json:"type" validate:"required"`
	
	// AttachTarget specifies where to attach (e.g., function name for kprobe)
	AttachTarget string `json:"attach_target" validate:"required"`
	
	// Code contains the eBPF bytecode or source
	Code []byte `json:"-"`
	
	// CodePath points to the eBPF program file
	CodePath string `json:"code_path"`
	
	// Maps used by this program
	Maps []MapSpec `json:"maps"`
}

// Validate checks if the program spec is valid
func (p ProgramSpec) Validate() error {
	if p.Name == "" {
		return ValidationError{Field: "name", Message: "program name is required"}
	}
	
	if p.Type == "" {
		return ValidationError{Field: "type", Message: "program type is required"}
	}
	
	if p.AttachTarget == "" {
		return ValidationError{Field: "attach_target", Message: "attach target is required"}
	}
	
	if len(p.Code) == 0 && p.CodePath == "" {
		return ValidationError{Field: "code", Message: "either code or code_path must be specified"}
	}
	
	return nil
}

// ProgramType represents the type of eBPF program
type ProgramType string

const (
	ProgramTypeKprobe        ProgramType = "kprobe"
	ProgramTypeKretprobe     ProgramType = "kretprobe"
	ProgramTypeTracepoint    ProgramType = "tracepoint"
	ProgramTypeRawTracepoint ProgramType = "raw_tracepoint"
	ProgramTypeXDP           ProgramType = "xdp"
	ProgramTypeTC            ProgramType = "tc"
	ProgramTypePerfEvent     ProgramType = "perf_event"
)

// MapSpec defines an eBPF map
type MapSpec struct {
	// Name identifies the map
	Name string `json:"name" validate:"required"`
	
	// Type of eBPF map
	Type MapType `json:"type" validate:"required"`
	
	// KeySize in bytes
	KeySize uint32 `json:"key_size" validate:"required,min=1"`
	
	// ValueSize in bytes
	ValueSize uint32 `json:"value_size" validate:"required,min=1"`
	
	// MaxEntries in the map
	MaxEntries uint32 `json:"max_entries" validate:"required,min=1"`
}

// MapType represents the type of eBPF map
type MapType string

const (
	MapTypeHash          MapType = "hash"
	MapTypeArray         MapType = "array"
	MapTypeProgArray     MapType = "prog_array"
	MapTypePerfEventArray MapType = "perf_event_array"
	MapTypePerCPUHash    MapType = "percpu_hash"
	MapTypePerCPUArray   MapType = "percpu_array"
	MapTypeStackTrace    MapType = "stack_trace"
	MapTypeCgroupArray   MapType = "cgroup_array"
	MapTypeLRUHash       MapType = "lru_hash"
	MapTypeLRUPerCPUHash MapType = "lru_percpu_hash"
	MapTypeLPMTrie       MapType = "lpm_trie"
	MapTypeArrayOfMaps   MapType = "array_of_maps"
	MapTypeHashOfMaps    MapType = "hash_of_maps"
	MapTypeRingBuf       MapType = "ringbuf"
)

// EventType represents different types of eBPF events
type EventType string

const (
	EventTypeSyscall    EventType = "syscall"
	EventTypeNetworkIn  EventType = "network_in"
	EventTypeNetworkOut EventType = "network_out"
	EventTypeFileIO     EventType = "file_io"
	EventTypeProcessExec EventType = "process_exec"
	EventTypeProcessExit EventType = "process_exit"
	EventTypeMemoryAlloc EventType = "memory_alloc"
	EventTypeMemoryFree  EventType = "memory_free"
	EventTypeScheduler   EventType = "scheduler"
	EventTypeCustom      EventType = "custom"
)

// Filter defines event filtering criteria
type Filter struct {
	// EventTypes to include (empty means all)
	EventTypes []EventType `json:"event_types"`
	
	// ProcessIDs to monitor (empty means all)
	ProcessIDs []uint32 `json:"process_ids"`
	
	// ContainerIDs to monitor (empty means all)
	ContainerIDs []string `json:"container_ids"`
	
	// Namespaces to monitor (empty means all)
	Namespaces []string `json:"namespaces"`
	
	// MinSeverity for events (empty means all severities)
	MinSeverity domain.Severity `json:"min_severity"`
	
	// ExcludeSystemProcesses filters out kernel threads and system processes
	ExcludeSystemProcesses bool `json:"exclude_system_processes"`
}

// Program represents a loaded eBPF program
type Program struct {
	// ID is the kernel-assigned program ID
	ID uint32
	
	// Name from the program spec
	Name string
	
	// Type of program
	Type ProgramType
	
	// AttachTarget where the program is attached
	AttachTarget string
	
	// LoadTime when the program was loaded
	LoadTime time.Time
	
	// Stats contains program statistics
	Stats ProgramStats
}

// ProgramStats contains runtime statistics for an eBPF program
type ProgramStats struct {
	// RunCount is the number of times the program has been executed
	RunCount uint64
	
	// RunTime is the total time spent executing the program
	RunTime time.Duration
	
	// LastRun is the last time the program was executed
	LastRun time.Time
}

// ProgramInfo provides information about a loaded eBPF program
type ProgramInfo struct {
	Program
	
	// Maps used by this program
	Maps []MapInfo
}

// Map represents an eBPF map handle
type Map interface {
	// Lookup retrieves a value from the map
	Lookup(key []byte) ([]byte, error)
	
	// Update sets a value in the map
	Update(key, value []byte) error
	
	// Delete removes a key from the map
	Delete(key []byte) error
	
	// Iterate over all entries in the map
	Iterate(fn func(key, value []byte) error) error
	
	// Close releases the map handle
	Close() error
}

// MapInfo provides information about an eBPF map
type MapInfo struct {
	// Name of the map
	Name string
	
	// Type of map
	Type MapType
	
	// KeySize in bytes
	KeySize uint32
	
	// ValueSize in bytes
	ValueSize uint32
	
	// MaxEntries in the map
	MaxEntries uint32
	
	// CurrentEntries currently in the map
	CurrentEntries uint32
}

// ProcessInfo contains process-related information
type ProcessInfo struct {
	PID  uint32
	PPID uint32
	UID  uint32
	GID  uint32
	Name string
}

// Stats contains eBPF collector statistics
type Stats struct {
	// ProgramStats by program name
	Programs map[string]ProgramStats
	
	// EventsCollected total
	EventsCollected uint64
	
	// EventsDropped due to buffer overflow
	EventsDropped uint64
	
	// EventsFiltered out
	EventsFiltered uint64
	
	// BytesProcessed total
	BytesProcessed uint64
	
	// CollectionErrors count
	CollectionErrors uint64
	
	// LastCollectionTime
	LastCollectionTime time.Time
	
	// StartTime of the collector
	StartTime time.Time
	
	// RingBufferStats
	RingBufferStats RingBufferStats
}

// RingBufferStats contains ring buffer statistics
type RingBufferStats struct {
	// Size of the ring buffer
	Size int
	
	// Used bytes currently in the buffer
	Used int
	
	// Lost events due to buffer full
	Lost uint64
	
	// ReadErrors count
	ReadErrors uint64
}

// Health represents the health status of the eBPF collector
type Health struct {
	// Status indicates overall health
	Status HealthStatus
	
	// Message provides additional context
	Message string
	
	// LastCheck time
	LastCheck time.Time
	
	// ProgramsLoaded count
	ProgramsLoaded int
	
	// ProgramsHealthy count
	ProgramsHealthy int
	
	// Issues list of current issues
	Issues []HealthIssue
}

// HealthStatus represents the overall health state
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// HealthIssue describes a specific health problem
type HealthIssue struct {
	// Component affected (e.g., program name, map name)
	Component string
	
	// Issue description
	Issue string
	
	// Severity of the issue
	Severity domain.Severity
	
	// Since when the issue has been present
	Since time.Time
}