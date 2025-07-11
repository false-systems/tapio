//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"encoding/binary"
	"fmt"
	"time"
)

// NetworkEventParser parses network events from eBPF
type NetworkEventParser struct{}

func NewNetworkEventParser() *NetworkEventParser {
	return &NetworkEventParser{}
}

func (p *NetworkEventParser) Parse(data []byte) (interface{}, error) {
	if len(data) < 64 { // Minimum size for network event
		return nil, fmt.Errorf("insufficient data for network event: %d bytes", len(data))
	}
	
	// Parse timestamp from binary data
	timestamp := binary.LittleEndian.Uint64(data[0:8])
	
	// Create event matching enhanced_collector.go's NetworkEvent
	event := &NetworkEvent{
		Timestamp:   time.Unix(0, int64(timestamp)),
		PID:         binary.LittleEndian.Uint32(data[8:12]),
		TGID:        binary.LittleEndian.Uint32(data[12:16]),
		UID:         binary.LittleEndian.Uint32(data[16:20]),
		SrcIP:       binary.LittleEndian.Uint32(data[20:24]),
		DstIP:       binary.LittleEndian.Uint32(data[24:28]),
		SrcPort:     binary.LittleEndian.Uint16(data[28:30]),
		DstPort:     binary.LittleEndian.Uint16(data[30:32]),
		Protocol:    data[32],
		EventType:   data[33],
		LatencyUS:   binary.LittleEndian.Uint32(data[34:38]),
		Bytes:       binary.LittleEndian.Uint32(data[38:42]),
		ErrorCode:   binary.LittleEndian.Uint16(data[42:44]),
	}
	
	// Parse command if present
	if len(data) > 44 {
		cmdEnd := 44
		for i := 44; i < len(data) && i < 60; i++ {
			if data[i] == 0 {
				cmdEnd = i
				break
			}
		}
		event.Command = string(data[44:cmdEnd])
	}
	
	// Parse container ID if present
	if len(data) > 60 {
		cidEnd := 60
		for i := 60; i < len(data) && i < 72; i++ {
			if data[i] == 0 {
				cidEnd = i
				break
			}
		}
		event.ContainerID = string(data[60:cidEnd])
	}
	
	return event, nil
}

func (p *NetworkEventParser) EventType() string {
	return "network"
}

// DNSEventParser parses DNS events from eBPF
type DNSEventParser struct{}

func NewDNSEventParser() *DNSEventParser {
	return &DNSEventParser{}
}

func (p *DNSEventParser) Parse(data []byte) (interface{}, error) {
	if len(data) < 32 { // Minimum size for DNS event
		return nil, fmt.Errorf("insufficient data for DNS event: %d bytes", len(data))
	}
	
	// Parse timestamp from binary data
	timestamp := binary.LittleEndian.Uint64(data[0:8])
	
	// Create event matching enhanced_collector.go's DNSEvent
	event := &DNSEvent{
		Timestamp:    time.Unix(0, int64(timestamp)),
		PID:          binary.LittleEndian.Uint32(data[8:12]),
		TGID:         binary.LittleEndian.Uint32(data[12:16]),
		UID:          binary.LittleEndian.Uint32(data[16:20]),
		SrcIP:        binary.LittleEndian.Uint32(data[20:24]),
		DstIP:        binary.LittleEndian.Uint32(data[24:28]),
		QueryID:      binary.LittleEndian.Uint16(data[28:30]),
		QueryType:    binary.LittleEndian.Uint16(data[30:32]),
		QueryClass:   binary.LittleEndian.Uint16(data[32:34]),
		EventType:    data[34],
		ResponseCode: data[35],
		LatencyMS:    binary.LittleEndian.Uint32(data[36:40]),
		AnswerCount:  binary.LittleEndian.Uint32(data[40:44]),
	}
	
	// Parse domain name
	if len(data) > 44 {
		nameEnd := 44
		for i := 44; i < len(data) && i < 300; i++ { // Max DNS name is 255 + overhead
			if data[i] == 0 {
				nameEnd = i
				break
			}
		}
		event.Domain = string(data[44:nameEnd])
	}
	
	return event, nil
}

