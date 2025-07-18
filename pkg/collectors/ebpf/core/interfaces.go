package core

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Collector defines the interface for eBPF event collection
type Collector interface {
	// Lifecycle management
	Start(ctx context.Context) error
	Stop() error
	
	// Event streaming
	Events() <-chan domain.Event
	
	// Health and monitoring
	Health() Health
	Statistics() Statistics
	
	// Configuration
	Configure(config Config) error
}

// Config defines eBPF collector configuration
type Config struct {
	// Basic settings
	Name            string        `json:"name"`
	Enabled         bool          `json:"enabled"`
	EventBufferSize int           `json:"event_buffer_size"`
	
	// Feature toggles
	EnableNetwork bool `json:"enable_network"`
	EnableMemory  bool `json:"enable_memory"`
	EnableProcess bool `json:"enable_process"`
	EnableFile    bool `json:"enable_file"`
	
	// Performance tuning
	RingBufferSize     int           `json:"ring_buffer_size"`
	EventRateLimit     int           `json:"event_rate_limit"`
	SamplingInterval   time.Duration `json:"sampling_interval"`
	BatchSize          int           `json:"batch_size"`          // New: for batch processing
	CollectionInterval time.Duration `json:"collection_interval"` // New: for batch collection
	MaxEventsPerSecond int           `json:"max_events_per_second"` // New: rate limiting
	
	// Advanced features
	Programs []ProgramSpec `json:"programs"` // New: eBPF program specifications
	Filter   Filter        `json:"filter"`   // New: event filtering
	
	// Data retention
	RetentionPeriod string        `json:"retention_period"`
	Timeout         time.Duration `json:"timeout"` // New: operation timeout
}

// Health represents collector health status
type Health struct {
	Status          HealthStatus      `json:"status"`
	Message         string            `json:"message"`
	LastEventTime   time.Time         `json:"last_event_time"`
	EventsProcessed uint64            `json:"events_processed"`
	EventsDropped   uint64            `json:"events_dropped"`
	ErrorCount      uint64            `json:"error_count"`
	Metrics         map[string]float64 `json:"metrics"`
}

// HealthStatus represents the health state
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// Statistics represents runtime statistics
type Statistics struct {
	StartTime          time.Time          `json:"start_time"`
	EventsCollected    uint64             `json:"events_collected"`
	EventsDropped      uint64             `json:"events_dropped"`
	BytesProcessed     uint64             `json:"bytes_processed"`
	ProgramsLoaded     int                `json:"programs_loaded"`
	MapsCreated        int                `json:"maps_created"`
	CPUTimeNanos       uint64             `json:"cpu_time_nanos"`
	MemoryBytes        uint64             `json:"memory_bytes"`
	Custom             map[string]interface{} `json:"custom"`
}

// ProgramLoader loads eBPF programs
type ProgramLoader interface {
	LoadProgram(ctx context.Context, spec ProgramSpec) error
	UnloadProgram(name string) error
	ListPrograms() []ProgramInfo
}

// ProgramSpec defines an eBPF program specification
type ProgramSpec struct {
	Name       string            `json:"name"`
	Type       ProgramType       `json:"type"`
	AttachType AttachType        `json:"attach_type"`
	Source     []byte            `json:"-"` // BPF bytecode
	Maps       map[string]MapSpec `json:"maps"`
}

// ProgramType represents the type of eBPF program
type ProgramType string

const (
	ProgramTypeKprobe     ProgramType = "kprobe"
	ProgramTypeKretprobe  ProgramType = "kretprobe"
	ProgramTypeTracepoint ProgramType = "tracepoint"
	ProgramTypeRawTrace   ProgramType = "raw_tracepoint"
	ProgramTypePerfEvent  ProgramType = "perf_event"
)

// AttachType represents how the program is attached
type AttachType string

const (
	AttachTypeEntry AttachType = "entry"
	AttachTypeExit  AttachType = "exit"
)

// MapSpec defines an eBPF map specification
type MapSpec struct {
	Type       MapType `json:"type"`
	KeySize    uint32  `json:"key_size"`
	ValueSize  uint32  `json:"value_size"`
	MaxEntries uint32  `json:"max_entries"`
}

// MapType represents the type of eBPF map
type MapType string

const (
	MapTypeHash      MapType = "hash"
	MapTypeArray     MapType = "array"
	MapTypeRingBuf   MapType = "ringbuf"
	MapTypePerfEvent MapType = "perf_event_array"
)

// ProgramInfo contains information about a loaded program
type ProgramInfo struct {
	Name         string    `json:"name"`
	Type         ProgramType `json:"type"`
	AttachType   AttachType  `json:"attach_type"`
	LoadedAt     time.Time   `json:"loaded_at"`
	EventCount   uint64      `json:"event_count"`
	ErrorCount   uint64      `json:"error_count"`
}

