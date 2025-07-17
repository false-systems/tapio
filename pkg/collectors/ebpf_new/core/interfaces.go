package core

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Collector defines the eBPF collector interface that implements domain.EventSource
type Collector interface {
	domain.EventSource

	// Additional eBPF-specific methods
	
	// LoadPrograms loads eBPF programs into the kernel
	LoadPrograms(ctx context.Context) error
	
	// UnloadPrograms removes eBPF programs from the kernel
	UnloadPrograms() error
	
	// GetLoadedPrograms returns information about currently loaded programs
	GetLoadedPrograms() ([]ProgramInfo, error)
	
	// SetFilter applies a filter to the eBPF event collection
	SetFilter(filter Filter) error
	
	// GetStats returns eBPF-specific statistics
	GetStats() (Stats, error)
	
	// GetHealth returns the health status of the collector
	GetHealth() Health
}

// ProgramLoader manages eBPF program lifecycle
type ProgramLoader interface {
	// Load compiles and loads an eBPF program
	Load(ctx context.Context, spec ProgramSpec) (Program, error)
	
	// Unload removes a loaded program
	Unload(program Program) error
	
	// List returns all loaded programs
	List() ([]Program, error)
}

// EventParser converts raw eBPF events to domain events
type EventParser interface {
	// Parse converts raw eBPF data to a domain event
	Parse(data []byte, eventType EventType) (domain.Event, error)
	
	// CanParse checks if the parser can handle the given event type
	CanParse(eventType EventType) bool
}

// RingBufferReader reads events from eBPF ring buffers
type RingBufferReader interface {
	// Read reads the next event from the ring buffer
	Read() ([]byte, error)
	
	// ReadBatch reads multiple events at once for efficiency
	ReadBatch(maxEvents int) ([][]byte, error)
	
	// Close closes the ring buffer reader
	Close() error
}

// MapManager manages eBPF maps
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