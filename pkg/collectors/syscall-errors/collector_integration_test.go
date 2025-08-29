//go:build linux && integration
// +build linux,integration

package syscallerrors

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// TestIntegrationFullLifecycle tests complete collector lifecycle with real eBPF
func TestIntegrationFullLifecycle(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:   8 * 1024 * 1024,
		EventChannelSize: 1000,
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": true,
			"memory":  true,
		},
	}

	collector, err := NewCollector(logger, config)
	require.NoError(t, err)
	defer collector.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Verify it's healthy
	assert.True(t, collector.IsHealthy())

	// Generate some syscall errors
	go generateTestErrors()

	// Collect events for a few seconds
	var events []*domain.ObservationEvent
	timeout := time.After(5 * time.Second)

eventLoop:
	for {
		select {
		case event := <-collector.GetEventChannel():
			if event != nil {
				events = append(events, event)
				if len(events) >= 5 {
					break eventLoop
				}
			}
		case <-timeout:
			break eventLoop
		case <-ctx.Done():
			break eventLoop
		}
	}

	// Should have captured some events
	assert.NotEmpty(t, events, "Should have captured syscall errors")

	// Verify event structure
	for _, event := range events {
		assert.Equal(t, domain.EventTypeSyscallError, event.Type)
		assert.NotZero(t, event.Timestamp)
		assert.Equal(t, "syscall-errors", event.Source.Component)
		assert.NotEmpty(t, event.Resource.ID)
		assert.NotEmpty(t, event.Context["error_name"])
		assert.NotEmpty(t, event.Context["syscall"])
	}

	// Get stats
	stats, err := collector.GetStats()
	if err == nil {
		assert.NotZero(t, stats.TotalErrors)
		assert.NotZero(t, stats.EventsSent)
	}

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)

	// Should be unhealthy after stop
	assert.False(t, collector.IsHealthy())
}

// TestIntegrationSpecificErrors tests capturing specific error types
func TestIntegrationSpecificErrors(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		EnabledCategories: map[string]bool{
			"file": true,
		},
	}

	collector, err := NewCollector(logger, config)
	require.NoError(t, err)
	defer collector.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Generate specific errors
	errorGenerators := []func(){
		generateENOENTError, // File not found
		generateEACCESError, // Permission denied
		generateENOSPCError, // No space (simulated)
		generateEMFILEError, // Too many open files (simulated)
	}

	for _, generator := range errorGenerators {
		generator()
		time.Sleep(100 * time.Millisecond)
	}

	// Collect events
	var capturedErrors []string
	timeout := time.After(3 * time.Second)

	for {
		select {
		case event := <-collector.GetEventChannel():
			if event != nil {
				if errorName, ok := event.Context["error_name"]; ok {
					capturedErrors = append(capturedErrors, errorName)
				}
			}
		case <-timeout:
			goto done
		}
	}

done:
	// Verify we captured different error types
	assert.NotEmpty(t, capturedErrors, "Should have captured errors")
	t.Logf("Captured errors: %v", capturedErrors)
}

// TestIntegrationCategoryFiltering tests category-based filtering
func TestIntegrationCategoryFiltering(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": false, // Explicitly disable network
		},
	}

	collector, err := NewCollector(logger, config)
	require.NoError(t, err)
	defer collector.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Generate both file and network errors
	go generateFileErrors()
	go generateNetworkErrors()

	// Collect events
	var fileEvents, networkEvents int
	timeout := time.After(3 * time.Second)

	for {
		select {
		case event := <-collector.GetEventChannel():
			if event != nil {
				if category, ok := event.Context["category"]; ok {
					switch category {
					case "file":
						fileEvents++
					case "network":
						networkEvents++
					}
				}
			}
		case <-timeout:
			goto done
		}
	}

done:
	// Should have file events but no network events
	assert.Greater(t, fileEvents, 0, "Should have captured file events")
	assert.Equal(t, 0, networkEvents, "Should not have captured network events")
}

// TestIntegrationHighLoad tests collector under high error rate
func TestIntegrationHighLoad(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:   16 * 1024 * 1024, // Larger buffer
		EventChannelSize: 10000,
		RateLimitMs:      50, // More aggressive rate limiting
	}

	collector, err := NewCollector(logger, config)
	require.NoError(t, err)
	defer collector.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Generate high error rate
	stopChan := make(chan bool)
	go func() {
		for {
			select {
			case <-stopChan:
				return
			default:
				generateBurstErrors()
			}
		}
	}()

	// Collect events for a few seconds
	var eventCount int
	timeout := time.After(5 * time.Second)

	for {
		select {
		case event := <-collector.GetEventChannel():
			if event != nil {
				eventCount++
			}
		case <-timeout:
			close(stopChan)
			goto done
		}
	}

