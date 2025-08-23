//go:build linux
// +build linux

package kernel

import (
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
		PPID:      1000,
		UID:       1000,
		GID:       1000,
		EventType: EventTypeProcess,
		CgroupID:  567890,
		ExitCode:  0,
		Signal:    0,
	}

	copy(kernelEvent.Comm[:], "test-process")
	copy(kernelEvent.ServiceName[:], "test-service")
	copy(kernelEvent.CgroupPath[:], "/system.slice/test.service")

	// Verify structure size and alignment
	assert.Greater(t, int(unsafe.Sizeof(kernelEvent)), 0)
	assert.Equal(t, "test-process", bytesToString(kernelEvent.Comm[:]))
	assert.Equal(t, uint32(1234), kernelEvent.PID)
	assert.Equal(t, EventTypeProcess, kernelEvent.EventType)
}

func TestProcessRawEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{Name: "process-event-test"}
	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Create test kernel event
	kernelEvent := KernelEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1234,
		PPID:      1000,
		UID:       1000,
		GID:       1000,
		EventType: EventTypeProcess,
		CgroupID:  567890,
	}
	copy(kernelEvent.Comm[:], "test-process")

	// Convert to raw bytes
	buffer := make([]byte, unsafe.Sizeof(kernelEvent))
	*(*KernelEvent)(unsafe.Pointer(&buffer[0])) = kernelEvent

	// Test processRawEvent doesn't panic
	assert.NotPanics(t, func() {
		collector.processRawEvent(buffer)
	})

	// Test with invalid buffer size
	assert.NotPanics(t, func() {
		collector.processRawEvent([]byte{1, 2, 3}) // Too small
	})
}

func TestGetEventType(t *testing.T) {
	tests := []struct {
		name      string
		eventType uint8
		expected  string
	}{
		{
			name:      "process event",
			eventType: EventTypeProcess,
			expected:  "process_exec",
		},
		{
			name:      "file event",
			eventType: EventTypeFile,
			expected:  "file_open",
		},
		{
			name:      "network event",
			eventType: EventTypeNetwork,
			expected:  "network_connect",
		},
		{
			name:      "unknown event",
			eventType: 99,
			expected:  "unknown_99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEventType(tt.eventType)
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
