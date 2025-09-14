package systemd

import "time"

// SystemdEvent represents a systemd event from eBPF - must match C struct
type SystemdEvent struct {
	Timestamp   uint64
	PID         uint32
	PPID        uint32
	UID         uint32
	GID         uint32
	CgroupID    uint64
	EventType   uint8
	Pad         [3]uint8
	Comm        [16]byte
	ServiceName [64]byte
	CgroupPath  [256]byte
	ExitCode    uint32
	Signal      uint32
}

// ObserverStats tracks observer metrics
type ObserverStats struct {
	EventsGenerated uint64
	EventsDropped   uint64
	LastEventTime   time.Time

	// Service state tracking
	ServicesMonitored int
	ServiceStarts     uint64
	ServiceStops      uint64
	ServiceRestarts   uint64
	ServiceFailures   uint64
}

// ServiceState represents the state of a systemd service
type ServiceState struct {
	Name         string
	State        string // active, inactive, failed, etc.
	SubState     string // running, exited, etc.
	PID          uint32
	ExitCode     int32
	LastChanged  time.Time
	RestartCount uint32
}

// Event types
const (
	EventTypeServiceStart uint8 = iota
	EventTypeServiceStop
	EventTypeServiceRestart
	EventTypeServiceReload
	EventTypeServiceFailed
	EventTypeCgroupCreated
	EventTypeCgroupDestroyed
)

// Service states
const (
	StateActive       = "active"
	StateInactive     = "inactive"
	StateFailed       = "failed"
	StateActivating   = "activating"
	StateDeactivating = "deactivating"
)