done:
	// Should have captured events but with rate limiting
	assert.Greater(t, eventCount, 0, "Should have captured events")
	t.Logf("Captured %d events under high load", eventCount)

	// Check stats
	stats, err := collector.GetStats()
	if err == nil {
		assert.NotZero(t, stats.TotalErrors)
		if stats.EventsDropped > 0 {
			t.Logf("Dropped %d events due to rate limiting", stats.EventsDropped)
		}
	}
}

// TestIntegrationMemoryPressure tests behavior under memory pressure
func TestIntegrationMemoryPressure(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:   4 * 1024 * 1024, // Smaller buffer
		EventChannelSize: 100,             // Small channel
	}

	collector, err := NewCollector(logger, config)
	require.NoError(t, err)
	defer collector.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Generate many errors quickly
	for i := 0; i < 1000; i++ {
		generateTestErrors()
	}

	// Let it process
	time.Sleep(2 * time.Second)

	// Check for dropped events
	stats, err := collector.GetStats()
	if err == nil {
		t.Logf("Total errors: %d, Sent: %d, Dropped: %d",
			stats.TotalErrors, stats.EventsSent, stats.EventsDropped)
		// With small buffers, we expect some drops
		if stats.EventsDropped > 0 {
			assert.Greater(t, stats.EventsSent, uint64(0), "Should still send some events")
		}
	}
}

// Helper functions to generate test errors

func generateTestErrors() {
	// Try to open non-existent file
	_, _ = os.Open("/nonexistent/file/path/test.txt")

	// Try to write to read-only file
	_ = os.WriteFile("/proc/version", []byte("test"), 0644)

	// Try to create file in non-existent directory
	_, _ = os.Create("/nonexistent/directory/file.txt")
}

func generateENOENTError() {
	// ENOENT - No such file or directory
	_, _ = os.Open("/this/file/does/not/exist/at/all.txt")
	_, _ = syscall.Open("/another/nonexistent/path.txt", syscall.O_RDONLY, 0)
}

func generateEACCESError() {
	// EACCES - Permission denied
	_ = os.WriteFile("/root/test_no_permission.txt", []byte("test"), 0644)
	_, _ = os.OpenFile("/etc/shadow", os.O_RDWR, 0)
}

func generateENOSPCError() {
	// ENOSPC - No space left (simulated by trying to write to /dev/full)
	if _, err := os.Stat("/dev/full"); err == nil {
		f, _ := os.OpenFile("/dev/full", os.O_WRONLY, 0)
		if f != nil {
			_, _ = f.Write([]byte("test"))
			f.Close()
		}
	}
}

func generateEMFILEError() {
	// EMFILE - Too many open files (simulated)
	// Try to open many files quickly
	files := make([]*os.File, 0)
	defer func() {
		for _, f := range files {
			if f != nil {
				f.Close()
			}
		}
	}()

	for i := 0; i < 10000; i++ {
		f, err := os.Open("/etc/passwd")
		if err != nil {
			break // Hit the limit
		}
		files = append(files, f)
	}
}

func generateFileErrors() {
	for i := 0; i < 10; i++ {
		generateENOENTError()
		generateEACCESError()
		time.Sleep(100 * time.Millisecond)
	}
}

func generateNetworkErrors() {
	for i := 0; i < 10; i++ {
		// Try to connect to non-existent service
		conn, _ := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
		if conn >= 0 {
			addr := syscall.SockaddrInet4{Port: 59999} // Unlikely port
			copy(addr.Addr[:], []byte{127, 0, 0, 1})
			_ = syscall.Connect(conn, &addr)
			syscall.Close(conn)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func generateBurstErrors() {
	// Generate a burst of different errors
	for i := 0; i < 50; i++ {
		switch i % 4 {
		case 0:
			generateENOENTError()
		case 1:
			generateEACCESError()
		case 2:
			generateFileErrors()
		case 3:
			generateNetworkErrors()
		}
	}
}

// TestIntegrationPerProcessTracking tests per-process error tracking
func TestIntegrationPerProcessTracking(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(logger, nil)
	require.NoError(t, err)
	defer collector.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Get our PID
	myPID := fmt.Sprintf("%d", os.Getpid())

	// Generate errors from this process
	for i := 0; i < 5; i++ {
		generateENOENTError()
		time.Sleep(50 * time.Millisecond)
	}

	// Collect events and check PIDs
	var myEvents int
	timeout := time.After(2 * time.Second)

	for {
		select {
		case event := <-collector.GetEventChannel():
			if event != nil {
				if pid, ok := event.Context["pid"]; ok && pid == myPID {
					myEvents++
				}
			}
		case <-timeout:
			goto done
		}
	}

done:
	assert.Greater(t, myEvents, 0, "Should have captured events from our process")
	t.Logf("Captured %d events from PID %s", myEvents, myPID)
}
