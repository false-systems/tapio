//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"encoding/binary"
	"fmt"
	"time"
	"unsafe"
)

// C struct layouts for parsing raw events from eBPF ring buffers
// These must match the structs defined in the eBPF C programs

// Network event from network_monitor.c
type rawNetworkEvent struct {
	Timestamp   uint64
	PID         uint32
	TGID        uint32
	UID         uint32
	SrcIP       uint32
	DstIP       uint32
	SrcPort     uint16
	DstPort     uint16
	Protocol    uint8
	EventType   uint8
	LatencyUS   uint32
	Bytes       uint32
	ErrorCode   uint16
	Comm        [16]byte
	ContainerID [64]byte
}

// Packet event from packet_analyzer.c
type rawPacketEvent struct {
	Timestamp   uint64
	PID         uint32
	TGID        uint32
	SrcIP       uint32
	DstIP       uint32
	SrcPort     uint16
	DstPort     uint16
	Protocol    uint8
	EventType   uint8
	LatencyUS   uint32
	PacketSize  uint32
	SequenceNum uint32
	AckNum      uint32
	WindowSize  uint16
	TCPFlags    uint8
	Comm        [16]byte
	Interface   [16]byte
}

// DNS event from dns_monitor.c
type rawDNSEvent struct {
	Timestamp    uint64
	PID          uint32
	TGID         uint32
	UID          uint32
	SrcIP        uint32
	DstIP        uint32
	QueryID      uint16
	QueryType    uint16
	QueryClass   uint16
	EventType    uint8
	ResponseCode uint8
	LatencyMS    uint32
	AnswerCount  uint32
	Domain       [256]byte
	Comm         [16]byte
	ContainerID  [64]byte
}

// Protocol event from protocol_analyzer.c
type rawProtocolEvent struct {
	Timestamp    uint64
	PID          uint32
	TGID         uint32
	UID          uint32
	SrcIP        uint32
	DstIP        uint32
	SrcPort      uint16
	DstPort      uint16
	ProtocolType uint8
	EventType    uint8
	StatusCode   uint16
	LatencyUS    uint32
	PayloadSize  uint32
	RequestID    uint32
	Method       [16]byte
	Path         [128]byte
	UserAgent    [64]byte
	ErrorMsg     [128]byte
	Comm         [16]byte
	ContainerID  [64]byte
}

// parseRawNetworkEvent parses a raw network event from the eBPF ring buffer
func parseRawNetworkEvent(data []byte) (*NetworkEvent, error) {
	if len(data) < int(unsafe.Sizeof(rawNetworkEvent{})) {
		return nil, fmt.Errorf("insufficient data for network event: got %d bytes, need %d", 
			len(data), unsafe.Sizeof(rawNetworkEvent{}))
	}

	var raw rawNetworkEvent
	if err := binary.Read(newBytesReader(data), binary.LittleEndian, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse network event: %w", err)
	}

	event := &NetworkEvent{
		Timestamp:   time.Unix(0, int64(raw.Timestamp)),
		PID:         raw.PID,
		TGID:        raw.TGID,
		UID:         raw.UID,
		SrcIP:       raw.SrcIP,
		DstIP:       raw.DstIP,
		SrcPort:     raw.SrcPort,
		DstPort:     raw.DstPort,
		Protocol:    raw.Protocol,
		EventType:   raw.EventType,
		LatencyUS:   raw.LatencyUS,
		Bytes:       raw.Bytes,
		ErrorCode:   raw.ErrorCode,
		Command:     cStringToString(raw.Comm[:]),
		ContainerID: cStringToString(raw.ContainerID[:]),
	}

	return event, nil
}

// parseRawPacketEvent parses a raw packet event from the eBPF ring buffer
func parseRawPacketEvent(data []byte) (*PacketEvent, error) {
	if len(data) < int(unsafe.Sizeof(rawPacketEvent{})) {
		return nil, fmt.Errorf("insufficient data for packet event: got %d bytes, need %d", 
			len(data), unsafe.Sizeof(rawPacketEvent{}))
	}

	var raw rawPacketEvent
	if err := binary.Read(newBytesReader(data), binary.LittleEndian, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse packet event: %w", err)
	}

	event := &PacketEvent{
		Timestamp:   time.Unix(0, int64(raw.Timestamp)),
		PID:         raw.PID,
		TGID:        raw.TGID,
		SrcIP:       raw.SrcIP,
		DstIP:       raw.DstIP,
		SrcPort:     raw.SrcPort,
		DstPort:     raw.DstPort,
		Protocol:    raw.Protocol,
		EventType:   raw.EventType,
		LatencyUS:   raw.LatencyUS,
		PacketSize:  raw.PacketSize,
		SequenceNum: raw.SequenceNum,
		AckNum:      raw.AckNum,
		WindowSize:  raw.WindowSize,
		TCPFlags:    raw.TCPFlags,
		Command:     cStringToString(raw.Comm[:]),
		Interface:   cStringToString(raw.Interface[:]),
	}

	return event, nil
}

