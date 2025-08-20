package kernel

// KernelEvent represents a kernel event from eBPF - must match C struct
type KernelEvent struct {
	Timestamp   uint64
	PID         uint32
	PPID        uint32
	UID         uint32
	GID         uint32
	CgroupID    uint64
	EventType   uint8
	Pad         [3]uint8
	Comm        [16]byte
	ServiceName [64]byte
	CgroupPath  [256]byte
	ExitCode    uint32
	Signal      uint32
}

// KernelEventData represents processed kernel event data
type KernelEventData struct {
	PID       uint32
	PPID      uint32
	UID       uint32
	GID       uint32
	CgroupID  uint64
	EventType uint8
	Comm      string
}
