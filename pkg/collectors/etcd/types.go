package etcd

// EtcdEvent represents an etcd event from eBPF
type EtcdEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	EventType uint32
	Key       [256]byte
	Value     [64]byte
	Op        uint8
	_         [7]byte // Padding
}

// Event types
const (
	EventTypeGet uint32 = iota
	EventTypePut
	EventTypeDelete
	EventTypeWatch
)