// parseRawDNSEvent parses a raw DNS event from the eBPF ring buffer
func parseRawDNSEvent(data []byte) (*DNSEvent, error) {
	if len(data) < int(unsafe.Sizeof(rawDNSEvent{})) {
		return nil, fmt.Errorf("insufficient data for DNS event: got %d bytes, need %d", 
			len(data), unsafe.Sizeof(rawDNSEvent{}))
	}

	var raw rawDNSEvent
	if err := binary.Read(newBytesReader(data), binary.LittleEndian, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse DNS event: %w", err)
	}

	event := &DNSEvent{
		Timestamp:    time.Unix(0, int64(raw.Timestamp)),
		PID:          raw.PID,
		TGID:         raw.TGID,
		UID:          raw.UID,
		SrcIP:        raw.SrcIP,
		DstIP:        raw.DstIP,
		QueryID:      raw.QueryID,
		QueryType:    raw.QueryType,
		QueryClass:   raw.QueryClass,
		EventType:    raw.EventType,
		ResponseCode: raw.ResponseCode,
		LatencyMS:    raw.LatencyMS,
		AnswerCount:  raw.AnswerCount,
		Domain:       cStringToString(raw.Domain[:]),
		Command:      cStringToString(raw.Comm[:]),
		ContainerID:  cStringToString(raw.ContainerID[:]),
	}

	return event, nil
}

// parseRawProtocolEvent parses a raw protocol event from the eBPF ring buffer
func parseRawProtocolEvent(data []byte) (*ProtocolEvent, error) {
	if len(data) < int(unsafe.Sizeof(rawProtocolEvent{})) {
		return nil, fmt.Errorf("insufficient data for protocol event: got %d bytes, need %d", 
			len(data), unsafe.Sizeof(rawProtocolEvent{}))
	}

	var raw rawProtocolEvent
	if err := binary.Read(newBytesReader(data), binary.LittleEndian, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse protocol event: %w", err)
	}

	event := &ProtocolEvent{
		Timestamp:    time.Unix(0, int64(raw.Timestamp)),
		PID:          raw.PID,
		TGID:         raw.TGID,
		UID:          raw.UID,
		SrcIP:        raw.SrcIP,
		DstIP:        raw.DstIP,
		SrcPort:      raw.SrcPort,
		DstPort:      raw.DstPort,
		ProtocolType: raw.ProtocolType,
		EventType:    raw.EventType,
		StatusCode:   raw.StatusCode,
		LatencyUS:    raw.LatencyUS,
		PayloadSize:  raw.PayloadSize,
		RequestID:    raw.RequestID,
		Method:       cStringToString(raw.Method[:]),
		Path:         cStringToString(raw.Path[:]),
		UserAgent:    cStringToString(raw.UserAgent[:]),
		ErrorMsg:     cStringToString(raw.ErrorMsg[:]),
		Command:      cStringToString(raw.Comm[:]),
		ContainerID:  cStringToString(raw.ContainerID[:]),
	}

	return event, nil
}

// Helper functions

// cStringToString converts a null-terminated C string byte array to a Go string
func cStringToString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// newBytesReader creates a bytes.Reader equivalent for binary parsing
func newBytesReader(data []byte) *bytesReader {
	return &bytesReader{data: data, pos: 0}
}

type bytesReader struct {
	data []byte
	pos  int
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, fmt.Errorf("EOF")
	}
	
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// Event type constants for network events
const (
	NetConnEstablished = 1
	NetConnClosed      = 2
	NetConnFailed      = 3
	NetPacketDrop      = 4
	NetHighLatency     = 5
	NetRetransmit      = 6
)

// Event type constants for packet events
const (
	PktLoss       = 1
	PktHighLatency = 2
	PktReorder    = 3
	PktDuplicate  = 4
	PktCorruption = 5
)

// Event type constants for DNS events
const (
	DNSQuery     = 1
	DNSResponse  = 2
	DNSTimeout   = 3
	DNSError     = 4
	DNSNXDomain  = 5
)

