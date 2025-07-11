package ebpf

import "time"

// SystemEvent is a unified event structure
type SystemEvent struct {
	Type      string
	Timestamp time.Time
	PID       uint32
	Data      interface{}
}

// NetworkEvent represents network-related events
type NetworkEvent struct {
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

// PacketEvent represents packet-level events
type PacketEvent struct {
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

// DNSEvent represents DNS resolution events
type DNSEvent struct {
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

// ProtocolEvent represents application protocol events
type ProtocolEvent struct {
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
	PID          uint32    `json:"pid"`
	TGID         uint32    `json:"tgid"`
	Comm         [16]byte  `json:"comm"`
	Timestamp    uint64    `json:"timestamp"`
	MemoryLimit  uint64    `json:"memory_limit"`
	MemoryUsage  uint64    `json:"memory_usage"`
	MemoryMaxUsage uint64  `json:"memory_max_usage"`
	OOMKillCount uint32    `json:"oom_kill_count"`
	ContainerID  string    `json:"container_id,omitempty"`
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
	Protocol      string
	RequestCount  uint64
	SuccessCount  uint64
	ErrorCount    uint64
	AvgLatency    time.Duration
	TotalBytes    uint64
}