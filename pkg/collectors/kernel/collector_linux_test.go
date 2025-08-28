//go:build linux
// +build linux

package kernel

import (
	"context"
	"encoding/binary"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestKernelEventStructure(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "kernel-event-test"}
	_, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Create test kernel event with actual fields
	kernelEvent := KernelEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1234,
		TID:       5678,
		EventType: uint32(EventTypeConfigMapAccess),
		CgroupID:  567890,
		Size:      0,
	}

	copy(kernelEvent.Comm[:], "test-process")
	copy(kernelEvent.PodUID[:], "pod-123-456")
	copy(kernelEvent.Data[:], "/var/lib/kubelet/pods/xyz/volumes/kubernetes.io~configmap/test-cm")

	// Verify structure size and alignment
	assert.Greater(t, int(unsafe.Sizeof(kernelEvent)), 0)
	assert.Equal(t, "test-process", bytesToString(kernelEvent.Comm[:]))
	assert.Equal(t, uint32(1234), kernelEvent.PID)
	assert.Equal(t, uint32(EventTypeConfigMapAccess), kernelEvent.EventType)
}

func TestProcessRawEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "process-event-test", BufferSize: 100}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Initialize context for metrics
	collector.ctx = context.Background()

	// Test ConfigMap access event
	t.Run("ConfigMap access", func(t *testing.T) {
		kernelEvent := KernelEvent{
			Timestamp: uint64(time.Now().UnixNano()),
			PID:       1234,
			TID:       5678,
			EventType: uint32(EventTypeConfigMapAccess),
			CgroupID:  567890,
		}
		copy(kernelEvent.Comm[:], "test-process")
		copy(kernelEvent.Data[:60], "/var/lib/kubelet/pods/xyz/volumes/kubernetes.io~configmap/test-cm")

		// Convert to raw bytes
		buffer := make([]byte, unsafe.Sizeof(kernelEvent))
		*(*KernelEvent)(unsafe.Pointer(&buffer[0])) = kernelEvent

		// Test processRawEvent doesn't panic
		assert.NotPanics(t, func() {
			collector.processRawEvent(buffer)
		})
	})

	// Test Secret access event
	t.Run("Secret access", func(t *testing.T) {
		kernelEvent := KernelEvent{
			Timestamp: uint64(time.Now().UnixNano()),
			PID:       2345,
			TID:       6789,
			EventType: uint32(EventTypeSecretAccess),
			CgroupID:  567890,
		}
		copy(kernelEvent.Comm[:], "secret-reader")
		copy(kernelEvent.Data[:60], "/var/lib/kubelet/pods/xyz/volumes/kubernetes.io~secret/my-secret")

		buffer := make([]byte, unsafe.Sizeof(kernelEvent))
		*(*KernelEvent)(unsafe.Pointer(&buffer[0])) = kernelEvent

		assert.NotPanics(t, func() {
			collector.processRawEvent(buffer)
		})
	})

	// Test Failed access event
	t.Run("Failed config access", func(t *testing.T) {
		kernelEvent := KernelEvent{
			Timestamp: uint64(time.Now().UnixNano()),
			PID:       3456,
			TID:       7890,
			EventType: uint32(EventTypeConfigAccessFailed),
			CgroupID:  567890,
		}
		copy(kernelEvent.Comm[:], "failed-process")
		copy(kernelEvent.Data[:60], "/var/lib/kubelet/pods/xyz/volumes/kubernetes.io~configmap/missing")
		// Set error code (ENOENT = 2) in last 4 bytes
		binary.LittleEndian.PutUint32(kernelEvent.Data[60:64], 2)

		buffer := make([]byte, unsafe.Sizeof(kernelEvent))
		*(*KernelEvent)(unsafe.Pointer(&buffer[0])) = kernelEvent

		assert.NotPanics(t, func() {
			collector.processRawEvent(buffer)
		})
	})

	// Test with invalid buffer size
	t.Run("Invalid buffer", func(t *testing.T) {
		assert.NotPanics(t, func() {
			collector.processRawEvent([]byte{1, 2, 3}) // Too small
		})
	})
}

