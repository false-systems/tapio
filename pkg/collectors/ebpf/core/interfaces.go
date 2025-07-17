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
	RingBufferSize   int           `json:"ring_buffer_size"`
	EventRateLimit   int           `json:"event_rate_limit"`
	SamplingInterval time.Duration `json:"sampling_interval"`
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

// EventProcessor processes raw eBPF events into domain events
type EventProcessor interface {
	ProcessEvent(ctx context.Context, raw RawEvent) (domain.Event, error)
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