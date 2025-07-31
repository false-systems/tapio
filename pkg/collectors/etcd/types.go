package etcd

import (
	"time"
)

// OperationType represents the type of etcd operation
type OperationType string

const (
	OpPut         OperationType = "put"
	OpGet         OperationType = "get"
	OpDelete      OperationType = "delete"
	OpWatch       OperationType = "watch"
	OpLease       OperationType = "lease"
	OpTransaction OperationType = "transaction"
	OpCompaction  OperationType = "compaction"
	OpDefragment  OperationType = "defragment"
	OpSnapshot    OperationType = "snapshot"
	OpMaintenance OperationType = "maintenance"
)

// EtcdEvent represents a captured etcd operation
type EtcdEvent struct {
	// Timing
	Timestamp time.Time     `json:"timestamp"`
	Latency   time.Duration `json:"latency,omitempty"`

	// Operation details
	Operation OperationType `json:"operation"`
	Key       string        `json:"key,omitempty"`
	Value     []byte        `json:"value,omitempty"`
	ValueSize int           `json:"value_size,omitempty"`
	Revision  int64         `json:"revision,omitempty"`

	// Network details
	ClientIP   string `json:"client_ip,omitempty"`
	ClientPort uint16 `json:"client_port,omitempty"`
	ServerIP   string `json:"server_ip,omitempty"`
	ServerPort uint16 `json:"server_port,omitempty"`

	// Capture details
	CapturePoint string `json:"capture_point"` // "network", "syscall", "api"

	// Error info
	Error      string `json:"error,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
}

// EtcdMetrics tracks etcd collector performance
type EtcdMetrics struct {
	EventsCaptured  uint64
	EventsDropped   uint64
	BytesProcessed  uint64
	PacketsCaptured uint64
	SyscallsTraced  uint64
	ParseErrors     uint64
	LastEventTime   time.Time
}

// CaptureConfig configures what to capture
type CaptureConfig struct {
	// Network capture
	CaptureNetwork bool
	NetworkPorts   []uint16
	PayloadSize    int

	// Syscall capture
	CaptureSyscalls bool
	EtcdPIDs        []uint32

	// File monitoring
	MonitorFiles bool
	DataDir      string

	// Filtering
	KeyPrefixes []string        // Only capture these key prefixes
	Operations  []OperationType // Only capture these operations
}