func (p *DNSEventParser) EventType() string {
	return "dns"
}

// PacketEventParser parses packet events from eBPF
type PacketEventParser struct{}

func NewPacketEventParser() *PacketEventParser {
	return &PacketEventParser{}
}

func (p *PacketEventParser) Parse(data []byte) (interface{}, error) {
	if len(data) < 32 { // Minimum size for packet event
		return nil, fmt.Errorf("insufficient data for packet event: %d bytes", len(data))
	}
	
	// Parse timestamp from binary data
	timestamp := binary.LittleEndian.Uint64(data[0:8])
	
	// Create event matching enhanced_collector.go's PacketEvent
	event := &PacketEvent{
		Timestamp:   time.Unix(0, int64(timestamp)),
		PID:         binary.LittleEndian.Uint32(data[8:12]),
		TGID:        binary.LittleEndian.Uint32(data[12:16]),
		SrcIP:       binary.LittleEndian.Uint32(data[16:20]),
		DstIP:       binary.LittleEndian.Uint32(data[20:24]),
		SrcPort:     binary.LittleEndian.Uint16(data[24:26]),
		DstPort:     binary.LittleEndian.Uint16(data[26:28]),
		Protocol:    data[28],
		EventType:   data[29],
		LatencyUS:   binary.LittleEndian.Uint32(data[30:34]),
		PacketSize:  binary.LittleEndian.Uint32(data[34:38]),
		SequenceNum: binary.LittleEndian.Uint32(data[38:42]),
		AckNum:      binary.LittleEndian.Uint32(data[42:46]),
		WindowSize:  binary.LittleEndian.Uint16(data[46:48]),
		TCPFlags:    data[48],
	}
	
	// Parse command if present
	if len(data) > 49 {
		cmdEnd := 49
		for i := 49; i < len(data) && i < 65; i++ {
			if data[i] == 0 {
				cmdEnd = i
				break
			}
		}
		event.Command = string(data[49:cmdEnd])
	}
	
	// Parse interface if present
	if len(data) > 65 {
		ifEnd := 65
		for i := 65; i < len(data) && i < 81; i++ {
			if data[i] == 0 {
				ifEnd = i
				break
			}
		}
		event.Interface = string(data[65:ifEnd])
	}
	
	return event, nil
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
	if len(data) < 32 { // Minimum size for protocol event
		return nil, fmt.Errorf("insufficient data for protocol event: %d bytes", len(data))
	}
	
	// Parse timestamp from binary data
	timestamp := binary.LittleEndian.Uint64(data[0:8])
	
	// Create event matching enhanced_collector.go's ProtocolEvent
	event := &ProtocolEvent{
		Timestamp:     time.Unix(0, int64(timestamp)),
		PID:           binary.LittleEndian.Uint32(data[8:12]),
		TGID:          binary.LittleEndian.Uint32(data[12:16]),
		UID:           binary.LittleEndian.Uint32(data[16:20]),
		SrcIP:         binary.LittleEndian.Uint32(data[20:24]),
		DstIP:         binary.LittleEndian.Uint32(data[24:28]),
		SrcPort:       binary.LittleEndian.Uint16(data[28:30]),
		DstPort:       binary.LittleEndian.Uint16(data[30:32]),
		ProtocolType:  data[32],
		EventType:     data[33],
		StatusCode:    binary.LittleEndian.Uint16(data[34:36]),
		LatencyUS:     binary.LittleEndian.Uint32(data[36:40]),
		PayloadSize:   binary.LittleEndian.Uint32(data[40:44]),
		RequestID:     binary.LittleEndian.Uint32(data[44:48]),
	}
	
	// Parse method if present (fixed 8 bytes)
	if len(data) > 48 {
		methodEnd := 48
		for i := 48; i < len(data) && i < 56; i++ {
			if data[i] == 0 {
				methodEnd = i
				break
			}
		}
		event.Method = string(data[48:methodEnd])
	}
	
	// Parse path if present (fixed 64 bytes)
	if len(data) > 56 {
		pathEnd := 56
		for i := 56; i < len(data) && i < 120; i++ {
			if data[i] == 0 {
				pathEnd = i
				break
			}
		}
		event.Path = string(data[56:pathEnd])
	}
	
	// Parse user agent if present
	if len(data) > 120 {
		uaEnd := 120
		for i := 120; i < len(data) && i < 184; i++ {
			if data[i] == 0 {
				uaEnd = i
				break
			}
		}
		event.UserAgent = string(data[120:uaEnd])
	}
	
	// Parse command
	if len(data) > 184 {
		cmdEnd := 184
		for i := 184; i < len(data) && i < 200; i++ {
			if data[i] == 0 {
				cmdEnd = i
				break
			}
		}
		event.Command = string(data[184:cmdEnd])
	}
	
	// Parse container ID
	if len(data) > 200 {
		cidEnd := 200
		for i := 200; i < len(data) && i < 212; i++ {
			if data[i] == 0 {
				cidEnd = i
				break
			}
		}
		event.ContainerID = string(data[200:cidEnd])
	}
	
	return event, nil
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
	if len(data) < 24 {
		return nil, fmt.Errorf("insufficient data for system event: %d bytes", len(data))
	}
	
	// SystemEvent is already defined in enhanced_collector.go
	// This parser would need to handle the specific binary format
	// For now, return a minimal implementation
	return &SystemEvent{
		Type:      "system",
		Timestamp: time.Now(),
		PID:       binary.LittleEndian.Uint32(data[0:4]),
		Data:      data,
	}, nil
}