// MapInfo contains information about an eBPF map
type MapInfo struct {
	Name       string  `json:"name"`
	Type       MapType `json:"type"`
	KeySize    uint32  `json:"key_size"`
	ValueSize  uint32  `json:"value_size"`
	MaxEntries uint32  `json:"max_entries"`
	UsedEntries uint32 `json:"used_entries"`
}

// Filter defines event filtering criteria
type Filter struct {
	EventTypes             []string `json:"event_types,omitempty"`
	ProcessNames           []string `json:"process_names,omitempty"`
	ProcessIDs             []uint32 `json:"process_ids,omitempty"`
	ContainerIDs           []string `json:"container_ids,omitempty"`
	Namespaces             []string `json:"namespaces,omitempty"`
	ExcludeSystemProcesses bool     `json:"exclude_system_processes"`
	MinSeverity            string   `json:"min_severity,omitempty"`
}

// EventProcessor processes raw eBPF events into domain events
type EventProcessor interface {
	ProcessEvent(ctx context.Context, raw RawEvent) (domain.Event, error)
}

// RingBufferReader reads events from eBPF ring buffers for better performance
type RingBufferReader interface {
	// Read reads the next event from the ring buffer
	Read() ([]byte, error)
	
	// ReadBatch reads multiple events at once for efficiency
	ReadBatch(maxEvents int) ([][]byte, error)
	
	// Close closes the ring buffer reader
	Close() error
}

// MapManager manages eBPF maps for advanced data structures
type MapManager interface {
	// CreateMap creates a new eBPF map
	CreateMap(spec MapSpec) (Map, error)
	
	// GetMap retrieves an existing map by name
	GetMap(name string) (Map, error)
	
	// DeleteMap removes a map
	DeleteMap(name string) error
	
	// ListMaps returns all managed maps
	ListMaps() ([]MapInfo, error)
}

// Map represents an eBPF map
type Map interface {
	// Lookup retrieves a value by key
	Lookup(key []byte) ([]byte, error)
	
	// Update sets a key-value pair
	Update(key, value []byte) error
	
	// Delete removes a key
	Delete(key []byte) error
	
	// Iterate iterates over all entries
	Iterate(fn func(key, value []byte) error) error
	
	// Close closes the map
	Close() error
}

// RawEvent represents a raw eBPF event
type RawEvent struct {
	Timestamp   time.Time              `json:"timestamp"`
	Type        string                 `json:"type"`
	CPU         uint32                 `json:"cpu"`
	PID         uint32                 `json:"pid"`
	TID         uint32                 `json:"tid"`
	UID         uint32                 `json:"uid"`
	GID         uint32                 `json:"gid"`
	Comm        string                 `json:"comm"`
	Data        []byte                 `json:"-"`
	Decoded     map[string]interface{} `json:"decoded,omitempty"`
}

// Monitor represents the eBPF monitoring interface for health checks
type Monitor interface {
	// IsAvailable checks if eBPF is available on the system
	IsAvailable() bool
	
	// GetLastError returns the last error encountered
	GetLastError() error
	
	// Start starts the monitor
	Start(ctx context.Context) error
	
	// Stop stops the monitor
	Stop() error
	
	// GetMemoryStats returns memory statistics
	GetMemoryStats() map[string]interface{}
}

// Validate validates the configuration
func (c Config) Validate() error {
	if c.EventBufferSize <= 0 {
		c.EventBufferSize = 1000
	}
	if c.RingBufferSize <= 0 {
		c.RingBufferSize = 8192
	}
	return nil
}

// GetDetailedStatus returns detailed eBPF status information
func GetDetailedStatus() map[string]interface{} {
	return map[string]interface{}{
		"kernel_support": checkKernelSupport(),
		"permissions":    checkPermissions(),
		"bpf_jit":       checkBPFJIT(),
		"recommendations": getRecommendations(),
	}
}

// GetAvailabilityStatus returns a human-readable availability status
func GetAvailabilityStatus() string {
	if !checkKernelSupport() {
		return "eBPF not supported on this kernel version"
	}
	if !checkPermissions() {
		return "Insufficient permissions for eBPF (need CAP_SYS_ADMIN or root)"
	}
	return "eBPF is available"
}

// Helper functions for status checks
func checkKernelSupport() bool {
	// This is a simplified check - in reality would check kernel version
	return true
}

func checkPermissions() bool {
	// This is a simplified check - in reality would check capabilities
	return true
}

func checkBPFJIT() bool {
	// This is a simplified check - in reality would check sysctl
	return true
}

func getRecommendations() []string {
	var recs []string
	if !checkKernelSupport() {
		recs = append(recs, "Upgrade to Linux kernel 4.14 or later for eBPF support")
	}
	if !checkPermissions() {
		recs = append(recs, "Run with sudo or add CAP_SYS_ADMIN capability")
	}
	return recs
}