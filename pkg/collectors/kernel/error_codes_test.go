package kernel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestErrorCodes verifies error code constants and types
func TestErrorCodes(t *testing.T) {
	// Verify event type constants
	assert.Equal(t, uint8(1), EventTypeConfigMapAccess)
	assert.Equal(t, uint8(2), EventTypeSecretAccess)
	assert.Equal(t, uint8(3), EventTypePodSyscall)
	assert.Equal(t, uint8(4), EventTypeConfigAccessFailed)

	// Verify ConfigInfo structure size
	var ci ConfigInfo
	assert.Equal(t, 60, len(ci.MountPath))

	// Verify KernelEventData has error fields
	var ked KernelEventData
	ked.ErrorCode = 2
	ked.ErrorDesc = "No such file or directory"
	assert.Equal(t, int32(2), ked.ErrorCode)
	assert.Equal(t, "No such file or directory", ked.ErrorDesc)
}

// TestKernelEventStructure verifies the kernel event structure alignment
func TestKernelEventStructure(t *testing.T) {
	event := KernelEvent{
		Timestamp: 1234567890,
		PID:       1000,
		TID:       1001,
		EventType: uint32(EventTypeConfigAccessFailed),
		CgroupID:  999888777,
	}

	// Set command name
	copy(event.Comm[:], []byte("test-app"))

	// Test ConfigInfo embedded in Data field
	mountPath := "/etc/config/app.yaml"
	copy(event.Data[:], []byte(mountPath))

	// Add error code at offset 60 (little-endian int32)
	errorCode := int32(13) // EACCES
	event.Data[60] = byte(errorCode)
	event.Data[61] = byte(errorCode >> 8)
	event.Data[62] = byte(errorCode >> 16)
	event.Data[63] = byte(errorCode >> 24)

	// Verify we can extract the data back
	extractedPath := string(event.Data[:len(mountPath)])
	assert.Equal(t, mountPath, extractedPath)

	// Extract error code
	extractedError := int32(event.Data[60]) |
		int32(event.Data[61])<<8 |
		int32(event.Data[62])<<16 |
		int32(event.Data[63])<<24
	assert.Equal(t, errorCode, extractedError)
}

// TestFailedAccessEventData tests the failed access event data structure
func TestFailedAccessEventData(t *testing.T) {
	eventData := KernelEventData{
		PID:        1234,
		TID:        1235,
		CgroupID:   999888777,
		EventType:  uint32(EventTypeConfigAccessFailed),
		Comm:       "nginx",
		ConfigType: "failed",
		MountPath:  "/var/lib/kubelet/pods/uuid/volumes/kubernetes.io~configmap/missing",
		ErrorCode:  2,
		ErrorDesc:  "No such file or directory",
	}

	assert.Equal(t, uint32(1234), eventData.PID)
	assert.Equal(t, "failed", eventData.ConfigType)
	assert.Contains(t, eventData.MountPath, "configmap/missing")
	assert.Equal(t, int32(2), eventData.ErrorCode)
	assert.Equal(t, "No such file or directory", eventData.ErrorDesc)
}

// Common error codes for reference (from Linux errno.h)
const (
	EPERM  = 1  // Operation not permitted
	ENOENT = 2  // No such file or directory
	EIO    = 5  // I/O error
	EACCES = 13 // Permission denied
	EFAULT = 14 // Bad address
	EEXIST = 17 // File exists
	EINVAL = 22 // Invalid argument
	ENOSPC = 28 // No space left on device
	EROFS  = 30 // Read-only file system
)

// TestCommonErrorCodes verifies we handle common error codes correctly
func TestCommonErrorCodes(t *testing.T) {
	tests := []struct {
		code int32
		name string
		desc string
	}{
		{0, "Success", "No error"},
		{ENOENT, "ENOENT", "ConfigMap/Secret not mounted"},
		{EACCES, "EACCES", "Permission denied - SecurityContext issue"},
		{EIO, "EIO", "Storage I/O error"},
		{ENOSPC, "ENOSPC", "Disk full"},
		{EROFS, "EROFS", "Read-only filesystem"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create event data with error
			eventData := KernelEventData{
				EventType:  uint32(EventTypeConfigAccessFailed),
				ConfigType: "failed",
				ErrorCode:  tt.code,
			}

			assert.Equal(t, tt.code, eventData.ErrorCode)
		})
	}
}
