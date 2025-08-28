//go:build linux
// +build linux

package syscallerrors

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestCollectorLinux(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping eBPF test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:   8 * 1024 * 1024,
		EventChannelSize: 1000,
		RateLimitMs:      100,
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": true,
			"memory":  true,
		},
	}

	collector, err := NewCollector(logger, config)
	require.NoError(t, err)
	assert.NotNil(t, collector)

	// Test health check before start
	assert.True(t, collector.IsHealthy())

	// Note: Starting the collector requires root privileges
	// and eBPF support, so we can't test it in regular CI
}

func TestConvertToObservationEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)

	event := &SyscallErrorEvent{
		TimestampNs: uint64(time.Now().UnixNano()),
		PID:         1234,
		PPID:        1,
		TID:         1235,
		UID:         1000,
		GID:         1000,
		CgroupID:    567890,
		SyscallNr:   2,   // open
		ErrorCode:   -28, // ENOSPC
		Category:    1,   // file
		ErrorCount:  3,
	}

	// Set command name
	copy(event.Comm[:], []byte("testapp"))
	// Set path
	copy(event.Path[:], []byte("/var/log/test.log"))

	obsEvent := collector.convertToObservationEvent(event)

	assert.NotNil(t, obsEvent)
	assert.Equal(t, domain.EventTypeSyscallError, obsEvent.Type)
	assert.Equal(t, domain.SeverityCritical, obsEvent.Severity) // ENOSPC is critical
	assert.Equal(t, "syscall-errors", obsEvent.Source.Component)
	assert.Equal(t, "process", obsEvent.Resource.Type)
	assert.Equal(t, "1234", obsEvent.Resource.ID)
	assert.Equal(t, "testapp", obsEvent.Resource.Name)
	assert.Contains(t, obsEvent.Description, "open")
	assert.Contains(t, obsEvent.Description, "ENOSPC")

	// Check context
	assert.Equal(t, "1234", obsEvent.Context["pid"])
	assert.Equal(t, "1", obsEvent.Context["ppid"])
	assert.Equal(t, "testapp", obsEvent.Context["command"])
	assert.Equal(t, "/var/log/test.log", obsEvent.Context["path"])
	assert.Equal(t, "ENOSPC", obsEvent.Context["error_name"])
	assert.Equal(t, "file", obsEvent.Context["category"])
}

func TestGetSeverityForError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)

	tests := []struct {
		errorCode int32
		expected  domain.EventSeverity
	}{
		{-28, domain.SeverityCritical}, // ENOSPC
		{-12, domain.SeverityCritical}, // ENOMEM
		{-111, domain.SeverityHigh},    // ECONNREFUSED
		{-110, domain.SeverityHigh},    // ETIMEDOUT
		{-5, domain.SeverityHigh},      // EIO
		{-13, domain.SeverityMedium},   // EACCES
		{-1, domain.SeverityMedium},    // EPERM
		{-2, domain.SeverityLow},       // ENOENT (default)
	}

	for _, tt := range tests {
		t.Run(collector.getErrorName(tt.errorCode), func(t *testing.T) {
			severity := collector.getSeverityForError(tt.errorCode)
			assert.Equal(t, tt.expected, severity)
		})
	}
}

func TestGetErrorName(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)

	tests := []struct {
		errorCode int32
		expected  string
	}{
		{-1, "EPERM"},
		{-2, "ENOENT"},
		{-5, "EIO"},
		{-11, "EAGAIN"},
		{-12, "ENOMEM"},
		{-13, "EACCES"},
		{-28, "ENOSPC"},
		{-110, "ETIMEDOUT"},
		{-111, "ECONNREFUSED"},
		{-999, "ERROR_999"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			name := collector.getErrorName(tt.errorCode)
			assert.Equal(t, tt.expected, name)
		})
	}
}

func TestGetCategoryName(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)

	tests := []struct {
		category uint8
		expected string
	}{
		{1, "file"},
		{2, "network"},
		{3, "memory"},
		{4, "process"},
		{5, "other"},
		{99, "other"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			name := collector.getCategoryName(tt.category)
			assert.Equal(t, tt.expected, name)
		})
	}
}

func TestGetSyscallName(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)

	tests := []struct {
		syscallNr int32
		expected  string
	}{
		{0, "read"},
		{1, "write"},
		{2, "open"},
		{3, "close"},
		{41, "socket"},
		{42, "connect"},
		{43, "accept"},
		{257, "openat"},
		{999, "syscall_999"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			name := collector.getSyscallName(tt.syscallNr)
			assert.Equal(t, tt.expected, name)
		})
	}
}

func TestUpdateErrorMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)

	ctx := context.Background()

	// Test updating metrics for known error codes
	errorCodes := []int32{-28, -12, -111}
	for _, code := range errorCodes {
		collector.updateErrorMetrics(ctx, code)
		// Metrics would be recorded if initialized
	}
}

func TestCollectorStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)

	// Stop should be idempotent
	err = collector.Stop()
	assert.NoError(t, err)

	// Second stop should also succeed
	err = collector.Stop()
	assert.NoError(t, err)

	// Channel should be closed
	_, ok := <-collector.GetEventChannel()
	assert.False(t, ok)
}
