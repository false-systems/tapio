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
	assert.Contains(t, result.Evidence, "oom_kill")
}

// TestContainerEventBPF_Size verifies struct size for binary.Read compatibility
func TestContainerEventBPF_Size(t *testing.T) {
	var evt ContainerEventBPF
	size := unsafe.Sizeof(evt)

	// Size should match C struct layout
	// type (u32) + pid (u32) + tid (u32) + exit_code (s32) + signal (s32) +
	// cgroup_path (256) + memory_limit (u64) + memory_usage (u64) + timestamp_ns (u64)
	// = 4 + 4 + 4 + 4 + 4 + 256 + 8 + 8 + 8 = 300 bytes
	expectedSize := uintptr(4 + 4 + 4 + 4 + 4 + 256 + 8 + 8 + 8)
	assert.Equal(t, expectedSize, size, "ContainerEventBPF struct size")
}

// TestContainerEventBPF_FieldOffsets verifies field alignment matches C struct
func TestContainerEventBPF_FieldOffsets(t *testing.T) {
	var evt ContainerEventBPF

	assert.Equal(t, uintptr(0), unsafe.Offsetof(evt.Type), "Type offset")
	assert.Equal(t, uintptr(4), unsafe.Offsetof(evt.PID), "PID offset")
	assert.Equal(t, uintptr(8), unsafe.Offsetof(evt.TID), "TID offset")
	assert.Equal(t, uintptr(12), unsafe.Offsetof(evt.ExitCode), "ExitCode offset")
	assert.Equal(t, uintptr(16), unsafe.Offsetof(evt.Signal), "Signal offset")
	assert.Equal(t, uintptr(20), unsafe.Offsetof(evt.CgroupPath), "CgroupPath offset")
	assert.Equal(t, uintptr(276), unsafe.Offsetof(evt.MemoryLimit), "MemoryLimit offset")
	assert.Equal(t, uintptr(284), unsafe.Offsetof(evt.MemoryUsage), "MemoryUsage offset")
	assert.Equal(t, uintptr(292), unsafe.Offsetof(evt.TimestampNs), "TimestampNs offset")
}

// TestConstants_EventTypes verifies event type constants
func TestConstants_EventTypes(t *testing.T) {
	assert.Equal(t, uint32(0), EventTypeOOMKill, "EventTypeOOMKill")
	assert.Equal(t, uint32(1), EventTypeExit, "EventTypeExit")
}
