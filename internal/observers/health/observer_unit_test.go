package health

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// TestNewObserver tests observer creation with various configurations
func TestNewObserver(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		wantErr   bool
		errString string
	}{
		{
			name:    "with default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name:    "with nil config uses default",
			config:  nil,
			wantErr: false,
		},
		{
			name: "with custom config",
			config: &Config{
				RingBufferSize:   4 * 1024 * 1024,
				EventChannelSize: 5000,
				RateLimitMs:      50,
				EnabledCategories: map[string]bool{
					"file": true,
				},
				RequireAllMetrics: false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			observer, err := NewObserver(logger, tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errString)
				assert.Nil(t, observer)
			} else {
				require.NoError(t, err)
				require.NotNil(t, observer)
				assert.Equal(t, "health", observer.Name())
				assert.NotNil(t, observer.Events())
				assert.False(t, observer.IsHealthy())
			}
		})
	}
}

// TestObserverLifecycle tests Start and Stop methods
func TestObserverLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	ctx := context.Background()

	// Test Start
	err = observer.Start(ctx)
	require.NoError(t, err)
	assert.True(t, observer.IsHealthy())

	// Give some time for goroutines to start
	time.Sleep(100 * time.Millisecond)

	// Test Stop
	err = observer.Stop()
	require.NoError(t, err)
	assert.False(t, observer.IsHealthy())
}

