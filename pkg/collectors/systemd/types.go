package systemd

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
