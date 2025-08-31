//go:build linux
// +build linux

package syscallerrors

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// TestProcessRawEvent tests the processRawEvent method
func TestProcessRawEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		EventChannelSize: 100,
		EnabledCategories: map[string]bool{
			"file": true,
		},
	}
	collector, err := NewCollector(logger, config)
	require.NoError(t, err)
	defer collector.Stop()

	// Create a test event
	event := SyscallErrorEvent{
		TimestampNs: uint64(time.Now().UnixNano()),
		PID:         12345,
		PPID:        1234,
		TID:         12346,
		UID:         1000,
		GID:         1000,
		CgroupID:    98765,
		SyscallNr:   2,   // open
		ErrorCode:   -28, // ENOSPC
		Category:    1,   // file
		ErrorCount:  3,
	}
	copy(event.Comm[:], []byte("testproc"))
	copy(event.Path[:], []byte("/test/path"))

	// Convert to raw bytes
	var buf bytes.Buffer
	err = binary.Write(&buf, binary.LittleEndian, event)
	require.NoError(t, err)

	// Process the raw event
	err = collector.processRawEvent(buf.Bytes())
	assert.NoError(t, err)

	// Check if event was sent to channel
	select {
	case obsEvent := <-collector.Events():
		assert.NotNil(t, obsEvent)
		assert.Equal(t, domain.EventTypeSyscallError, obsEvent.Type)
		assert.Equal(t, "12345", obsEvent.Context["pid"])
		assert.Equal(t, "testproc", obsEvent.Context["command"])
		assert.Equal(t, "ENOSPC", obsEvent.Context["error_name"])
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected event in channel")
	}
}

// TestProcessRawEventFiltering tests category filtering
func TestProcessRawEventFiltering(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		EventChannelSize: 100,
		EnabledCategories: map[string]bool{
			"file": false, // Disable file events
		},
	}
	collector, err := NewCollector(logger, config)
	require.NoError(t, err)
	defer collector.Stop()

	// Create a file category event (should be filtered)
	event := SyscallErrorEvent{
		TimestampNs: uint64(time.Now().UnixNano()),
		PID:         12345,
		SyscallNr:   2,   // open
		ErrorCode:   -28, // ENOSPC
		Category:    1,   // file
	}
	copy(event.Comm[:], []byte("testproc"))

	var buf bytes.Buffer
	err = binary.Write(&buf, binary.LittleEndian, event)
	require.NoError(t, err)

	// Process the event
	err = collector.processRawEvent(buf.Bytes())
	assert.NoError(t, err)

	// Event should be filtered out
	select {
	case <-collector.Events():
		t.Error("Event should have been filtered")
	case <-time.After(50 * time.Millisecond):
		// Expected - no event
	}
}

// TestProcessRawEventInvalidSize tests handling of invalid event sizes
func TestProcessRawEventInvalidSize(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)
	defer collector.Stop()

	// Too small buffer
	smallBuf := make([]byte, 10)
	err = collector.processRawEvent(smallBuf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too small")

	// Exact size buffer should work
	exactBuf := make([]byte, unsafe.Sizeof(SyscallErrorEvent{}))
	err = collector.processRawEvent(exactBuf)
	assert.NoError(t, err) // Won't generate event but shouldn't error
}

// TestGetStats tests statistics retrieval
func TestGetStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)
	defer collector.Stop()

	// Stats should return error if eBPF not started
	_, err = collector.GetStats()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

// TestHelperMethods tests the public interface methods
func TestHelperMethods(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)
	defer collector.Stop()

	// Test GetName
	assert.Equal(t, "syscall-errors", collector.GetName())

	// Test IsHealthy
	assert.True(t, collector.IsHealthy())

	// Test Events
	ch := collector.Events()
	assert.NotNil(t, ch)
}


// TestBytesToString tests the bytesToString helper function
func TestBytesToString(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "null-terminated string",
			input:    []byte{'h', 'e', 'l', 'l', 'o', 0, 'w', 'o', 'r', 'l', 'd'},
			expected: "hello",
		},
		{
			name:     "no null terminator",
			input:    []byte{'h', 'e', 'l', 'l', 'o'},
			expected: "hello",
		},
		{
			name:     "empty slice",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "only null",
			input:    []byte{0},
			expected: "",
		},
		{
			name:     "null at beginning",
			input:    []byte{0, 'h', 'e', 'l', 'l', 'o'},
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

// TestErrorCodeMapping tests error code to name mapping via event conversion
func TestErrorCodeMapping(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)
	defer collector.Stop()

	errorTests := []struct {
		errorCode    int32
		expectedName string
	}{
		{-28, "ENOSPC"},
		{-12, "ENOMEM"},
		{-24, "EMFILE"},
		{-122, "EDQUOT"},
		{-111, "ECONNREFUSED"},
	}

	for _, tt := range errorTests {
		event := &SyscallErrorEvent{
			TimestampNs: uint64(time.Now().UnixNano()),
			PID:         12345,
			SyscallNr:   2,
			ErrorCode:   tt.errorCode,
			Category:    1,
		}
		copy(event.Comm[:], []byte("test"))

		obsEvent := collector.convertToObservationEvent(event)
		assert.Equal(t, tt.expectedName, obsEvent.Context["error_name"])
	}
}

// TestSyscallMapping tests syscall number to name mapping via event conversion
func TestSyscallMapping(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)
	defer collector.Stop()

	syscallTests := []struct {
		syscallNr    int32
		expectedName string
	}{
		{0, "read"},
		{1, "write"},
		{2, "open"},
		{41, "socket"},
		{42, "connect"},
		{257, "openat"},
	}

	for _, tt := range syscallTests {
		event := &SyscallErrorEvent{
			TimestampNs: uint64(time.Now().UnixNano()),
			PID:         12345,
			SyscallNr:   tt.syscallNr,
			ErrorCode:   -1,
			Category:    1,
		}
		copy(event.Comm[:], []byte("test"))

		obsEvent := collector.convertToObservationEvent(event)
		assert.Equal(t, tt.expectedName, obsEvent.Context["syscall"])
	}
}

// TestSeverityAssignment tests severity assignment via event conversion
func TestSeverityAssignment(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)
	defer collector.Stop()

	severityTests := []struct {
		errorCode        int32
		expectedSeverity domain.EventSeverity
	}{
		{-28, domain.SeverityCritical},  // ENOSPC
		{-12, domain.SeverityCritical},  // ENOMEM
		{-24, domain.SeverityCritical},  // EMFILE
		{-122, domain.SeverityCritical}, // EDQUOT
		{-111, domain.SeverityHigh},     // ECONNREFUSED
		{-5, domain.SeverityHigh},       // EIO
		{-13, domain.SeverityMedium},    // EACCES
		{-2, domain.SeverityLow},        // ENOENT
	}

	for _, tt := range severityTests {
		event := &SyscallErrorEvent{
			TimestampNs: uint64(time.Now().UnixNano()),
			PID:         12345,
			SyscallNr:   2,
			ErrorCode:   tt.errorCode,
			Category:    1,
		}
		copy(event.Comm[:], []byte("test"))

		obsEvent := collector.convertToObservationEvent(event)
		assert.Equal(t, tt.expectedSeverity, obsEvent.Severity)
	}
}
