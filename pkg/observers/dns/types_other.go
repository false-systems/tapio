//go:build !linux
// +build !linux

package dns

// eBPFState is a stub type for non-Linux platforms
type eBPFState struct {
	// Empty struct for non-Linux platforms
}

// BPFDNSEvent is a stub type for non-Linux platforms (for testing)
type BPFDNSEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	UID       uint32
	GID       uint32
	CgroupID  uint64
	EventType uint8
	Protocol  uint8
	IPVersion uint8
	Pad1      uint8
	SrcAddr   [16]byte
	DstAddr   [16]byte
	SrcPort   uint16
	DstPort   uint16
	DNSID     uint16
	DNSFlags  uint16
	Opcode    uint8
	Rcode     uint8
	QType     uint16
	DataLen   uint32
	LatencyNs uint32
	QueryName [128]byte
	Data      [512]byte
}
