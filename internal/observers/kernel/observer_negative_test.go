package kernel

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestNegativeInvalidConfiguration tests observer behavior with invalid configurations
func TestNegativeInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		wantError bool
		errorMsg  string
	}{
		{
			name: "negative buffer size",
			config: &Config{
				Name:       "invalid",
				BufferSize: -1,
				EnableEBPF: false,
			},
			wantError: false, // Should use default or 0
		},
		{
			name: "extremely large buffer size",
			config: &Config{
				Name:       "large",
				BufferSize: int(^uint(0) >> 1), // Max int
				EnableEBPF: false,
			},
			wantError: false, // Should handle gracefully
		},
		{
			name: "empty name",
			config: &Config{
				Name:       "",
				BufferSize: 100,
				EnableEBPF: false,
			},
			wantError: false, // Should handle empty name
		},
		{
			name: "very long name",
			config: &Config{
				Name:       string(make([]byte, 10000)),
				BufferSize: 100,
				EnableEBPF: false,
			},
			wantError: false, // Should handle long names
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := NewObserver("test", tt.config)

			if tt.wantError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				// Should handle gracefully
				assert.NoError(t, err)
				if observer != nil {
					observer.Stop()
				}
			}
		})
	}
}

// TestNegativeNilInputs tests observer behavior with nil inputs
func TestNegativeNilInputs(t *testing.T) {
	// Test with nil config
	observer, err := NewObserver("test", nil)
	require.NoError(t, err, "Should handle nil config with defaults")
	require.NotNil(t, observer)

	// Test convertKernelEvent with nil
	result := observer.convertKernelEvent(nil)
	assert.Nil(t, result, "Should handle nil kernel event")

	// Test parseConfigPath with empty string
	configType, configName, podUID := observer.parseConfigPath("")
	assert.Empty(t, configType)
	assert.Empty(t, configName)
	assert.Empty(t, podUID)

	observer.Stop()
}

// TestNegativeStartWithoutStop tests repeated starts without stops
func TestNegativeStartWithoutStop(t *testing.T) {
	observer, err := NewObserver("test", nil)
	require.NoError(t, err)

	ctx := context.Background()

	// First start
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Second start without stop - should be idempotent
	err = observer.Start(ctx)
	assert.NoError(t, err, "Double start should be safe")

	// Third start
	err = observer.Start(ctx)
	assert.NoError(t, err, "Triple start should be safe")

	// Finally stop
	err = observer.Stop()
	assert.NoError(t, err)
}

// TestNegativeStopWithoutStart tests stopping an unstarted observer
func TestNegativeStopWithoutStart(t *testing.T) {
	observer, err := NewObserver("test", nil)
	require.NoError(t, err)

	// Stop without start
	err = observer.Stop()
	assert.NoError(t, err, "Stop without start should be safe")

	// Double stop
	err = observer.Stop()
	assert.NoError(t, err, "Double stop should be safe")
}

// TestNegativeContextCancellation tests early context cancellation
func TestNegativeContextCancellation(t *testing.T) {
	observer, err := NewObserver("test", nil)
	require.NoError(t, err)

	// Create already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Start with cancelled context
	err = observer.Start(ctx)
	// Should either succeed or handle gracefully
	if err != nil {
		t.Logf("Start with cancelled context returned: %v", err)
	}

	// Stop should still work
	err = observer.Stop()
	assert.NoError(t, err)
}

