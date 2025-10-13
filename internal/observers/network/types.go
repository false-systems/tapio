package network

// NetworkEventBPF field ordering matches the C struct layout exactly (70 bytes of data).
// Go cannot enforce packed structs; the Go struct size is 72 bytes due to trailing alignment padding.
// binary.Read handles the padding when reading from binary data.
type NetworkEventBPF struct {
	PID      uint32   // offset 0, size 4
	SrcIP    uint32   // offset 4, size 4
	DstIP    uint32   // offset 8, size 4
	SrcIPv6  [16]byte // offset 12, size 16
	DstIPv6  [16]byte // offset 28, size 16
	SrcPort  uint16   // offset 44, size 2
	DstPort  uint16   // offset 46, size 2
	Family   uint16   // offset 48, size 2
	Protocol uint8    // offset 50, size 1
	OldState uint8    // offset 51, size 1
	NewState uint8    // offset 52, size 1
	Pad      uint8    // offset 53, size 1 - MUST match C padding
	Comm     [16]byte // offset 54, size 16
}

// Address families (from linux/socket.h)
const (
	AF_INET  = 2
	AF_INET6 = 10
)

// IP protocols (from linux/in.h)
const (
	IPPROTO_TCP = 6
	IPPROTO_UDP = 17
)

// TCP states (from linux/tcp.h)
const (
	TCP_ESTABLISHED = 1
	TCP_SYN_SENT    = 2
	TCP_SYN_RECV    = 3
	TCP_FIN_WAIT1   = 4
	TCP_FIN_WAIT2   = 5
	TCP_TIME_WAIT   = 6
	TCP_CLOSE       = 7
	TCP_CLOSE_WAIT  = 8
	TCP_LAST_ACK    = 9
	TCP_LISTEN      = 10
	TCP_CLOSING     = 11
)
