package storage

// StorageEventBPF is the event struct received from eBPF ring buffer.
// Must match struct storage_event in bpf/storage_monitor.c exactly.
type StorageEventBPF struct {
	TimestampNs uint64   // Event timestamp (nanoseconds since boot)
	LatencyNs   uint64   // I/O latency (complete - issue)
	CgroupID    uint64   // Container attribution
	Sector      uint64   // Starting sector
	DevMajor    uint32   // Device major number
	DevMinor    uint32   // Device minor number
	Bytes       uint32   // I/O size in bytes
	PID         uint32   // Process ID
	ErrorCode   uint16   // 0 = success, otherwise Linux errno
	Opcode      uint8    // 0=READ, 1=WRITE
	Severity    uint8    // 0=normal, 1=warning, 2=critical
	Comm        [16]byte // Process name (null-terminated)
}

// Operation types from eBPF.
const (
	OpRead  uint8 = 0
	OpWrite uint8 = 1
)

// Severity levels from eBPF.
const (
	SeverityNormal   uint8 = 0
	SeverityWarning  uint8 = 1
	SeverityCritical uint8 = 2
)

// Event subtypes for domain events.
const (
	SubtypeIOLatencySpike = "io_latency_spike"
	SubtypeIOError        = "io_error"
	SubtypeThroughputDrop = "throughput_drop"
)

// Linux errno codes we care about.
var errorNames = map[uint16]string{
	5:  "EIO",    // I/O error
	28: "ENOSPC", // No space left on device
	30: "EROFS",  // Read-only file system
	16: "EBUSY",  // Device or resource busy
	19: "ENODEV", // No such device
}

// ErrorName returns a human-readable error name for a Linux errno.
func ErrorName(code uint16) string {
	if name, ok := errorNames[code]; ok {
		return name
	}
	return "UNKNOWN"
}

// OperationName returns "read" or "write" for the opcode.
func OperationName(opcode uint8) string {
	if opcode == OpRead {
		return "read"
	}
	return "write"
}

// extractComm extracts null-terminated process name from fixed-size array.
func extractComm(comm [16]byte) string {
	for i, b := range comm {
		if b == 0 {
			return string(comm[:i])
		}
	}
	return string(comm[:])
}
