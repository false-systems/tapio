//go:build linux
// +build linux

package storageio

import (
	"bytes"
	"context"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// Test Linux-specific shouldFilterEvent function
func TestShouldFilterEventLinux(t *testing.T) {
	config := NewDefaultConfig()
	config.MinIOSize = 1024
	config.ExcludedPaths = []string{"/tmp", "/var/tmp"}
	config.ExcludedProcesses = []string{"systemd", "kthreadd"}
	config.IncludedProcesses = []string{"postgres", "nginx", "app"}

	collector, err := NewCollector("test-filter-linux", config)
	require.NoError(t, err)

	tests := []struct {
		name     string
		event    *StorageIOEvent
		expected bool
	}{
		{
			name: "should not filter - good event",
			event: &StorageIOEvent{
				Size:    2048,
				Path:    "/var/lib/kubelet/pods/test",
				Command: "postgres",
			},
			expected: false,
		},
		{
			name: "should filter - too small",
			event: &StorageIOEvent{
				Size:    512, // < MinIOSize
				Path:    "/var/lib/kubelet/pods/test",
				Command: "postgres",
			},
			expected: true,
		},
		{
			name: "should filter - excluded path",
			event: &StorageIOEvent{
				Size:    2048,
				Path:    "/tmp/test-file", // In excluded paths
				Command: "postgres",
			},
			expected: true,
		},
		{
			name: "should filter - excluded process",
			event: &StorageIOEvent{
				Size:    2048,
				Path:    "/var/lib/kubelet/pods/test",
				Command: "systemd", // In excluded processes
			},
			expected: true,
		},
		{
			name: "should filter - not in included processes",
			event: &StorageIOEvent{
				Size:    2048,
				Path:    "/var/lib/kubelet/pods/test",
				Command: "unknown-process", // Not in included processes
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.shouldFilterEvent(tt.event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test convertRawToStorageEvent function
func TestConvertRawToStorageEventAdditional(t *testing.T) {
	collector, err := NewCollector("test-convert-raw", NewDefaultConfig())
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)

	// Create a mock raw event
	rawEvent := &StorageIOEventRaw{
		EventType:   uint8(VFSProbeRead),
		PID:         1000,
		PPID:        999,
		UID:         1001,
		GID:         1001,
		CgroupID:    12345,
		StartTimeNs: 1000000000, // 1 second
		EndTimeNs:   1005000000, // 1.005 seconds (5ms duration)
		Inode:       67890,
		Size:        4096,
		Offset:      0,
		Flags:       0,
		Mode:        0644,
		ErrorCode:   0,
		DevMajor:    8,
		DevMinor:    0,
	}

	// Set path and command
	copy(rawEvent.Path[:], "/var/lib/kubelet/pods/test/volumes/kubernetes.io~csi/pvc-123/mount/data.db")
	copy(rawEvent.Comm[:], "postgres")

	// Convert to storage event
	storageEvent, err := collector.convertRawToStorageEvent(rawEvent)
	require.NoError(t, err)

	assert.Equal(t, "read", storageEvent.Operation)
	assert.Equal(t, "/var/lib/kubelet/pods/test/volumes/kubernetes.io~csi/pvc-123/mount/data.db", storageEvent.Path)
	assert.Equal(t, int64(4096), storageEvent.Size)
	assert.Equal(t, int64(0), storageEvent.Offset)
	assert.Equal(t, 5*time.Millisecond, storageEvent.Duration)
	assert.Equal(t, "8:0", storageEvent.Device)
	assert.Equal(t, uint64(67890), storageEvent.Inode)
	assert.Equal(t, int32(1000), storageEvent.PID)
	assert.Equal(t, int32(999), storageEvent.PPID)
	assert.Equal(t, int32(1001), storageEvent.UID)
	assert.Equal(t, int32(1001), storageEvent.GID)
	assert.Equal(t, "postgres", storageEvent.Command)
	assert.Equal(t, uint64(12345), storageEvent.CgroupID)
	assert.Equal(t, int32(0), storageEvent.ErrorCode)
	assert.Equal(t, "vfs_read", storageEvent.VFSLayer)
	assert.False(t, storageEvent.SlowIO) // 5ms < 10ms default threshold
	assert.False(t, storageEvent.BlockedIO)
}

// Test convertEventType function
func TestConvertEventTypeAdditional(t *testing.T) {
	collector, err := NewCollector("test-event-type", NewDefaultConfig())
	require.NoError(t, err)

	tests := []struct {
		eventType uint8
		expected  string
	}{
		{uint8(VFSProbeRead), "read"},
		{uint8(VFSProbeWrite), "write"},
		{uint8(VFSProbeFsync), "fsync"},
		{uint8(VFSProbeIterateDir), "iterate_dir"},
		{uint8(VFSProbeOpen), "open"},
		{uint8(VFSProbeClose), "close"},
		{99, "unknown_99"}, // Invalid type
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := collector.convertEventType(tt.eventType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test validateRawEvent function
func TestValidateRawEventAdditional(t *testing.T) {
	collector, err := NewCollector("test-validate", NewDefaultConfig())
	require.NoError(t, err)

	tests := []struct {
		name      string
		event     *StorageIOEventRaw
		shouldErr bool
	}{
		{
			name: "valid event",
			event: &StorageIOEventRaw{
				PID:         1000,
				StartTimeNs: 1000000000,
				EndTimeNs:   1005000000,
				EventType:   uint8(VFSProbeRead),
			},
			shouldErr: false,
		},
		{
			name: "invalid PID",
			event: &StorageIOEventRaw{
				PID:         0, // Invalid
				StartTimeNs: 1000000000,
				EndTimeNs:   1005000000,
				EventType:   uint8(VFSProbeRead),
			},
			shouldErr: true,
		},
		{
			name: "invalid timestamps - both zero",
			event: &StorageIOEventRaw{
				PID:         1000,
				StartTimeNs: 0, // Invalid
				EndTimeNs:   0, // Invalid
				EventType:   uint8(VFSProbeRead),
			},
			shouldErr: true,
		},
		{
			name: "invalid timestamps - end before start",
			event: &StorageIOEventRaw{
				PID:         1000,
				StartTimeNs: 1005000000,
				EndTimeNs:   1000000000, // Before start time
				EventType:   uint8(VFSProbeRead),
			},
			shouldErr: true,
		},
		{
			name: "invalid event type",
			event: &StorageIOEventRaw{
				PID:         1000,
				StartTimeNs: 1000000000,
				EndTimeNs:   1005000000,
				EventType:   0, // Invalid
			},
			shouldErr: true,
		},
		{
			name: "event type too high",
			event: &StorageIOEventRaw{
				PID:         1000,
				StartTimeNs: 1000000000,
				EndTimeNs:   1005000000,
				EventType:   10, // > 6, invalid
			},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := collector.validateRawEvent(tt.event)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test parseStorageEventRaw function
func TestParseStorageEventRawAdditional(t *testing.T) {
	collector, err := NewCollector("test-parse", NewDefaultConfig())
	require.NoError(t, err)

	// Create a valid raw event in binary format
	rawEvent := StorageIOEventRaw{
		EventType:   uint8(VFSProbeWrite),
		PID:         1500,
		PPID:        1499,
		UID:         1002,
		GID:         1002,
		CgroupID:    98765,
		StartTimeNs: 2000000000,
		EndTimeNs:   2008000000,
		Inode:       11111,
		Size:        2048,
		Offset:      4096,
		Flags:       0x1,
		Mode:        0755,
		ErrorCode:   0,
		DevMajor:    8,
		DevMinor:    2,
	}

	copy(rawEvent.Path[:], "/var/lib/kubelet/pods/test-pod/volumes/kubernetes.io~secret/tls-certs/cert.pem")
	copy(rawEvent.Comm[:], "nginx")

	// Convert to bytes using a bytes buffer to ensure proper alignment
	var buf bytes.Buffer
	err = writeRawEventToBuf(&buf, &rawEvent)
	require.NoError(t, err)

	// Parse it back
	parsed, err := collector.parseStorageEventRaw(buf.Bytes())
	require.NoError(t, err)

	assert.Equal(t, uint8(VFSProbeWrite), parsed.EventType)
	assert.Equal(t, uint32(1500), parsed.PID)
	assert.Equal(t, uint32(1499), parsed.PPID)
	assert.Equal(t, uint32(1002), parsed.UID)
	assert.Equal(t, uint32(1002), parsed.GID)
	assert.Equal(t, uint64(98765), parsed.CgroupID)
	assert.Equal(t, uint64(2000000000), parsed.StartTimeNs)
	assert.Equal(t, uint64(2008000000), parsed.EndTimeNs)
	assert.Equal(t, uint64(11111), parsed.Inode)
	assert.Equal(t, int64(2048), parsed.Size)
	assert.Equal(t, int64(4096), parsed.Offset)
	assert.Equal(t, uint32(0x1), parsed.Flags)
	assert.Equal(t, uint32(0755), parsed.Mode)
	assert.Equal(t, int32(0), parsed.ErrorCode)
	assert.Equal(t, uint32(8), parsed.DevMajor)
	assert.Equal(t, uint32(2), parsed.DevMinor)

	// Test path and command extraction
	pathStr := bytesToString(parsed.Path[:])
	assert.Equal(t, "/var/lib/kubelet/pods/test-pod/volumes/kubernetes.io~secret/tls-certs/cert.pem", pathStr)

	commStr := bytesToString(parsed.Comm[:])
	assert.Equal(t, "nginx", commStr)
}

// Helper function to write raw event to buffer with proper binary encoding
func writeRawEventToBuf(buf *bytes.Buffer, event *StorageIOEventRaw) error {
	// Write the struct to a byte slice manually to maintain alignment
	data := make([]byte, unsafe.Sizeof(*event))
	ptr := unsafe.Pointer(event)
	copy(data, (*[unsafe.Sizeof(*event)]byte)(ptr)[:])

	_, err := buf.Write(data)
	return err
}

// Test parseStorageEventRaw with too small data
func TestParseStorageEventRawTooSmallAdditional(t *testing.T) {
	collector, err := NewCollector("test-parse-small", NewDefaultConfig())
	require.NoError(t, err)

	// Data too small
	smallData := make([]byte, 10)

	_, err = collector.parseStorageEventRaw(smallData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "event data too small")
}

// Test getErrorMessage function
func TestGetErrorMessageAdditional(t *testing.T) {
	collector, err := NewCollector("test-errors", NewDefaultConfig())
	require.NoError(t, err)

	tests := []struct {
		errorCode int32
		expected  string
	}{
		{2, "No such file or directory"},
		{5, "Input/output error"},
		{13, "Permission denied"},
		{16, "Device or resource busy"},
		{28, "No space left on device"},
		{30, "Read-only file system"},
		{99, "Error code 99"}, // Unknown error
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := collector.getErrorMessage(tt.errorCode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test processRawStorageEvent function - the full pipeline
func TestProcessRawStorageEventAdditional(t *testing.T) {
	collector, err := NewCollector("test-pipeline", NewDefaultConfig())
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)

	// Set up context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector.ctx = ctx
	collector.cancel = cancel

	// Create a valid raw event
	rawEvent := StorageIOEventRaw{
		EventType:   uint8(VFSProbeRead),
		PID:         3000,
		PPID:        2999,
		UID:         1003,
		GID:         1003,
		CgroupID:    55555,
		StartTimeNs: 3000000000,
		EndTimeNs:   3025000000, // 25ms duration - slow!
		Inode:       22222,
		Size:        8192,
		Offset:      0,
		Flags:       0,
		Mode:        0644,
		ErrorCode:   0,
		DevMajor:    8,
		DevMinor:    3,
	}

	copy(rawEvent.Path[:], "/var/lib/kubelet/pods/test/volumes/kubernetes.io~csi/pvc-database/mount/slow-query.log")
	copy(rawEvent.Comm[:], "mysql")

	// Convert to bytes
	var buf bytes.Buffer
	err = writeRawEventToBuf(&buf, &rawEvent)
	require.NoError(t, err)

	// Process the raw event
	err = collector.processRawStorageEvent(buf.Bytes())
	require.NoError(t, err)

	// Check that event was processed and sent to channel
	select {
	case event := <-collector.events:
		assert.NotNil(t, event)

		storageData, ok := event.GetStorageIOData()
		require.True(t, ok)

		assert.Equal(t, "read", storageData.Operation)
		assert.True(t, storageData.SlowIO)             // 25ms > 10ms threshold
		assert.Equal(t, "pvc", storageData.VolumeType) // Should be enriched

	case <-time.After(100 * time.Millisecond):
		t.Fatal("Processed event not received within timeout")
	}
}

// Test bytesToString function
func TestBytesToStringAdditional(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "null terminated string",
			input:    []byte("hello\x00world\x00"),
			expected: "hello",
		},
		{
			name:     "no null terminator",
			input:    []byte("hello"),
			expected: "hello",
		},
		{
			name:     "empty bytes",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "only null",
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

// Test utility functions - matchesPath and hashPath
func TestUtilityFunctionsLinux(t *testing.T) {
	// Test matchesPath
	assert.True(t, matchesPath("/var/lib/kubelet/pods/test/pvc", "/var/lib/kubelet"))
	assert.True(t, matchesPath("/var/lib/kubelet/pods/test/pvc", "/var/lib/kubelet/pods/test/pvc"))
	assert.False(t, matchesPath("/var/lib/kub", "/var/lib/kubelet"))
	assert.False(t, matchesPath("", "/var/lib/kubelet"))

	// Test hashPath - just verify it returns consistent values
	hash1 := hashPath("/test/path")
	hash2 := hashPath("/test/path")
	hash3 := hashPath("/different/path")

	assert.Equal(t, hash1, hash2)      // Same input should give same hash
	assert.NotEqual(t, hash1, hash3)   // Different input should give different hash (usually)
	assert.GreaterOrEqual(t, hash1, 0) // Hash should be non-negative
}

// Test slow IO tracking with actual SlowIOEvent struct
func TestSlowIOEventStructure(t *testing.T) {
	collector, err := NewCollector("test-slow-struct", NewDefaultConfig())
	require.NoError(t, err)

	event := &StorageIOEvent{
		Operation: "write",
		Path:      "/var/lib/kubelet/pods/test/pvc-data/slow-file.db",
		Duration:  100 * time.Millisecond,
		PID:       4000,
		Timestamp: time.Now(),
	}

	collector.trackSlowIOEvent(event)

	// Check the slow IO cache structure
	key := "write:/var/lib/kubelet/pods/test/pvc-data/slow-file.db:4000"
	collector.slowIOCacheMu.RLock()
	slowEvent, exists := collector.slowIOCache[key]
	collector.slowIOCacheMu.RUnlock()

	assert.True(t, exists)
	assert.NotNil(t, slowEvent)
	if slowEvent != nil {
		assert.Equal(t, "write", slowEvent.Operation)
		assert.Equal(t, "/var/lib/kubelet/pods/test/pvc-data/slow-file.db", slowEvent.Path)
		assert.Equal(t, int32(4000), slowEvent.PID)
		assert.Equal(t, 100*time.Millisecond, slowEvent.Duration)
	}
}
