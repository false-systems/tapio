//go:build linux
// +build linux

package container

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

// TestExitClassification_OOMKill verifies OOM kill detection
func TestExitClassification_OOMKill(t *testing.T) {
	result := ClassifyExit(137, 9, true)

	assert.Equal(t, ExitCategoryOOMKill, result.Category)
	assert.Equal(t, int32(137), result.ExitCode)
	assert.Equal(t, int32(9), result.Signal)
	assert.Contains(t, result.Evidence, "oom_kill event detected")
}

// TestExitClassification_NormalExitCode verifies exit code 0 is normal
func TestExitClassification_NormalExitCode(t *testing.T) {
	result := ClassifyExit(0, 0, false)

	assert.Equal(t, ExitCategoryNormal, result.Category)
	assert.Equal(t, int32(0), result.ExitCode)
	assert.Equal(t, int32(0), result.Signal)
	assert.Contains(t, result.Evidence, "exit_code=0")
}

// TestExitClassification_NormalSIGTERM verifies SIGTERM is normal
func TestExitClassification_NormalSIGTERM(t *testing.T) {
	result := ClassifyExit(0, 15, false)

	assert.Equal(t, ExitCategoryNormal, result.Category)
	assert.Equal(t, int32(0), result.ExitCode)
	assert.Equal(t, int32(15), result.Signal)
	assert.Contains(t, result.Evidence, "SIGTERM (clean shutdown)")
}

// TestExitClassification_ErrorNonZero verifies non-zero exit is error
func TestExitClassification_ErrorNonZero(t *testing.T) {
	result := ClassifyExit(1, 0, false)

	assert.Equal(t, ExitCategoryError, result.Category)
	assert.Equal(t, int32(1), result.ExitCode)
	assert.Equal(t, int32(0), result.Signal)
	assert.Contains(t, result.Evidence, "exit_code=1")
}

// TestExitClassification_ErrorSIGKILL verifies SIGKILL is error
func TestExitClassification_ErrorSIGKILL(t *testing.T) {
	result := ClassifyExit(137, 9, false)

	assert.Equal(t, ExitCategoryError, result.Category)
	assert.Equal(t, int32(137), result.ExitCode)
	assert.Equal(t, int32(9), result.Signal)
	assert.Contains(t, result.Evidence, "signal=9")
}

// TestExitClassification_OOMPriority verifies OOM takes priority over error
func TestExitClassification_OOMPriority(t *testing.T) {
	// Even with error exit code, OOM flag takes priority
	result := ClassifyExit(137, 9, true)

	assert.Equal(t, ExitCategoryOOMKill, result.Category)
	assert.NotContains(t, result.Evidence, "signal=9")
	assert.Contains(t, result.Evidence, "oom_kill event detected")
}

// TestContainerEventBPF_Size verifies struct size for binary.Read compatibility
func TestContainerEventBPF_Size(t *testing.T) {
	var evt ContainerEventBPF
	size := unsafe.Sizeof(evt)

	// Go aligns struct size to its largest field (uint64 = 8 bytes)
	// Actual fields: 3*uint64 (24) + 5*uint32/int32 (20) + [256]byte (256) = 300 bytes
	// Go rounds up to multiple of 8: 300 → 304 bytes
	expectedSize := uintptr(304)
	assert.Equal(t, expectedSize, size, "ContainerEventBPF struct size (with Go padding)")

	// Verify we can still read 300 bytes from C (we'll handle the 4-byte difference in code)
	minSize := uintptr(300)
	assert.GreaterOrEqual(t, size, minSize, "Struct must be at least 300 bytes for C compatibility")
}

// TestContainerEventBPF_FieldOffsets verifies field alignment matches C struct
func TestContainerEventBPF_FieldOffsets(t *testing.T) {
	var evt ContainerEventBPF

	// Reordered to avoid padding: uint64 fields first, then uint32/int32, then byte array
	assert.Equal(t, uintptr(0), unsafe.Offsetof(evt.MemoryLimit), "MemoryLimit offset")
	assert.Equal(t, uintptr(8), unsafe.Offsetof(evt.MemoryUsage), "MemoryUsage offset")
	assert.Equal(t, uintptr(16), unsafe.Offsetof(evt.TimestampNs), "TimestampNs offset")
	assert.Equal(t, uintptr(24), unsafe.Offsetof(evt.Type), "Type offset")
	assert.Equal(t, uintptr(28), unsafe.Offsetof(evt.PID), "PID offset")
	assert.Equal(t, uintptr(32), unsafe.Offsetof(evt.TID), "TID offset")
	assert.Equal(t, uintptr(36), unsafe.Offsetof(evt.ExitCode), "ExitCode offset")
	assert.Equal(t, uintptr(40), unsafe.Offsetof(evt.Signal), "Signal offset")
	assert.Equal(t, uintptr(44), unsafe.Offsetof(evt.CgroupPath), "CgroupPath offset")
}

// TestConstants_EventTypes verifies event type constants
func TestConstants_EventTypes(t *testing.T) {
	assert.Equal(t, uint32(0), EventTypeOOMKill, "EventTypeOOMKill")
	assert.Equal(t, uint32(1), EventTypeExit, "EventTypeExit")
}
