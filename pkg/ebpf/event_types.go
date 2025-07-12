package ebpf

// EventCategory represents the category of eBPF events
type EventCategory string

const (
	// Event categories - returned by EventType() method
	CategoryNetwork  EventCategory = "network"
	CategoryDNS      EventCategory = "dns"
	CategoryPacket   EventCategory = "packet"
	CategoryProtocol EventCategory = "protocol"
	CategoryMemory   EventCategory = "memory"
	CategoryCPU      EventCategory = "cpu"
	CategoryIO       EventCategory = "io"
	CategorySystem   EventCategory = "system"
)

// NetworkEventType represents specific network event types
type NetworkEventType uint8

const (
	// Network event subtypes
	NetworkEventConnEstablished NetworkEventType = 1
	NetworkEventConnClosed      NetworkEventType = 2
	NetworkEventConnFailed      NetworkEventType = 3
	NetworkEventPacketDrop      NetworkEventType = 4
	NetworkEventHighLatency     NetworkEventType = 5
	NetworkEventRetransmit      NetworkEventType = 6
)

// DNSEventType represents specific DNS event types
type DNSEventType uint8

const (
	// DNS event subtypes
	DNSEventQuery    DNSEventType = 1
	DNSEventResponse DNSEventType = 2
	DNSEventTimeout  DNSEventType = 3
	DNSEventError    DNSEventType = 4
	DNSEventNXDomain DNSEventType = 5
)

// PacketEventType represents specific packet event types
type PacketEventType uint8

const (
	// Packet event subtypes
	PacketEventLoss        PacketEventType = 1
	PacketEventHighLatency PacketEventType = 2
	PacketEventReorder     PacketEventType = 3
	PacketEventDuplicate   PacketEventType = 4
	PacketEventCorruption  PacketEventType = 5
)

// ProtocolEventType represents specific protocol event types
type ProtocolEventType uint8

const (
	// Protocol event subtypes
	ProtocolEventRequest  ProtocolEventType = 1
	ProtocolEventResponse ProtocolEventType = 2
	ProtocolEventError    ProtocolEventType = 3
	ProtocolEventTimeout  ProtocolEventType = 4
	ProtocolEventSlow     ProtocolEventType = 5
)

// MemoryEventType represents specific memory event types
type MemoryEventType uint32

const (
	// Memory event subtypes
	MemoryEventAlloc       MemoryEventType = 1
	MemoryEventFree        MemoryEventType = 2
	MemoryEventOOMKill     MemoryEventType = 3
	MemoryEventProcessExit MemoryEventType = 4
)

// CPUEventType represents specific CPU event types
type CPUEventType uint8

const (
	// CPU event subtypes
	CPUEventThrottle  CPUEventType = 1
	CPUEventSchedule  CPUEventType = 2
	CPUEventMigration CPUEventType = 3
	CPUEventStall     CPUEventType = 4
	CPUEventPressure  CPUEventType = 5
)

// IOEventType represents specific I/O event types
type IOEventType uint8

const (
	// I/O event subtypes
	IOEventRead    IOEventType = 1
	IOEventWrite   IOEventType = 2
	IOEventSync    IOEventType = 3
	IOEventLatency IOEventType = 4
	IOEventError   IOEventType = 5
)

// String representations for better logging
func (n NetworkEventType) String() string {
	switch n {
	case NetworkEventConnEstablished:
		return "connection_established"
	case NetworkEventConnClosed:
		return "connection_closed"
	case NetworkEventConnFailed:
		return "connection_failed"
	case NetworkEventPacketDrop:
		return "packet_drop"
	case NetworkEventHighLatency:
		return "high_latency"
	case NetworkEventRetransmit:
		return "retransmit"
	default:
		return "unknown"
	}
}

func (d DNSEventType) String() string {
	switch d {
	case DNSEventQuery:
		return "query"
	case DNSEventResponse:
		return "response"
	case DNSEventTimeout:
		return "timeout"
	case DNSEventError:
		return "error"
	case DNSEventNXDomain:
		return "nxdomain"
	default:
		return "unknown"
	}
}

func (m MemoryEventType) String() string {
	switch m {
	case MemoryEventAlloc:
		return "allocation"
	case MemoryEventFree:
		return "free"
	case MemoryEventOOMKill:
		return "oom_kill"
	case MemoryEventProcessExit:
		return "process_exit"
	default:
		return "unknown"
	}
}
