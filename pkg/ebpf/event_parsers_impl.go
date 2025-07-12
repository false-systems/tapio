//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"encoding/binary"
	"time"
)

// NetworkEventParser parses network events from eBPF
type NetworkEventParser struct{}

func NewNetworkEventParser() *NetworkEventParser {
	return &NetworkEventParser{}
}

func (p *NetworkEventParser) Parse(data []byte) (interface{}, error) {
	// Use the proper parsing function from event_parsers.go
	return parseRawNetworkEvent(data)
}

func (p *NetworkEventParser) EventType() string {
	return string(CategoryNetwork)
}

// DNSEventParser parses DNS events from eBPF
type DNSEventParser struct{}

func NewDNSEventParser() *DNSEventParser {
	return &DNSEventParser{}
}

func (p *DNSEventParser) Parse(data []byte) (interface{}, error) {
	// Use the proper parsing function from event_parsers.go
	return parseRawDNSEvent(data)
}

func (p *DNSEventParser) EventType() string {
	return string(CategoryDNS)
}

// PacketEventParser parses packet events from eBPF
type PacketEventParser struct{}

func NewPacketEventParser() *PacketEventParser {
	return &PacketEventParser{}
}

func (p *PacketEventParser) Parse(data []byte) (interface{}, error) {
	// Use the proper parsing function from event_parsers.go
	return parseRawPacketEvent(data)
}

func (p *PacketEventParser) EventType() string {
	return "packet"
}

// ProtocolEventParser parses protocol events from eBPF
type ProtocolEventParser struct{}

func NewProtocolEventParser() *ProtocolEventParser {
	return &ProtocolEventParser{}
}

func (p *ProtocolEventParser) Parse(data []byte) (interface{}, error) {
	// Use the proper parsing function from event_parsers.go
	return parseRawProtocolEvent(data)
}

func (p *ProtocolEventParser) EventType() string {
	return "protocol"
}

// MemoryEventParser parses memory events from eBPF (already exists but adding for completeness)
type MemoryEventParser struct{}

func NewMemoryEventParser() *MemoryEventParser {
	return &MemoryEventParser{}
}

func (p *MemoryEventParser) Parse(data []byte) (interface{}, error) {
	// Use the existing parseRawMemoryEvent function from events.go
	return parseRawMemoryEvent(data)
}

func (p *MemoryEventParser) EventType() string {
	return "memory"
}

// SystemEventParser parses generic system events
type SystemEventParser struct{}

func NewSystemEventParser() *SystemEventParser {
	return &SystemEventParser{}
}

func (p *SystemEventParser) Parse(data []byte) (interface{}, error) {
	// For now, system events are generic and don't have a specific parser
	// This is a placeholder for future implementation
	if len(data) < 12 {
		return nil, NewParserError("SystemEventParser", string(CategorySystem), len(data), 12)
	}
	
	// Return a minimal SystemEvent
	return &SystemEvent{
		Type:      string(CategorySystem),
		Timestamp: time.Now(),
		PID:       binary.LittleEndian.Uint32(data[0:4]),
		Data:      data,
	}, nil
}

func (p *SystemEventParser) EventType() string {
	return string(CategorySystem)
}

// CPUEventParser parses CPU events (for future implementation)
type CPUEventParser struct{}

func NewCPUEventParser() *CPUEventParser {
	return &CPUEventParser{}
}

func (p *CPUEventParser) Parse(data []byte) (interface{}, error) {
	// CPU events are not yet implemented in the C eBPF programs
	// This is a placeholder for future implementation
	if len(data) < 36 {
		return nil, NewParserError("CPUEventParser", string(CategoryCPU), len(data), 36)
	}
	
	// Parse timestamp from binary data
	timestamp := binary.LittleEndian.Uint64(data[0:8])
	
	event := &CPUEvent{
		Timestamp:      time.Unix(0, int64(timestamp)),
		CPU:            binary.LittleEndian.Uint32(data[8:12]),
		PID:            binary.LittleEndian.Uint32(data[12:16]),
		TGID:           binary.LittleEndian.Uint32(data[16:20]),
		EventType:      data[20],
		RunQueueLength: binary.LittleEndian.Uint32(data[24:28]),
		LatencyNS:      binary.LittleEndian.Uint64(data[28:36]),
	}
	
	return event, nil
}

func (p *CPUEventParser) EventType() string {
	return string(CategoryCPU)
}

// IOEventParser parses I/O events (for future implementation)
type IOEventParser struct{}

func NewIOEventParser() *IOEventParser {
	return &IOEventParser{}
}

func (p *IOEventParser) Parse(data []byte) (interface{}, error) {
	// I/O events are not yet implemented in the C eBPF programs
	// This is a placeholder for future implementation
	if len(data) < 48 {
		return nil, NewParserError("IOEventParser", string(CategoryIO), len(data), 48)
	}
	
	// Parse timestamp from binary data
	timestamp := binary.LittleEndian.Uint64(data[0:8])
	
	event := &IOEvent{
		Timestamp:   time.Unix(0, int64(timestamp)),
		PID:         binary.LittleEndian.Uint32(data[8:12]),
		TGID:        binary.LittleEndian.Uint32(data[12:16]),
		EventType:   data[16],
		DeviceMajor: binary.LittleEndian.Uint32(data[20:24]),
		DeviceMinor: binary.LittleEndian.Uint32(data[24:28]),
		Sector:      binary.LittleEndian.Uint64(data[28:36]),
		BytesCount:  binary.LittleEndian.Uint32(data[36:40]),
		LatencyNS:   binary.LittleEndian.Uint64(data[40:48]),
	}
	
	return event, nil
}

func (p *IOEventParser) EventType() string {
	return string(CategoryIO)
}

// Additional event types for completeness
type CPUEvent struct {
	Timestamp      time.Time
	CPU            uint32
	PID            uint32
	TGID           uint32
	EventType      uint8
	RunQueueLength uint32
	LatencyNS      uint64
}

type IOEvent struct {
	Timestamp   time.Time
	PID         uint32
	TGID        uint32
	EventType   uint8
	DeviceMajor uint32
	DeviceMinor uint32
	Sector      uint64
	BytesCount  uint32
	LatencyNS   uint64
}