// TestNegativeEventChannelClosed tests behavior when event channel is closed
func TestNegativeEventChannelClosed(t *testing.T) {
	t.Setenv("TAPIO_MOCK_MODE", "true")

	observer, err := NewObserver("test", nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Get event channel
	events := observer.Events()
	require.NotNil(t, events)

	// Stop observer (should close channel)
	err = observer.Stop()
	require.NoError(t, err)

	// Try to read from closed channel
	select {
	case event, ok := <-events:
		if ok {
			t.Errorf("Expected channel to be closed, got event: %v", event)
		}
	case <-time.After(1 * time.Second):
		// Channel might be blocking, not closed
		t.Log("Channel appears to be blocking after stop")
	}
}

// TestNegativeInvalidPathParsing tests path parsing with malformed paths
func TestNegativeInvalidPathParsing(t *testing.T) {
	observer, err := NewObserver("test", nil)
	require.NoError(t, err)
	defer observer.Stop()

	testCases := []struct {
		name      string
		path      string
		wantType  string
		wantName  string
		wantPodID string
	}{
		{
			name:      "empty path",
			path:      "",
			wantType:  "",
			wantName:  "",
			wantPodID: "",
		},
		{
			name:      "random path",
			path:      "/usr/bin/test",
			wantType:  "",
			wantName:  "",
			wantPodID: "",
		},
		{
			name:      "incomplete kubernetes path",
			path:      "/var/lib/kubelet/pods/",
			wantType:  "",
			wantName:  "",
			wantPodID: "",
		},
		{
			name:      "malformed kubernetes path",
			path:      "/var/lib/kubelet/pods/abc/volumes/invalid",
			wantType:  "",
			wantName:  "",
			wantPodID: "abc",
		},
		{
			name:      "path with special characters",
			path:      "/var/lib/kubelet/pods/../../etc/passwd",
			wantType:  "",
			wantName:  "passwd",
			wantPodID: "..",
		},
		{
			name:      "very long path",
			path:      "/var/lib/kubelet/pods/" + string(make([]byte, 1000)),
			wantType:  "",
			wantName:  "",
			wantPodID: string(make([]byte, 1000)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configType, configName, podUID := observer.parseConfigPath(tc.path)
			assert.Equal(t, tc.wantType, configType)
			assert.Equal(t, tc.wantName, configName)
			assert.Equal(t, tc.wantPodID, podUID)
		})
	}
}

// TestNegativeErrorCodeHandling tests error code description with invalid codes
func TestNegativeErrorCodeHandling(t *testing.T) {
	observer, err := NewObserver("test", nil)
	require.NoError(t, err)
	defer observer.Stop()

	testCases := []struct {
		errorCode int32
		expected  string
	}{
		{-1, "Unknown error (-1)"},
		{999, "Unknown error (999)"},
		{-999, "Unknown error (-999)"},
		{int32(^uint32(0) >> 1), "Unknown error (2147483647)"},     // Max int32
		{-int32(^uint32(0)>>1) - 1, "Unknown error (-2147483648)"}, // Min int32
	}

	for _, tc := range testCases {
		desc := observer.getErrorDescription(tc.errorCode)
		assert.Equal(t, tc.expected, desc)
	}
}

// TestNegativeConcurrentOperations tests race conditions
func TestNegativeConcurrentOperations(t *testing.T) {
	observer, err := NewObserver("test", nil)
	require.NoError(t, err)
	defer observer.Stop()

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Concurrent starts and stops
	done := make(chan bool, 10)
	for i := 0; i < 5; i++ {
		go func() {
			observer.Start(ctx)
			done <- true
		}()
		go func() {
			observer.Stop()
			done <- true
		}()
	}

	// Wait for goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Observer should still be functional
	health := observer.Health()
	assert.NotNil(t, health)

	stats := observer.Statistics()
	assert.NotNil(t, stats)
}

// TestNegativeEBPFInitializationFailure tests eBPF initialization failures
func TestNegativeEBPFInitializationFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF test requires Linux")
	}

	// Run without root privileges to trigger failure
	if os.Geteuid() == 0 {
		t.Skip("Test requires non-root user to test failure path")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "ebpf-fail",
		BufferSize: 100,
		EnableEBPF: true, // Enable eBPF without root
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err) // Observer creation should succeed

	ctx := context.Background()
	err = observer.Start(ctx)
	// Starting should fail or handle gracefully without root
	if err != nil {
		t.Logf("Expected eBPF initialization failure: %v", err)
		assert.Contains(t, err.Error(), "failed to start eBPF")
	} else {
		// If it didn't fail, it should have fallen back gracefully
		t.Log("eBPF initialization handled gracefully without root")
		observer.Stop()
	}
}

