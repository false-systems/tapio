//go:build linux

package containerruntime

import (
	"context"
	"testing"
	"time"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/observers/container"
)

// TestNewRingReader_CreatesReader verifies reader initialization
func TestNewRingReader_CreatesReader(t *testing.T) {
	// Create mock ring buffer reader (nil is valid for testing structure)
	var mockRing *ringbuf.Reader

	reader := NewRingReader(mockRing)
	require.NotNil(t, reader, "Reader should be created")
	// Note: ring can be nil for testing - that's what TestRingReader_ReadWithNilRing tests
}

// TestRingReader_ReadWithNilRing verifies error handling for nil ring
func TestRingReader_ReadWithNilRing(t *testing.T) {
	reader := NewRingReader(nil)
	ctx := context.Background()

	record, err := reader.Read(ctx)
	require.Error(t, err, "Read should fail with nil ring")
	assert.Nil(t, record, "Record should be nil on error")
	assert.Contains(t, err.Error(), "ring buffer is nil")
}

// TestRingReader_Close verifies cleanup
func TestRingReader_Close(t *testing.T) {
	// Nil ring is safe to close
	reader := NewRingReader(nil)

	err := reader.Close()
	assert.NoError(t, err, "Close should succeed")
}

// TestRingReader_ParseRecord verifies event parsing
func TestRingReader_ParseRecord(t *testing.T) {
	reader := NewRingReader(nil)

	// Create valid ContainerEventBPF bytes
	var evt container.ContainerEventBPF
	evt.Type = container.EventTypeOOMKill
	evt.PID = 12345
	evt.ExitCode = 137
	evt.Signal = 9
	evt.TimestampNs = uint64(time.Now().UnixNano())

	// Marshal to bytes (unsafe cast for testing)
	rawBytes := make([]byte, 304) // ContainerEventBPF size
	copy(rawBytes[0:8], uint64ToBytes(evt.MemoryLimit))
	copy(rawBytes[8:16], uint64ToBytes(evt.MemoryUsage))
	copy(rawBytes[16:24], uint64ToBytes(evt.TimestampNs))
	copy(rawBytes[24:28], uint32ToBytes(evt.Type))
	copy(rawBytes[28:32], uint32ToBytes(evt.PID))

	parsed, err := reader.ParseRecord(rawBytes)
	require.NoError(t, err, "Parse should succeed")
	require.NotNil(t, parsed, "Parsed event should not be nil")

	assert.Equal(t, container.EventTypeOOMKill, parsed.Type)
	assert.Equal(t, uint32(12345), parsed.PID)
}

// TestRingReader_ParseInvalidRecord verifies error handling
func TestRingReader_ParseInvalidRecord(t *testing.T) {
	reader := NewRingReader(nil)

	// Too small buffer
	tooSmall := make([]byte, 10)

	parsed, err := reader.ParseRecord(tooSmall)
	require.Error(t, err, "Parse should fail with small buffer")
	assert.Nil(t, parsed, "Parsed event should be nil on error")
	assert.Contains(t, err.Error(), "invalid record size")
}

// Helper functions for byte conversion
func uint64ToBytes(val uint64) []byte {
	b := make([]byte, 8)
	b[0] = byte(val)
	b[1] = byte(val >> 8)
	b[2] = byte(val >> 16)
	b[3] = byte(val >> 24)
	b[4] = byte(val >> 32)
	b[5] = byte(val >> 40)
	b[6] = byte(val >> 48)
	b[7] = byte(val >> 56)
	return b
}

func uint32ToBytes(val uint32) []byte {
	b := make([]byte, 4)
	b[0] = byte(val)
	b[1] = byte(val >> 8)
	b[2] = byte(val >> 16)
	b[3] = byte(val >> 24)
	return b
}