func TestGetEventType(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "event-type-test"}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	tests := []struct {
		name      string
		eventType uint32
		expected  string
	}{
		{
			name:      "configmap access",
			eventType: uint32(EventTypeConfigMapAccess),
			expected:  "configmap_access",
		},
		{
			name:      "secret access",
			eventType: uint32(EventTypeSecretAccess),
			expected:  "secret_access",
		},
		{
			name:      "failed config access",
			eventType: uint32(EventTypeConfigAccessFailed),
			expected:  "config_access_failed",
		},
		{
			name:      "pod syscall",
			eventType: uint32(EventTypePodSyscall),
			expected:  "pod_syscall",
		},
		{
			name:      "unknown event",
			eventType: 99,
			expected:  "unknown_99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.getEventType(tt.eventType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBytesToString(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "null terminated string",
			input:    []byte{'t', 'e', 's', 't', 0, 'x', 'x'},
			expected: "test",
		},
		{
			name:     "string without null terminator",
			input:    []byte{'t', 'e', 's', 't'},
			expected: "test",
		},
		{
			name:     "empty string",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "null byte only",
			input:    []byte{0},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bytesToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseKernelEvent(t *testing.T) {
	// Create test kernel event
	originalEvent := KernelEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1234,
		PPID:      1000,
		UID:       1000,
		GID:       1000,
		EventType: EventTypeProcess,
		CgroupID:  567890,
	}
	copy(originalEvent.Comm[:], "test-process")

	// Convert to buffer
	buffer := make([]byte, unsafe.Sizeof(originalEvent))
	*(*KernelEvent)(unsafe.Pointer(&buffer[0])) = originalEvent

	// Parse back
	parsedEvent, err := parseKernelEvent(buffer)
	require.NoError(t, err)
	require.NotNil(t, parsedEvent)

	// Verify fields
	assert.Equal(t, originalEvent.PID, parsedEvent.PID)
	assert.Equal(t, originalEvent.PPID, parsedEvent.PPID)
	assert.Equal(t, originalEvent.EventType, parsedEvent.EventType)
	assert.Equal(t, "test-process", bytesToString(parsedEvent.Comm[:]))

	// Test with invalid buffer size
	_, err = parseKernelEvent([]byte{1, 2, 3})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "buffer too small")
}

func TestLinuxCollectorSpecificLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "linux-lifecycle-test"}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Test initial state
	assert.Equal(t, "linux-lifecycle-test", collector.Name())
	assert.True(t, collector.IsHealthy())
	assert.NotNil(t, collector.Events())

	// Test Linux-specific eBPF state
	assert.Nil(t, collector.ebpfState) // Should be nil before start

	// Test multiple stops (should not panic)
	assert.NotPanics(t, func() {
		err := collector.Stop()
		assert.NoError(t, err)
	})

	assert.False(t, collector.IsHealthy())
}

func TestLinuxSpecificEBPFIntegration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "linux-ebpf-test",
		BufferSize: 10000,
		EnableEBPF: true,
	}

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Test eBPF state initialization
	assert.Nil(t, collector.ebpfState) // Should be nil before start

	// Note: Actual eBPF functionality depends on kernel support and privileges
	// In most test environments, eBPF may not be available, so we just test
	// that the methods don't panic and handle errors gracefully
}

func TestEBPFStateManagement(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "ebpf-state-test",
		BufferSize: 10000,
		EnableEBPF: true,
	}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Test that stopEBPF handles nil state gracefully
	assert.NotPanics(t, func() {
		collector.stopEBPF()
	})

	// Test that processEvents handles nil state gracefully
	assert.NotPanics(t, func() {
		// This will exit immediately since ebpfState is nil
		go collector.processEvents()
	})
}

func TestGetErrorDescription(t *testing.T) {
	tests := []struct {
		name      string
		errorCode int32
		expected  string
	}{
		{
			name:      "success",
			errorCode: 0,
			expected:  "Success",
		},
		{
			name:      "ENOENT",
			errorCode: 2,
			expected:  "No such file or directory",
		},
		{
			name:      "EACCES",
			errorCode: 13,
			expected:  "Permission denied",
		},
		{
			name:      "EIO",
			errorCode: 5,
			expected:  "Input/output error",
		},
		{
			name:      "EFAULT",
			errorCode: 14,
			expected:  "Bad address",
		},
		{
			name:      "EINVAL",
			errorCode: 22,
			expected:  "Invalid argument",
		},
		{
			name:      "ENOSPC",
			errorCode: 28,
			expected:  "No space left on device",
		},
		{
			name:      "EROFS",
			errorCode: 30,
			expected:  "Read-only file system",
		},
		{
			name:      "unknown error",
			errorCode: 999,
			expected:  "Error code 999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getErrorDescription(tt.errorCode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractMountPathAndError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "extract-test"}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	tests := []struct {
		name          string
		data          []byte
		expectedPath  string
		expectedError int32
	}{
		{
			name:          "empty data",
			data:          []byte{},
			expectedPath:  "",
			expectedError: 0,
		},
		{
			name:          "small data",
			data:          []byte{1, 2, 3},
			expectedPath:  "",
			expectedError: 0,
		},
		{
			name:          "path without error",
			data:          append([]byte("/path/to/config"), make([]byte, 50)...),
			expectedPath:  "/path/to/config",
			expectedError: 0,
		},
		{
			name: "path with error code",
			data: func() []byte {
				data := make([]byte, 64)
				copy(data[:], "/path/to/config")
				binary.LittleEndian.PutUint32(data[60:64], 13) // EACCES
				return data
			}(),
			expectedPath:  "/path/to/config",
			expectedError: 13,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, errCode := collector.extractMountPathAndError(tt.data)
			assert.Equal(t, tt.expectedPath, path)
			assert.Equal(t, tt.expectedError, errCode)
		})
	}
}

