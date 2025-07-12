package ebpf

import (
	"context"
	"errors"
	"time"
)

// Common errors
var (
	ErrNotSupported = errors.New("eBPF is not supported on this platform")
	ErrNotEnabled   = errors.New("eBPF monitoring is disabled")
)

// SystemEvent is a unified event structure
type SystemEvent struct {
	Type      string
	Timestamp time.Time
	PID       uint32
	Data      interface{}
}

// SimpleNetworkEvent represents network-related events (simplified version)
type SimpleNetworkEvent struct {
	Timestamp    uint64
	PID          uint32
	SrcIP        uint32
	DstIP        uint32
	SrcPort      uint16
	DstPort      uint16
	Protocol     uint8
	EventType    uint8
	BytesSent    uint64
	BytesRecv    uint64
	Duration     uint64
	Retransmits  uint32
	PacketsLost  uint32
	Latency      uint32
	ConnectionID uint64
	ContainerID  string
}

// SimplePacketEvent represents packet-level events
type SimplePacketEvent struct {
	Timestamp   uint64
	SrcIP       uint32
	DstIP       uint32
	SrcPort     uint16
	DstPort     uint16
	Protocol    uint8
	PacketSize  uint16
	Direction   uint8
	Flags       uint8
	QueueDelay  uint32
	ContainerID string
}

// SimpleDNSEvent represents DNS resolution events
type SimpleDNSEvent struct {
	Timestamp    uint64
	PID          uint32
	QueryType    uint16
	ResponseCode uint16
	QueryTime    uint32
	ServerIP     uint32
	Flags        uint16
	ContainerID  string
	Domain       string
}

// SimpleProtocolEvent represents application protocol events
type SimpleProtocolEvent struct {
	Timestamp    uint64
	PID          uint32
	Protocol     uint8
	Method       uint8
	StatusCode   uint16
	RequestSize  uint32
	ResponseSize uint32
	Duration     uint32
	ContainerID  string
}

// OOMEvent represents an OOM event
type OOMEvent struct {
	PID            uint32   `json:"pid"`
	TGID           uint32   `json:"tgid"`
	Comm           [16]byte `json:"comm"`
	Timestamp      uint64   `json:"timestamp"`
	MemoryLimit    uint64   `json:"memory_limit"`
	MemoryUsage    uint64   `json:"memory_usage"`
	MemoryMaxUsage uint64   `json:"memory_max_usage"`
	OOMKillCount   uint32   `json:"oom_kill_count"`
	ContainerID    string   `json:"container_id,omitempty"`
}

// Statistics structures
type NetworkConnectionStats struct {
	StartTime       time.Time
	LastSeen        time.Time
	BytesSent       uint64
	BytesReceived   uint64
	PacketsSent     uint64
	PacketsReceived uint64
	Retransmits     uint64
	Latency         time.Duration
}

type DNSQueryStats struct {
	Domain       string
	QueryCount   uint64
	SuccessCount uint64
	FailureCount uint64
	AvgLatency   time.Duration
	LastQueried  time.Time
}

type ProtocolStats struct {
	Protocol     string
	RequestCount uint64
	SuccessCount uint64
	ErrorCount   uint64
	AvgLatency   time.Duration
	TotalBytes   uint64
}

// ProcessMemoryStats represents memory statistics for a process
type ProcessMemoryStats struct {
	PID            uint32            `json:"pid"`
	Command        string            `json:"command"`
	TotalAllocated uint64            `json:"total_allocated"`
	TotalFreed     uint64            `json:"total_freed"`
	CurrentUsage   uint64            `json:"current_usage"`
	AllocationRate float64           `json:"allocation_rate"` // bytes per second
	LastUpdate     time.Time         `json:"last_update"`
	InContainer    bool              `json:"in_container"`
	ContainerPID   uint32            `json:"container_pid"`
	GrowthPattern  []MemoryDataPoint `json:"growth_pattern"`
}

// MemoryDataPoint represents a single memory measurement
type MemoryDataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Usage     uint64    `json:"usage"`
}

// OOMPrediction represents an out-of-memory prediction
type OOMPrediction struct {
	PID                uint32        `json:"pid"`
	TimeToOOM          time.Duration `json:"time_to_oom"`
	Confidence         float64       `json:"confidence"`
	CurrentUsage       uint64        `json:"current_usage"`
	MemoryLimit        uint64        `json:"memory_limit"`
	PredictedPeakUsage uint64        `json:"predicted_peak_usage"`
}

// Monitor defines the interface for eBPF monitoring
type Monitor interface {
	// Start begins eBPF monitoring
	Start(ctx context.Context) error

	// Stop gracefully stops monitoring
	Stop() error

	// GetMemoryStats returns current memory statistics
	GetMemoryStats() ([]ProcessMemoryStats, error)

	// GetMemoryPredictions returns OOM predictions
	GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error)

	// IsAvailable checks if eBPF is available on this system
	IsAvailable() bool

	// GetLastError returns the last error encountered
	GetLastError() error
}

// Config represents eBPF monitor configuration
type Config struct {
	Enabled                 bool          `json:"enabled"`
	EnableMemoryMonitoring  bool          `json:"enable_memory_monitoring"`
	EnableNetworkMonitoring bool          `json:"enable_network_monitoring"`
	EnablePacketAnalysis    bool          `json:"enable_packet_analysis"`
	EnableDNSMonitoring     bool          `json:"enable_dns_monitoring"`
	EnableProtocolAnalysis  bool          `json:"enable_protocol_analysis"`
	SamplingRate            float64       `json:"sampling_rate"`
	BufferSize              int           `json:"buffer_size"`
	ProcessTimeout          time.Duration `json:"process_timeout"`
	Debug                   bool          `json:"debug"`
	EventBufferSize         int           `json:"event_buffer_size"`
	RetentionPeriod         string        `json:"retention_period"`
}

// DefaultConfig returns default eBPF configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:                 false, // Disabled by default
		EnableMemoryMonitoring:  true,
		EnableNetworkMonitoring: true,
		EnablePacketAnalysis:    true,
		EnableDNSMonitoring:     true,
		EnableProtocolAnalysis:  true,
		SamplingRate:            1.0,
		BufferSize:              65536,
		EventBufferSize:         1000,
		RetentionPeriod:         "5m",
		ProcessTimeout:          5 * time.Minute,
		Debug:                   false,
	}
}
