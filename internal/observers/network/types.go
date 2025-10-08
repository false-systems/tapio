package network

// NetworkEventBPF is the Go representation of the BPF event structure.
// IMPORTANT: This struct must match the C struct layout exactly for proper unmarshaling.
type NetworkEventBPF struct {
	PID      uint32
	SrcIP    uint32
	DstIP    uint32
	SrcPort  uint16
	DstPort  uint16
	Protocol uint8
	_        uint8 // Padding (_pad1 in C)
	Comm     [16]byte
	_        uint16 // Padding (_pad2 in C) - aligns to 4-byte boundary
}

// Protocol constants matching IPPROTO_* values
const (
	ProtocolTCP = 6
	ProtocolUDP = 17
)
