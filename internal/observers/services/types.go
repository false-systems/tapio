package services

import (
	"fmt"
	"time"
)

// ConnectionEventType represents the type of connection event
type ConnectionEventType uint8

const (
	ConnectionConnect ConnectionEventType = 1 // tcp_connect
	ConnectionAccept  ConnectionEventType = 2 // tcp_accept
	ConnectionClose   ConnectionEventType = 3 // tcp_close
)

// ConnectionEvent represents a raw TCP connection event from eBPF
type ConnectionEvent struct {
	// Core event info
	Timestamp uint64              // Kernel timestamp (nanoseconds)
	EventType ConnectionEventType // Connect/Accept/Close
	Direction uint8               // 0=outbound, 1=inbound

	// Connection details
	SrcIP   [16]byte // Source IP (v4 or v6)
	DstIP   [16]byte // Destination IP (v4 or v6)
	SrcPort uint16   // Source port
	DstPort uint16   // Destination port
	Family  uint16   // AF_INET or AF_INET6

	// Process context
	PID      uint32   // Process ID
	TID      uint32   // Thread ID
	UID      uint32   // User ID
	GID      uint32   // Group ID
	CgroupID uint64   // Cgroup ID (for K8s pod mapping)
	Comm     [16]byte // Process name

	// Network namespace
	NetNS uint32 // Network namespace inode
}

// String returns event type as string
func (t ConnectionEventType) String() string {
	switch t {
	case ConnectionConnect:
		return "connect"
	case ConnectionAccept:
		return "accept"
	case ConnectionClose:
		return "close"
	default:
		return "unknown"
	}
}

// GetComm extracts the process command from the byte array
func (e *ConnectionEvent) GetComm() string {
	for i, b := range e.Comm {
		if b == 0 {
			return string(e.Comm[:i])
		}
	}
	return string(e.Comm[:])
}

// GetSrcIPString returns source IP as string
func (e *ConnectionEvent) GetSrcIPString() string {
	return ipBytesToString(e.SrcIP[:], e.Family)
}

// GetDstIPString returns destination IP as string
func (e *ConnectionEvent) GetDstIPString() string {
	return ipBytesToString(e.DstIP[:], e.Family)
}

// GetTimestamp returns timestamp as time.Time
func (e *ConnectionEvent) GetTimestamp() time.Time {
	return time.Unix(0, int64(e.Timestamp))
}

// IsInbound returns true if this is an inbound connection
func (e *ConnectionEvent) IsInbound() bool {
	return e.Direction == 1
}

// IsOutbound returns true if this is an outbound connection
func (e *ConnectionEvent) IsOutbound() bool {
	return e.Direction == 0
}

// ConnectionKey uniquely identifies a connection
type ConnectionKey struct {
	SrcIP   string
	DstIP   string
	SrcPort uint16
	DstPort uint16
	PID     uint32
}

// String returns a string representation of the connection key
func (k ConnectionKey) String() string {
	return fmt.Sprintf("%s:%d->%s:%d@%d", k.SrcIP, k.SrcPort, k.DstIP, k.DstPort, k.PID)
}

// ActiveConnection represents a tracked connection
type ActiveConnection struct {
	Key         ConnectionKey
	StartTime   time.Time
	LastSeen    time.Time
	BytesSent   uint64
	BytesRecv   uint64
	State       ConnectionState
	ProcessName string
	CgroupID    uint64
	NetNS       uint32
}

// ConnectionState represents the state of a connection
type ConnectionState uint8

const (
	StateActive ConnectionState = 1
	StateClosed ConnectionState = 2
)

// ConnectionStats tracks connection statistics
type ConnectionStats struct {
	ActiveConnections uint64
	TotalConnects     uint64
	TotalAccepts      uint64
	TotalCloses       uint64
	LastEventTime     time.Time
}

// Helper function to convert IP bytes to string
func ipBytesToString(ip []byte, family uint16) string {
	if family == 2 { // AF_INET (IPv4)
		if len(ip) >= 4 {
			return fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3])
		}
		return "unknown"
	} else if family == 10 { // AF_INET6 (IPv6)
		if len(ip) >= 16 {
			// Format IPv6 properly
			return fmt.Sprintf("%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x",
				ip[0], ip[1], ip[2], ip[3], ip[4], ip[5], ip[6], ip[7],
				ip[8], ip[9], ip[10], ip[11], ip[12], ip[13], ip[14], ip[15])
		}
		return "ipv6"
	}
	return "unknown"
}
