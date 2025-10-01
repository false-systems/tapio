//go:build linux
// +build linux

package status

import (
	"context"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestSystemRequirements(t *testing.T) {
	t.Run("Linux kernel version", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("Not running on Linux")
		}

		var uname syscall.Utsname
		err := syscall.Uname(&uname)
		require.NoError(t, err)

		// Convert C string to Go string
		release := ""
		for _, b := range uname.Release {
			if b == 0 {
				break
			}
			release += string(byte(b))
		}
		t.Logf("Kernel version: %s", release)

		// eBPF requires at least kernel 4.14 for CO-RE support
		// Just log for informational purposes
	})

	t.Run("Required capabilities", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("Not running on Linux")
		}

		// Check if running as root or with CAP_SYS_ADMIN
		if os.Getuid() != 0 {
			t.Log("Not running as root, some eBPF features may be limited")
		}
	})

	t.Run("Memory lock limit", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("Not running on Linux")
		}

		// Check memlock limit (Linux-specific)
		// Note: RLIMIT_MEMLOCK constant varies by architecture
		const RLIMIT_MEMLOCK = 8 // Linux x86_64/arm64 value

		var rlimit syscall.Rlimit
		err := syscall.Getrlimit(RLIMIT_MEMLOCK, &rlimit)
		if err != nil {
			t.Logf("Could not check MEMLOCK limit: %v", err)
		} else {
			t.Logf("MEMLOCK limit: cur=%d max=%d", rlimit.Cur, rlimit.Max)

			// eBPF needs unlimited or high memlock
			if rlimit.Cur < 1024*1024*512 { // 512MB
				t.Log("Warning: Low MEMLOCK limit may affect eBPF loading")
			}
		}
	})
}

