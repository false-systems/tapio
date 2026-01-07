package storage

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

func TestStorageEventBPF_Size(t *testing.T) {
	// Verify struct size matches what eBPF will send.
	// This test catches misalignment issues between C and Go.
	var evt StorageEventBPF
	size := unsafe.Sizeof(evt)

	// Expected size calculation:
	// TimestampNs: 8 + LatencyNs: 8 + CgroupID: 8 + Sector: 8 = 32
	// DevMajor: 4 + DevMinor: 4 + Bytes: 4 + PID: 4 = 16
	// ErrorCode: 2 + Opcode: 1 + Severity: 1 = 4
	// Comm: 16
	// Subtotal: 32 + 16 + 4 + 16 = 68 bytes
	// + 4 bytes end padding (struct aligned to 8 bytes for uint64 fields)
	// Total: 72 bytes
	assert.Equal(t, uintptr(72), size, "StorageEventBPF struct size must match C struct")
}

func TestStorageEventBPF_FieldOffsets(t *testing.T) {
	// Verify field offsets match C struct layout.
	var evt StorageEventBPF

	assert.Equal(t, uintptr(0), unsafe.Offsetof(evt.TimestampNs))
	assert.Equal(t, uintptr(8), unsafe.Offsetof(evt.LatencyNs))
	assert.Equal(t, uintptr(16), unsafe.Offsetof(evt.CgroupID))
	assert.Equal(t, uintptr(24), unsafe.Offsetof(evt.Sector))
	assert.Equal(t, uintptr(32), unsafe.Offsetof(evt.DevMajor))
	assert.Equal(t, uintptr(36), unsafe.Offsetof(evt.DevMinor))
	assert.Equal(t, uintptr(40), unsafe.Offsetof(evt.Bytes))
	assert.Equal(t, uintptr(44), unsafe.Offsetof(evt.PID))
	assert.Equal(t, uintptr(48), unsafe.Offsetof(evt.ErrorCode))
	assert.Equal(t, uintptr(50), unsafe.Offsetof(evt.Opcode))
	assert.Equal(t, uintptr(51), unsafe.Offsetof(evt.Severity))
	assert.Equal(t, uintptr(52), unsafe.Offsetof(evt.Comm))
}

func TestOperationName(t *testing.T) {
	assert.Equal(t, "read", OperationName(OpRead))
	assert.Equal(t, "write", OperationName(OpWrite))
}

func TestErrorName(t *testing.T) {
	assert.Equal(t, "EIO", ErrorName(5))
	assert.Equal(t, "ENOSPC", ErrorName(28))
	assert.Equal(t, "UNKNOWN", ErrorName(999))
}

func TestExtractComm(t *testing.T) {
	// Test null-terminated string extraction
	comm := [16]byte{'d', 'd', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	assert.Equal(t, "dd", extractComm(comm))

	// Test full buffer (no null)
	full := [16]byte{'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p'}
	assert.Equal(t, "abcdefghijklmnop", extractComm(full))
}