// TestConvertToCollectorEvent tests event conversion
func TestConvertToCollectorEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	tests := []struct {
		name     string
		event    *HealthEvent
		validate func(t *testing.T, result *domain.CollectorEvent)
	}{
		{
			name: "disk space exhaustion event",
			event: &HealthEvent{
				TimestampNs: 1234567890,
				PID:         1000,
				PPID:        1,
				TID:         1001,
				UID:         1000,
				GID:         1000,
				CgroupID:    999,
				SyscallNr:   1, // write
				ErrorCode:   -28, // ENOSPC
				Category:    1, // file
				Comm:        [16]byte{'t', 'e', 's', 't'},
				Path:        [256]byte{'/', 't', 'm', 'p', '/', 'f', 'i', 'l', 'e'},
				ErrorCount:  5,
			},
			validate: func(t *testing.T, result *domain.CollectorEvent) {
				assert.Equal(t, "health-1000-1234567890", result.EventID)
				assert.Equal(t, domain.EventTypeKernelSyscall, result.Type)
				assert.Equal(t, "health", result.Source)
				assert.Equal(t, domain.EventSeverityCritical, result.Severity)

				kernel := result.EventData.Kernel
				require.NotNil(t, kernel)
				assert.Equal(t, "health_issue", kernel.EventType)
				assert.Equal(t, int32(1000), kernel.PID)
				assert.Equal(t, int32(1), kernel.PPID)
				assert.Equal(t, "test", kernel.Command)
				assert.Equal(t, "write", kernel.Syscall)
				assert.Equal(t, int32(-28), kernel.ReturnCode)
				assert.Equal(t, "ENOSPC", kernel.ErrorMessage)

				assert.Equal(t, "5", result.Metadata.Labels["error_count"])
				assert.Equal(t, "file", result.Metadata.Labels["category"])
				assert.Equal(t, "/tmp/file", result.Metadata.Labels["path"])
			},
		},
		{
			name: "memory exhaustion event",
			event: &HealthEvent{
				TimestampNs: 9876543210,
				PID:         2000,
				PPID:        1,
				UID:         0,
				GID:         0,
				SyscallNr:   9, // mmap
				ErrorCode:   -12, // ENOMEM
				Category:    3, // memory
				Comm:        [16]byte{'m', 'e', 'm', 'h', 'o', 'g'},
				ErrorCount:  1,
			},
			validate: func(t *testing.T, result *domain.CollectorEvent) {
				assert.Equal(t, domain.EventSeverityCritical, result.Severity)
				assert.Equal(t, "ENOMEM", result.EventData.Kernel.ErrorMessage)
				assert.Equal(t, "memory", result.Metadata.Labels["category"])
				assert.Equal(t, "memhog", result.EventData.Kernel.Command)
			},
		},
		{
			name: "network connection refused",
			event: &HealthEvent{
				TimestampNs: 5555555555,
				PID:         3000,
				PPID:        100,
				SyscallNr:   42, // connect
				ErrorCode:   -111, // ECONNREFUSED
				Category:    2, // network
				Comm:        [16]byte{'c', 'u', 'r', 'l'},
				SrcIP:       0x0100007f, // 127.0.0.1
				DstIP:       0x0100007f, // 127.0.0.1
				SrcPort:     45678,
				DstPort:     8080,
			},
			validate: func(t *testing.T, result *domain.CollectorEvent) {
				assert.Equal(t, domain.EventSeverityError, result.Severity)
				assert.Equal(t, "ECONNREFUSED", result.EventData.Kernel.ErrorMessage)
				assert.Equal(t, "network", result.Metadata.Labels["category"])
				assert.Equal(t, "connect", result.EventData.Kernel.Syscall)

				// Check network context
				require.NotNil(t, result.EventData.Custom)
				assert.Equal(t, "127.0.0.1", result.EventData.Custom["src_ip"])
				assert.Equal(t, "127.0.0.1", result.EventData.Custom["dst_ip"])
				assert.Equal(t, "45678", result.EventData.Custom["src_port"])
				assert.Equal(t, "8080", result.EventData.Custom["dst_port"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := observer.convertToCollectorEvent(tt.event)
			require.NotNil(t, result)
			tt.validate(t, result)
		})
	}
}

// TestBytesToString tests the bytesToString helper function
func TestBytesToString(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "normal string",
			input:    []byte{'h', 'e', 'l', 'l', 'o', 0, 0, 0},
			expected: "hello",
		},
		{
			name:     "full buffer no null",
			input:    []byte{'f', 'u', 'l', 'l'},
			expected: "full",
		},
		{
			name:     "empty buffer",
			input:    []byte{0, 0, 0, 0},
			expected: "",
		},
		{
			name:     "string with null in middle",
			input:    []byte{'t', 'e', 's', 't', 0, 'x', 'x'},
			expected: "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bytesToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFormatIP tests IP address formatting
func TestFormatIP(t *testing.T) {
	tests := []struct {
		name     string
		input    uint32
		expected string
	}{
		{
			name:     "localhost",
			input:    0x0100007f, // 127.0.0.1 in little-endian
			expected: "127.0.0.1",
		},
		{
			name:     "private IP",
			input:    0x0101a8c0, // 192.168.1.1 in little-endian
			expected: "192.168.1.1",
		},
		{
			name:     "zeros",
			input:    0x00000000,
			expected: "0.0.0.0",
		},
		{
			name:     "broadcast",
			input:    0xffffffff,
			expected: "255.255.255.255",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatIP(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetCategoryName tests syscall category name mapping
func TestGetCategoryName(t *testing.T) {
	tests := []struct {
		category uint8
		expected string
	}{
		{1, "file"},
		{2, "network"},
		{3, "memory"},
		{4, "process"},
		{0, "unknown"},
		{99, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := getCategoryName(tt.category)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMapSeverity tests severity mapping
func TestMapSeverity(t *testing.T) {
	tests := []struct {
		input    string
		expected domain.EventSeverity
	}{
		{"critical", domain.EventSeverityCritical},
		{"high", domain.EventSeverityError},
		{"medium", domain.EventSeverityWarning},
		{"low", domain.EventSeverityInfo},
		{"unknown", domain.EventSeverityInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapSeverity(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetErrorName tests error code to name mapping
func TestGetErrorName(t *testing.T) {
	tests := []struct {
		code     int32
		expected string
	}{
		{-1, "EPERM"},
		{-2, "ENOENT"},
		{-5, "EIO"},
		{-12, "ENOMEM"},
		{-13, "EACCES"},
		{-16, "EBUSY"},
		{-22, "EINVAL"},
		{-24, "EMFILE"},
		{-28, "ENOSPC"},
		{-110, "ETIMEDOUT"},
		{-111, "ECONNREFUSED"},
		{-122, "EDQUOT"},
		{-999, "ERROR_-999"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := getErrorName(tt.code)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetErrorSeverity tests error severity classification
func TestGetErrorSeverity(t *testing.T) {
	tests := []struct {
		code     int32
		expected string
	}{
		{-12, "critical"},  // ENOMEM
		{-28, "critical"},  // ENOSPC
		{-122, "critical"}, // EDQUOT
		{-24, "critical"},  // EMFILE
		{-5, "high"},       // EIO
		{-111, "high"},     // ECONNREFUSED
		{-110, "high"},     // ETIMEDOUT
		{-13, "medium"},    // EACCES
		{-1, "medium"},     // EPERM
		{-16, "medium"},    // EBUSY
		{-2, "low"},        // ENOENT (default)
		{-999, "low"},      // Unknown
	}

	for _, tt := range tests {
		t.Run(getErrorName(tt.code), func(t *testing.T) {
			result := getErrorSeverity(tt.code)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetSyscallName tests syscall number to name mapping
func TestGetSyscallName(t *testing.T) {
	tests := []struct {
		nr       int32
		expected string
	}{
		{0, "read"},
		{1, "write"},
		{2, "open"},
		{3, "close"},
		{41, "socket"},
		{42, "connect"},
		{43, "accept"},
		{59, "execve"},
		{83, "mkdir"},
		{87, "unlink"},
		{257, "openat"},
		{999, "syscall_999"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := getSyscallName(tt.nr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDefaultConfig tests default configuration
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, 8*1024*1024, config.RingBufferSize)
	assert.Equal(t, 10000, config.EventChannelSize)
	assert.Equal(t, 100, config.RateLimitMs)
	assert.False(t, config.RequireAllMetrics)

	// Check enabled categories
	assert.True(t, config.EnabledCategories["file"])
	assert.True(t, config.EnabledCategories["network"])
	assert.True(t, config.EnabledCategories["memory"])
	assert.False(t, config.EnabledCategories["unknown"])
}

// TestObserverHealth tests health status methods
func TestObserverHealth(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	// Initially unhealthy
	assert.False(t, observer.IsHealthy())
	health := observer.Health()
	assert.Equal(t, domain.HealthUnhealthy, health.Status)

	// Start should make it healthy
	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	assert.True(t, observer.IsHealthy())

	health = observer.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)
	// Health doesn't have ObserverName, verify through observer itself
	assert.Equal(t, "health", observer.Name())

	// Stop should make it unhealthy again
	err = observer.Stop()
	require.NoError(t, err)
	assert.False(t, observer.IsHealthy())
}

// TestObserverStatistics tests statistics tracking
func TestObserverStatistics(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	// Get initial stats
	stats := observer.Statistics()
	assert.NotNil(t, stats)
	// CollectorStats has EventsProcessed and ErrorCount
	assert.Equal(t, int64(0), stats.EventsProcessed)
	assert.Equal(t, int64(0), stats.ErrorCount)

	// Record some events
	observer.BaseObserver.RecordEvent()
	observer.BaseObserver.RecordEvent()
	observer.BaseObserver.RecordDrop()

	// Check updated stats
	stats = observer.Statistics()
	assert.Equal(t, int64(2), stats.EventsProcessed)
	// Drops are tracked in CustomMetrics
	if stats.CustomMetrics != nil {
		assert.Equal(t, "1", stats.CustomMetrics["events_dropped"])
	}
}

// TestEventChannel tests event channel operations
func TestEventChannel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		EventChannelSize:  2,
		RingBufferSize:   1024,
		RateLimitMs:      10,
		EnabledCategories: map[string]bool{"test": true},
	}

	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	// Get channel
	eventChan := observer.Events()
	require.NotNil(t, eventChan)

	// Send events with proper validation fields
	event1 := &domain.CollectorEvent{
		EventID:   "1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelSyscall,
		Source:    "health",
		Severity:  domain.EventSeverityInfo,
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "health",
				"test":     "true",
			},
		},
	}
	event2 := &domain.CollectorEvent{
		EventID:   "2",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelSyscall,
		Source:    "health",
		Severity:  domain.EventSeverityInfo,
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "health",
				"test":     "true",
			},
		},
	}
	event3 := &domain.CollectorEvent{
		EventID:   "3",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelSyscall,
		Source:    "health",
		Severity:  domain.EventSeverityInfo,
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "health",
				"test":     "true",
			},
		},
	}

	// First two should succeed
	assert.True(t, observer.EventChannelManager.SendEvent(event1))
	assert.True(t, observer.EventChannelManager.SendEvent(event2))

	// Third should fail (channel full)
	assert.False(t, observer.EventChannelManager.SendEvent(event3))

	// Read events
	select {
	case e := <-eventChan:
		assert.Equal(t, "1", e.EventID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout reading event")
	}

	select {
	case e := <-eventChan:
		assert.Equal(t, "2", e.EventID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout reading event")
	}
}

// TestFactoryFunction tests the factory registration
func TestFactoryFunction(t *testing.T) {
	// Factory function is tested via integration tests
	// since it requires orchestrator types
	t.Log("Factory function tested in integration tests")
}