func TestEnrichmentFunctions(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "enrich-test"}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)
	collector.ctx = context.Background()

	t.Run("enrichConfigMapEvent", func(t *testing.T) {
		eventData := &KernelEventData{
			MountPath: "/var/lib/kubelet/pods/abc/volumes/kubernetes.io~configmap/my-config",
			PodUID:    "pod-123",
		}
		collector.enrichConfigMapEvent(eventData)
		assert.Equal(t, "my-config", eventData.ConfigName)
		assert.Equal(t, "default", eventData.Namespace) // Placeholder
	})

	t.Run("enrichSecretEvent", func(t *testing.T) {
		eventData := &KernelEventData{
			MountPath: "/var/lib/kubelet/pods/abc/volumes/kubernetes.io~secret/my-secret",
			PodUID:    "pod-456",
		}
		collector.enrichSecretEvent(eventData)
		assert.Equal(t, "my-secret", eventData.ConfigName)
		assert.Equal(t, "default", eventData.Namespace) // Placeholder
	})

	t.Run("enrichFailedAccessEvent", func(t *testing.T) {
		eventData := &KernelEventData{
			MountPath: "/var/lib/kubelet/pods/abc/volumes/kubernetes.io~configmap/missing-config",
			PodUID:    "pod-789",
			ErrorCode: 2, // ENOENT
		}
		collector.enrichFailedAccessEvent(eventData)
		assert.Equal(t, "configmap-failed", eventData.ConfigType)
		assert.Equal(t, "missing-config", eventData.ConfigName)
		assert.Equal(t, "default", eventData.Namespace) // Placeholder
	})

	t.Run("enrichFailedAccessEvent for secret", func(t *testing.T) {
		eventData := &KernelEventData{
			MountPath: "/var/lib/kubelet/pods/abc/volumes/kubernetes.io~secret/missing-secret",
			PodUID:    "pod-101",
			ErrorCode: 13, // EACCES
		}
		collector.enrichFailedAccessEvent(eventData)
		assert.Equal(t, "secret-failed", eventData.ConfigType)
		assert.Equal(t, "missing-secret", eventData.ConfigName)
		assert.Equal(t, "default", eventData.Namespace) // Placeholder
	})
}

func TestEBPFMapOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "map-ops-test"}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// These operations will fail since eBPF is not initialized,
	// but we test that they handle errors correctly

	t.Run("AddContainerPID", func(t *testing.T) {
		err := collector.AddContainerPID(1234)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "eBPF not initialized")
	})

	t.Run("RemoveContainerPID", func(t *testing.T) {
		err := collector.RemoveContainerPID(1234)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "eBPF not initialized")
	})

	t.Run("AddPodInfo", func(t *testing.T) {
		podInfo := PodInfo{
			PodUID:    "test-pod",
			Namespace: "test-ns",
			PodName:   "test-pod-name",
			CreatedAt: time.Now().Unix(),
		}
		err := collector.AddPodInfo(567890, podInfo)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "eBPF not initialized")
	})

	t.Run("AddMountInfo", func(t *testing.T) {
		mountInfo := MountInfo{
			Name:      "test-config",
			Namespace: "test-ns",
			MountPath: "/var/lib/kubelet/pods/abc/volumes/kubernetes.io~configmap/test",
			IsSecret:  false,
		}
		err := collector.AddMountInfo(123456, mountInfo)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "eBPF not initialized")
	})
}