// Event type constants for protocol events
const (
	ProtoRequest  = 1
	ProtoResponse = 2
	ProtoError    = 3
	ProtoTimeout  = 4
	ProtoSlow     = 5
)

// Protocol type constants
const (
	ProtoHTTP      = 1
	ProtoHTTPS     = 2
	ProtoGRPC      = 3
	ProtoMySQL     = 4
	ProtoPostgres  = 5
	ProtoRedis     = 6
	ProtoUnknown   = 7
)

// Network protocol constants
const (
	ProtocolTCP = 6
	ProtocolUDP = 17
)

// Helper functions for event interpretation

// GetNetworkEventTypeString returns a human-readable string for network event types
func GetNetworkEventTypeString(eventType uint8) string {
	switch eventType {
	case NetConnEstablished:
		return "connection_established"
	case NetConnClosed:
		return "connection_closed"
	case NetConnFailed:
		return "connection_failed"
	case NetPacketDrop:
		return "packet_drop"
	case NetHighLatency:
		return "high_latency"
	case NetRetransmit:
		return "retransmit"
	default:
		return "unknown"
	}
}

// GetPacketEventTypeString returns a human-readable string for packet event types
func GetPacketEventTypeString(eventType uint8) string {
	switch eventType {
	case PktLoss:
		return "packet_loss"
	case PktHighLatency:
		return "high_latency"
	case PktReorder:
		return "reorder"
	case PktDuplicate:
		return "duplicate"
	case PktCorruption:
		return "corruption"
	default:
		return "unknown"
	}
}

// GetDNSEventTypeString returns a human-readable string for DNS event types
func GetDNSEventTypeString(eventType uint8) string {
	switch eventType {
	case DNSQuery:
		return "query"
	case DNSResponse:
		return "response"
	case DNSTimeout:
		return "timeout"
	case DNSError:
		return "error"
	case DNSNXDomain:
		return "nxdomain"
	default:
		return "unknown"
	}
}

// GetProtocolEventTypeString returns a human-readable string for protocol event types
func GetProtocolEventTypeString(eventType uint8) string {
	switch eventType {
	case ProtoRequest:
		return "request"
	case ProtoResponse:
		return "response"
	case ProtoError:
		return "error"
	case ProtoTimeout:
		return "timeout"
	case ProtoSlow:
		return "slow"
	default:
		return "unknown"
	}
}

// GetProtocolTypeString returns a human-readable string for protocol types
func GetProtocolTypeString(protocolType uint8) string {
	switch protocolType {
	case ProtoHTTP:
		return "HTTP"
	case ProtoHTTPS:
		return "HTTPS"
	case ProtoGRPC:
		return "gRPC"
	case ProtoMySQL:
		return "MySQL"
	case ProtoPostgres:
		return "PostgreSQL"
	case ProtoRedis:
		return "Redis"
	default:
		return "Unknown"
	}
}

// GetHTTPStatusCategory returns the category for an HTTP status code
func GetHTTPStatusCategory(statusCode uint16) string {
	switch {
	case statusCode >= 100 && statusCode < 200:
		return "Informational"
	case statusCode >= 200 && statusCode < 300:
		return "Success"
	case statusCode >= 300 && statusCode < 400:
		return "Redirection"
	case statusCode >= 400 && statusCode < 500:
		return "Client Error"
	case statusCode >= 500 && statusCode < 600:
		return "Server Error"
	default:
		return "Unknown"
	}
}

// IsHTTPError returns true if the status code indicates an error
func IsHTTPError(statusCode uint16) bool {
	return statusCode >= 400
}

// FormatIPAddress converts a uint32 IP address to a string
func FormatIPAddress(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		ip&0xFF,
		(ip>>8)&0xFF,
		(ip>>16)&0xFF,
		(ip>>24)&0xFF)
}

// FormatLatency formats latency with appropriate units
func FormatLatency(latencyUS uint32) string {
	if latencyUS < 1000 {
		return fmt.Sprintf("%dµs", latencyUS)
	} else if latencyUS < 1000000 {
		return fmt.Sprintf("%.1fms", float64(latencyUS)/1000)
	} else {
		return fmt.Sprintf("%.2fs", float64(latencyUS)/1000000)
	}
}

// FormatBytes formats byte counts with appropriate units
func FormatBytes(bytes uint32) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	} else if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	} else {
		return fmt.Sprintf("%.2fGB", float64(bytes)/(1024*1024*1024))
	}
}