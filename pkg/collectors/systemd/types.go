package systemd

// SystemdEvent represents a systemd-related event from eBPF
type SystemdEvent struct {
	Timestamp   uint64
	PID         uint32
	EventType   uint32
	ServiceName [64]byte
	UnitState   uint8
	_           [7]byte // Padding
}

// CollectorStats tracks collector metrics
type CollectorStats struct {
	EventsGenerated uint64
	EventsDropped   uint64
	LastEventTime   uint64
}

// Event types
const (
	EventTypeServiceStart uint32 = iota
	EventTypeServiceStop
	EventTypeServiceRestart
	EventTypeServiceReload
)