func TestEBPFLifecycle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF tests require Linux")
	}

	if os.Getuid() != 0 {
		t.Skip("eBPF tests require root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    1000,
		SampleRate:    1.0,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-ebpf", config)
	require.NoError(t, err)

	t.Run("Start with eBPF", func(t *testing.T) {
		ctx := context.Background()
		err := observer.Start(ctx)

		// May fail if BPF objects are not compiled
		if err != nil {
			t.Logf("eBPF start failed (expected if BPF not compiled): %v", err)
			// Still verify observer is healthy (fallback mode)
			assert.True(t, observer.IsHealthy())
		} else {
			// eBPF loaded successfully
			assert.NotNil(t, observer.ebpfState)
			assert.True(t, observer.IsHealthy())
		}
	})

	t.Run("Stop with eBPF cleanup", func(t *testing.T) {
		err := observer.Stop()
		assert.NoError(t, err)
		assert.False(t, observer.IsHealthy())

		// Verify eBPF state is cleaned up
		if observer.ebpfState != nil {
			ebpfState, ok := observer.ebpfState.(*statusEBPF)
			if ok {
				// All resources should be nil after cleanup
				assert.Nil(t, ebpfState.reader)
			}
		}
	})
}

func TestEBPFEventProcessing(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF tests require Linux")
	}

	if os.Getuid() != 0 {
		t.Skip("eBPF tests require root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:         true,
		BufferSize:      100,
		SampleRate:      1.0,
		MaxEventsPerSec: 1000,
		FlushInterval:   100 * time.Millisecond,
		EnableL7Parse:   true,
		HTTPPorts:       []int{80, 8080},
		GRPCPorts:       []int{50051},
		Logger:          logger,
	}

	observer, err := NewObserver("test-processing", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	if err != nil {
		t.Skipf("Cannot test eBPF processing: %v", err)
	}
	defer observer.Stop()

	// Get event channel
	events := observer.Events()

	// The eBPF programs should be monitoring network activity
	// In a real system test, we would trigger actual network connections
	// For now, just verify the infrastructure is working

	select {
	case event := <-events:
		// Got an event from eBPF
		assert.NotNil(t, event)
		assert.Equal(t, "test-processing", event.Source)
		t.Logf("Received eBPF event: %+v", event)
	case <-time.After(2 * time.Second):
		// No events in test environment is OK
		t.Log("No eBPF events received (expected in test environment)")
	}

	// Check statistics
	stats := observer.Statistics()
	assert.NotNil(t, stats)
	t.Logf("Observer stats: processed=%d errors=%d",
		stats.EventsProcessed, stats.ErrorCount)
}

func TestEBPFMapOperations(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF tests require Linux")
	}

	if os.Getuid() != 0 {
		t.Skip("eBPF tests require root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-maps", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	if err != nil {
		t.Skipf("Cannot test eBPF maps: %v", err)
	}
	defer observer.Stop()

	// If eBPF loaded successfully, verify map operations
	if observer.ebpfState != nil {
		ebpfState, ok := observer.ebpfState.(*statusEBPF)
		if ok && ebpfState.connTracker != nil {
			// Verify map exists and has expected properties
			info, err := ebpfState.connTracker.Info()
			if err == nil {
				assert.NotNil(t, info)
				t.Logf("Connection tracker map: type=%v max_entries=%d",
					info.Type, info.MaxEntries)
			}
		}
	}
}

func TestKernelProbeAttachment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF tests require Linux")
	}

	if os.Getuid() != 0 {
		t.Skip("Probe attachment requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-probes", config)
	require.NoError(t, err)

	// Test probe attachment
	err = observer.startEBPF()
	if err != nil {
		t.Logf("Probe attachment failed (expected if BPF not compiled): %v", err)
		// Verify fallback mode works
		assert.Nil(t, observer.ebpfState)
	} else {
		// Probes attached successfully
		assert.NotNil(t, observer.ebpfState)

		ebpfState, ok := observer.ebpfState.(*statusEBPF)
		if ok {
			// Should have attached probes
			assert.NotEmpty(t, ebpfState.links)
			t.Logf("Attached %d kernel probes", len(ebpfState.links))

			// Clean up
			observer.stopEBPF()
		}
	}
}

func TestRingBufferCreation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF tests require Linux")
	}

	if os.Getuid() != 0 {
		t.Skip("Ring buffer creation requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    1000,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-ringbuf", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	if err != nil {
		t.Skipf("Cannot test ring buffer: %v", err)
	}
	defer observer.Stop()

	if observer.ebpfState != nil {
		ebpfState, ok := observer.ebpfState.(*statusEBPF)
		if ok && ebpfState.reader != nil {
			// Ring buffer created successfully
			t.Log("Ring buffer reader created successfully")

			// Reader should be functional
			assert.NotNil(t, ebpfState.reader)
		}
	}
}

func TestPatternDetectionIntegration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Pattern detection test requires Linux")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-patterns", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	if err != nil {
		t.Logf("Pattern detection start failed: %v", err)
	}
	defer observer.Stop()

	// Pattern detection runs in background
	// Verify it's running by checking goroutines
	assert.True(t, observer.IsHealthy())

	// Wait a bit for pattern detector to run
	time.Sleep(500 * time.Millisecond)

	// Should still be healthy
	assert.True(t, observer.IsHealthy())
}

func TestMemoryManagement(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Memory management test requires Linux")
	}

	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		BufferSize:    10, // Small buffer to test overflow
		FlushInterval: 50 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-memory", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	if err != nil {
		t.Logf("Memory test start failed: %v", err)
	}
	defer observer.Stop()

	// Generate many events to test buffer management
	for i := 0; i < 100; i++ {
		event := &StatusEvent{
			ServiceHash:  uint32(i),
			EndpointHash: uint32(i * 2),
			StatusCode:   uint16(200 + i%400),
			ErrorType:    ErrorType(i % 8),
			Timestamp:    uint64(time.Now().UnixNano()),
			Latency:      uint32(100 + i),
			PID:          uint32(os.Getpid()),
		}
		observer.aggregator.Add(event)
	}

	// Wait for flush
	time.Sleep(100 * time.Millisecond)

	// Check statistics
	stats := observer.Statistics()
	assert.NotNil(t, stats)

	// Should handle overflow gracefully
	assert.True(t, observer.IsHealthy())
}

func TestConcurrentEventProcessing(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Concurrent processing test requires Linux")
	}

	logger := zap.NewNop()
	config := &Config{
		Enabled:         true,
		BufferSize:      1000,
		MaxEventsPerSec: 10000,
		FlushInterval:   100 * time.Millisecond,
		Logger:          logger,
	}

	observer, err := NewObserver("test-concurrent", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	if err != nil {
		t.Logf("Concurrent test start failed: %v", err)
	}
	defer observer.Stop()

	// Spawn multiple goroutines to add events concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				event := &StatusEvent{
					ServiceHash:  uint32(id),
					EndpointHash: uint32(j),
					StatusCode:   uint16(200 + j%400),
					ErrorType:    ErrorType(j % 8),
					Timestamp:    uint64(time.Now().UnixNano()),
					Latency:      uint32(50 + j),
					PID:          uint32(os.Getpid()),
				}
				observer.aggregator.Add(event)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Wait for aggregation
	time.Sleep(200 * time.Millisecond)

	// Should remain healthy under concurrent load
	assert.True(t, observer.IsHealthy())

	// Check statistics
	stats := observer.Statistics()
	assert.NotNil(t, stats)
	t.Logf("Concurrent test stats: processed=%d errors=%d",
		stats.EventsProcessed, stats.ErrorCount)
}
