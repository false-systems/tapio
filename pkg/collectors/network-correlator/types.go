package networkcorrelator

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// Event types - only failures matter
const (
	EventTCPSYNTimeout   = 1  // SYN sent, no SYN-ACK
	EventTCPReset        = 2  // Connection refused
	EventARPTimeout      = 3  // ARP request, no reply
	EventICMPUnreachable = 4  // Host/port unreachable
	EventFINNoACK        = 5  // FIN sent, no ACK (half-closed)
	EventOrphanACK       = 6  // ACK without SYN (connection hijack?)
	EventOrphanRST       = 7  // RST for unknown connection
	EventDupSYN          = 8  // Duplicate SYNs (retry storm)
	EventBlackHole       = 9  // Packets disappear (no response at all)
	EventWrongDirection  = 10 // Packet flow in wrong direction
	EventTTLExpired      = 11 // Routing loops
)

// Failure codes
const (
	TimeoutNoResponse = 1
	ARPNoResponse     = 2
	ConnectionRefused = 3
	HostUnreachable   = 4
	PortUnreachable   = 5
)

// NetworkEvent represents a network failure event
type NetworkEvent struct {
	Timestamp time.Time
	EventType uint32

	// L2 info
	SrcMAC [6]byte
	DstMAC [6]byte

	// L3/L4 info
	SrcIP    net.IP
	DstIP    net.IP
	SrcPort  uint16
	DstPort  uint16
	Protocol uint8

	// Failure info
	FailureCode uint32
	Duration    time.Duration

	// Context
	CgroupID uint64
	NetnsID  uint32
	Comm     string
}

// FailureType returns a string description of the failure
func (e *NetworkEvent) FailureType() string {
	switch e.EventType {
	case EventTCPSYNTimeout:
		return "SYN Timeout"
	case EventTCPReset:
		return "Connection Reset"
	case EventARPTimeout:
		return "ARP Timeout"
	case EventICMPUnreachable:
		return "ICMP Unreachable"
	case EventFINNoACK:
		return "FIN No ACK"
	case EventOrphanACK:
		return "Orphan ACK"
	case EventOrphanRST:
		return "Orphan RST"
	case EventDupSYN:
		return "Duplicate SYN"
	case EventBlackHole:
		return "Black Hole"
	case EventWrongDirection:
		return "Wrong Direction"
	case EventTTLExpired:
		return "TTL Expired"
	default:
		return fmt.Sprintf("Unknown(%d)", e.EventType)
	}
}

// String returns a string representation of the event
func (e *NetworkEvent) String() string {
	return fmt.Sprintf("%s: %s:%d -> %s:%d (%s)",
		e.FailureType(),
		e.SrcIP, e.SrcPort,
		e.DstIP, e.DstPort,
		e.Duration)
}

// SYNAttempt tracks a pending SYN packet
type SYNAttempt struct {
	Timestamp time.Time
	SrcIP     net.IP
	DstIP     net.IP
	SrcPort   uint16
	DstPort   uint16
	SrcMAC    [6]byte
	DstMAC    [6]byte
	CgroupID  uint64
}

// ARPRequest tracks a pending ARP request
type ARPRequest struct {
	Timestamp    time.Time
	RequesterIP  net.IP
	TargetIP     net.IP
	RequesterMAC [6]byte
}

// PolicyInfo represents a NetworkPolicy
type PolicyInfo struct {
	Name      string
	Namespace string
	Rules     []PolicyRule
}

// PolicyRule represents a single rule in a NetworkPolicy
type PolicyRule struct {
	Direction string // ingress/egress
	From      []string
	To        []string
	Ports     []uint16
	Action    string // allow/deny
}

// ServiceInfo represents a Kubernetes service
type ServiceInfo struct {
	Name          string
	Namespace     string
	ClusterIP     net.IP
	Ports         []uint16
	EndpointCount int
}

// PodInfo represents a Kubernetes pod
type PodInfo struct {
	Name        string
	Namespace   string
	IP          net.IP
	Labels      map[string]string
	ServiceName string
}

// ConnHash generates a hash for connection tracking
func ConnHash(sip, dip net.IP, sport, dport uint16) uint64 {
	hash := uint64(0)

	// Hash source IP
	if sip4 := sip.To4(); sip4 != nil {
		hash = uint64(binary.BigEndian.Uint32(sip4))
	}

	// Hash dest IP
	if dip4 := dip.To4(); dip4 != nil {
		hash = (hash << 16) ^ uint64(binary.BigEndian.Uint32(dip4))
	}

	// Add ports
	hash = (hash << 8) ^ uint64(sport)
	hash = (hash << 8) ^ uint64(dport)

	return hash
}