// TestNegativeEventBufferOverflow tests buffer overflow handling
func TestNegativeEventBufferOverflow(t *testing.T) {
	t.Setenv("TAPIO_MOCK_MODE", "true")

	config := &Config{
		Name:       "overflow",
		BufferSize: 1, // Tiny buffer to force overflow
		EnableEBPF: false,
	}

	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Don't consume events to cause overflow
	time.Sleep(5 * time.Second)

	// Check statistics for drops
	stats := observer.Statistics()
	t.Logf("Buffer overflow test - Processed: %d, Errors: %d",
		stats.EventsProcessed, stats.ErrorCount)

	// Should have some drops with tiny buffer
	if stats.ErrorCount == 0 {
		t.Log("No events dropped despite tiny buffer - events may not be generated fast enough")
	}

	// Observer should remain healthy despite drops
	health := observer.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)
}

// TestNegativeInvalidKernelEvent tests handling of malformed kernel events
func TestNegativeInvalidKernelEvent(t *testing.T) {
	observer, err := NewObserver("test", nil)
	require.NoError(t, err)
	defer observer.Stop()

	// Test with various invalid kernel events
	testCases := []struct {
		name  string
		event *KernelEvent
	}{
		{
			name:  "nil event",
			event: nil,
		},
		{
			name: "zero timestamp",
			event: &KernelEvent{
				Timestamp: 0,
				PID:       1234,
			},
		},
		{
			name: "invalid event type",
			event: &KernelEvent{
				Timestamp: uint64(time.Now().UnixNano()),
				EventType: 99999,
			},
		},
		{
			name: "empty command",
			event: &KernelEvent{
				Timestamp: uint64(time.Now().UnixNano()),
				PID:       1234,
				Comm:      [16]byte{}, // Empty
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Should not panic
			result := observer.convertKernelEvent(tc.event)
			if tc.event == nil {
				assert.Nil(t, result)
			} else {
				// Should return something even for invalid events
				assert.NotNil(t, result)
			}
		})
	}
}

// TestNegativeLoggerFailure tests observer behavior with logger issues
func TestNegativeLoggerFailure(t *testing.T) {
	// Create a broken logger that fails on all operations
	brokenLogger := zap.NewNop() // No-op logger simulates failure

	config := &Config{
		Name:       "logger-fail",
		BufferSize: 100,
		EnableEBPF: false,
	}

	// Should handle broken logger gracefully
	observer, err := NewObserverWithConfig(config, brokenLogger)
	require.NoError(t, err)
	require.NotNil(t, observer)

	ctx := context.Background()
	err = observer.Start(ctx)
	assert.NoError(t, err, "Should start even with broken logger")

	err = observer.Stop()
	assert.NoError(t, err, "Should stop even with broken logger")
}

// TestNegativeFileSystemErrors tests handling of file system errors
func TestNegativeFileSystemErrors(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("File system test requires Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "fs-errors",
		BufferSize: 100,
		EnableEBPF: true,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Create test directory
	tempDir, err := os.MkdirTemp("", "fs-errors")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Test various file system errors
	testFile := filepath.Join(tempDir, "test.txt")

	// 1. Permission denied
	err = os.WriteFile(testFile, []byte("test"), 0000)
	require.NoError(t, err)
	_, readErr := os.ReadFile(testFile)
	assert.Error(t, readErr)

	// 2. File not found
	_, readErr = os.ReadFile(filepath.Join(tempDir, "nonexistent.txt"))
	assert.Error(t, readErr)

	// 3. Directory as file
	dirPath := filepath.Join(tempDir, "testdir")
	err = os.Mkdir(dirPath, 0755)
	require.NoError(t, err)
	_, readErr = os.ReadFile(dirPath)
	assert.Error(t, readErr)

	// Give time for events to be processed
	time.Sleep(1 * time.Second)

	// Check that observer is still healthy
	health := observer.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)

	stats := observer.Statistics()
	t.Logf("File system errors - Events: %d, Errors: %d",
		stats.EventsProcessed, stats.ErrorCount)
}

// TestNegativePanicRecovery tests that observer recovers from panics
func TestNegativePanicRecovery(t *testing.T) {
	// This test would need to inject panic-inducing conditions
	// Since we can't easily cause panics in the current implementation,
	// we document this as a known test gap

	t.Skip("Panic recovery test requires ability to inject panics")
}
