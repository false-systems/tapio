//go:build linux

package etcdebpf

import "time"

// EtcdEvent represents an etcd event from eBPF monitoring
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

// EBPFEventData represents strongly-typed eBPF event data
type EBPFEventData struct {
	Timestamp uint64 `json:"timestamp"`
	PID       uint32 `json:"pid"`
	TID       uint32 `json:"tid"`
	Type      uint32 `json:"type"`
	DataLen   uint32 `json:"data_len"`
	SrcIP     string `json:"src_ip,omitempty"`
	DstIP     string `json:"dst_ip,omitempty"`
	SrcPort   uint16 `json:"src_port,omitempty"`
	DstPort   uint16 `json:"dst_port,omitempty"`
	RawData   []byte `json:"raw_data,omitempty"`
}

// EtcdProcessInfo represents verified etcd process information
type EtcdProcessInfo struct {
	PID        int32     `json:"pid"`
	PPID       int32     `json:"ppid"`
	Comm       string    `json:"comm"`
	Cmdline    string    `json:"cmdline"`
	StartTime  time.Time `json:"start_time"`
	VerifiedAt time.Time `json:"verified_at"`
}

// CollectorStats represents strongly-typed collector statistics
type CollectorStats struct {
	EventsProcessed int64             `json:"events_processed"`
	ErrorCount      int64             `json:"error_count"`
	LastEventTime   time.Time         `json:"last_event_time"`
	Uptime          time.Duration     `json:"uptime"`
	CustomMetrics   map[string]string `json:"custom_metrics,omitempty"`
}

// HealthStatus represents strongly-typed health status
type HealthStatus struct {
	Healthy       bool              `json:"healthy"`
	Message       string            `json:"message"`
	LastCheck     time.Time         `json:"last_check"`
	ComponentInfo map[string]string `json:"component_info,omitempty"`
}

// Event types for syscall monitoring
const (
	EventTypeGet uint32 = iota
	EventTypePut
	EventTypeDelete
	EventTypeWatch
)