func (p *SystemEventParser) EventType() string {
	return "system"
}

// CPUEventParser parses CPU events (for future implementation)
type CPUEventParser struct{}

func NewCPUEventParser() *CPUEventParser {
	return &CPUEventParser{}
}

func (p *CPUEventParser) Parse(data []byte) (interface{}, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("insufficient data for CPU event: %d bytes", len(data))
	}
	
	event := &CPUEvent{
		Timestamp:      time.Now(),
		CPU:            binary.LittleEndian.Uint32(data[0:4]),
		PID:            binary.LittleEndian.Uint32(data[4:8]),
		TGID:           binary.LittleEndian.Uint32(data[8:12]),
		EventType:      data[12],
		RunQueueLength: binary.LittleEndian.Uint32(data[16:20]),
		LatencyNS:      binary.LittleEndian.Uint64(data[20:28]),
	}
	
	return event, nil
}

func (p *CPUEventParser) EventType() string {
	return "cpu"
}

// IOEventParser parses I/O events (for future implementation)
type IOEventParser struct{}

func NewIOEventParser() *IOEventParser {
	return &IOEventParser{}
}

func (p *IOEventParser) Parse(data []byte) (interface{}, error) {
	if len(data) < 40 {
		return nil, fmt.Errorf("insufficient data for I/O event: %d bytes", len(data))
	}
	
	event := &IOEvent{
		Timestamp:   time.Now(),
		PID:         binary.LittleEndian.Uint32(data[0:4]),
		TGID:        binary.LittleEndian.Uint32(data[4:8]),
		EventType:   data[8],
		DeviceMajor: binary.LittleEndian.Uint32(data[12:16]),
		DeviceMinor: binary.LittleEndian.Uint32(data[16:20]),
		Sector:      binary.LittleEndian.Uint64(data[20:28]),
		BytesCount:  binary.LittleEndian.Uint32(data[28:32]),
		LatencyNS:   binary.LittleEndian.Uint64(data[32:40]),
	}
	
	return event, nil
}

func (p *IOEventParser) EventType() string {
	return "io"
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