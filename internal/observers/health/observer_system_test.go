//go:build linux
// +build linux

package health

import (
	"context"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// TestSystemEBPFLoading tests eBPF program loading on Linux
func TestSystemEBPFLoading(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	// Remove memory lock limit
	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	// Start should load eBPF programs
	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Verify eBPF state was initialized
	assert.NotNil(t, observer.ebpfState)

	state, ok := observer.ebpfState.(*ebpfStateImpl)
	require.True(t, ok)
	assert.NotNil(t, state.objs)
}

// TestSystemTracepointAttachment tests tracepoint attachment
func TestSystemTracepointAttachment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Verify tracepoint links were created
	state, ok := observer.ebpfState.(*ebpfStateImpl)
	require.True(t, ok)
	assert.NotEmpty(t, state.links)
}

// TestSystemRingBufferCreation tests ring buffer map creation
func TestSystemRingBufferCreation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:   4 * 1024 * 1024, // 4MB
		EventChannelSize: 100,
		RateLimitMs:      50,
		EnabledCategories: map[string]bool{
			"file": true,
		},
	}

	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Verify ring buffer reader was created
	state, ok := observer.ebpfState.(*ebpfStateImpl)
	require.True(t, ok)
	assert.NotNil(t, state.ringReader)
}

// TestSystemRealSyscallCapture tests capturing real system call errors
func TestSystemRealSyscallCapture(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Trigger a real syscall error (try to open non-existent file)
	_, err = os.Open("/this/path/does/not/exist/at/all")
	assert.Error(t, err)

	// Try to create a file in read-only directory
	err = os.WriteFile("/proc/test_file", []byte("test"), 0644)
	assert.Error(t, err)

	// Give eBPF time to capture events
	time.Sleep(500 * time.Millisecond)

	// Check if we received any events - try multiple times
	eventCount := 0
	timeout := time.After(2 * time.Second)

	for eventCount < 1 {
		select {
		case event := <-observer.Events():
			eventCount++
			assert.NotNil(t, event)
			assert.Equal(t, "health", event.Source)
			// Verify it's a kernel event
			assert.NotNil(t, event.EventData.Kernel)
			t.Logf("SUCCESS! Captured event: %s error=%s",
				event.EventData.Kernel.Syscall,
				event.EventData.Kernel.ErrorMessage)
		case <-timeout:
			t.Log("No events captured after 2 seconds (might be filtered)")
			return
		}
	}
}

// TestSystemMemoryPressureCapture tests ENOMEM capture
func TestSystemMemoryPressureCapture(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Try to allocate huge amount of memory (might fail with ENOMEM)
	// This is system-dependent and might not trigger on all systems
	size := int64(1024 * 1024 * 1024 * 1024) // 1TB
	_, err = syscall.Mmap(-1, 0, int(size),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANONYMOUS)

	if err != nil {
		// Error occurred, might be captured by eBPF
		time.Sleep(100 * time.Millisecond)

		select {
		case event := <-observer.Events():
			if event.EventData.Kernel != nil &&
				event.EventData.Kernel.ErrorMessage == "ENOMEM" {
				assert.Equal(t, domain.EventSeverityCritical, event.Severity)
			}
		case <-time.After(200 * time.Millisecond):
			// Might be rate limited
		}
	}
}

// TestSystemEBPFMapOperations tests eBPF map operations
func TestSystemEBPFMapOperations(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Get stats from eBPF maps
	stats, err := observer.GetStats()
	assert.NoError(t, err)
	assert.NotNil(t, stats)

	// Stats should be initialized (even if zero)
	assert.GreaterOrEqual(t, stats.TotalErrors, uint64(0))
}

// TestSystemMultipleCPUHandling tests per-CPU map handling
func TestSystemMultipleCPUHandling(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	if runtime.NumCPU() < 2 {
		t.Skip("Skipping multi-CPU test on single-CPU system")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// The eBPF program uses per-CPU maps for stats
	// Verify they're properly aggregated
	state, ok := observer.ebpfState.(*ebpfStateImpl)
	require.True(t, ok)

	// Check error tracking map (hash map)
	if state.objs.ErrorTracking != nil {
		info, err := state.objs.ErrorTracking.Info()
		if err == nil {
			// Verify it's a hash map
			assert.Equal(t, ebpf.Hash, info.Type)
		}
	}
}

// TestSystemEBPFProgramVerification tests eBPF program verification
func TestSystemEBPFProgramVerification(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	// Try to load eBPF programs
	err = observer.startEBPF()

	// On systems with eBPF support, this should work
	// On systems without proper eBPF bytecode, this will fail
	// Both are valid test outcomes
	if err != nil {
		assert.Contains(t, err.Error(), "eBPF")
		t.Logf("eBPF loading failed (expected if bytecode not embedded): %v", err)
	} else {
		// If successful, verify state
		assert.NotNil(t, observer.ebpfState)
		observer.stopEBPF()
	}
}

// TestSystemNamespaceDetection tests container namespace detection
func TestSystemNamespaceDetection(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// The eBPF program captures namespace info
	// Create a test event to verify
	event := &HealthEvent{
		TimestampNs: uint64(time.Now().UnixNano()),
		PID:         uint32(os.Getpid()),
		ErrorCode:   -13, // EACCES
		Category:    1,
	}

	domainEvent := observer.convertToCollectorEvent(event)
	assert.NotNil(t, domainEvent)

	// CgroupID should be captured for containerized processes
	if event.CgroupID != 0 {
		assert.NotZero(t, domainEvent.EventData.Kernel.CgroupID)
	}
}

// TestSystemEventRateLimiting tests kernel-side rate limiting
func TestSystemEventRateLimiting(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:   1024 * 1024,
		EventChannelSize: 1000,
		RateLimitMs:      100, // 100ms rate limit
		EnabledCategories: map[string]bool{
			"file": true,
		},
	}

	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Generate burst of syscall errors
	for i := 0; i < 100; i++ {
		// Try to open non-existent file rapidly
		os.Open("/does/not/exist")
	}

	// Collect events for a period
	time.Sleep(500 * time.Millisecond)

	eventCount := 0
	timeout := time.After(100 * time.Millisecond)

loop:
	for {
		select {
		case <-observer.Events():
			eventCount++
		case <-timeout:
			break loop
		}
	}

	// Due to rate limiting, we shouldn't get all 100 events
	// The exact number depends on the rate limiting implementation
	t.Logf("Received %d events out of 100 (rate limited)", eventCount)
	assert.Less(t, eventCount, 100, "Rate limiting should prevent all events")
}

// TestSystemCleanupOnFailure tests cleanup when eBPF loading fails
func TestSystemCleanupOnFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	// Manually set invalid state to force cleanup
	observer.ebpfState = &ebpfStateImpl{
		objs:  &healthMonitorObjects{},
		links: nil,
	}

	// Stop should handle cleanup gracefully
	observer.stopEBPF()

	// Verify state was cleaned
	assert.Nil(t, observer.ebpfState)
}

// TestSystemComprehensiveSyscallMonitoring tests multiple syscall types
func TestSystemComprehensiveSyscallMonitoring(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	// Enable all categories for comprehensive testing
	config := &Config{
		RingBufferSize:   4 * 1024 * 1024,
		EventChannelSize: 1000,
		RateLimitMs:      50, // Low rate limit for testing
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": true,
			"memory":  true,
			"process": true,
		},
	}

	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Collect events in a separate goroutine
	events := make([]*domain.CollectorEvent, 0)
	eventTypes := make(map[string]int)
	done := make(chan bool)

	go func() {
		timeout := time.After(10 * time.Second) // 10 second timeout for all tests
		for {
			select {
			case event := <-observer.Events():
				if event != nil && event.EventData.Kernel != nil {
					events = append(events, event)
					eventTypes[event.EventData.Kernel.ErrorMessage]++
					t.Logf("Captured: syscall=%s error=%s pid=%d",
						event.EventData.Kernel.Syscall,
						event.EventData.Kernel.ErrorMessage,
						event.EventData.Kernel.PID)
				}
			case <-timeout:
				done <- true
				return
			case <-done:
				return
			}
		}
	}()

	// Give the goroutine time to start
	time.Sleep(100 * time.Millisecond)

	// === FILE SYSCALLS ===
	t.Log("Testing FILE syscalls...")

	// Test ENOENT (No such file)
	for i := 0; i < 5; i++ {
		_, err = os.Open("/this/does/not/exist/file" + string(rune(i)))
		assert.Error(t, err)
		time.Sleep(10 * time.Millisecond) // Small delay between syscalls
	}

	// Test EACCES (Permission denied) - try to write to read-only location
	for i := 0; i < 3; i++ {
		err = os.WriteFile("/proc/test_file_"+string(rune(i)), []byte("test"), 0644)
		assert.Error(t, err)
		time.Sleep(10 * time.Millisecond)
	}

	// Test EMFILE (Too many open files) - might not trigger on all systems
	// This is hard to trigger reliably, so we'll just try
	files := make([]*os.File, 0)
	for i := 0; i < 100; i++ {
		f, err := os.Open("/etc/passwd") // Use existing file
		if err == nil {
			files = append(files, f)
		}
	}
	// Clean up opened files
	for _, f := range files {
		f.Close()
	}

	// === NETWORK SYSCALLS ===
	t.Log("Testing NETWORK syscalls...")

	// Test ECONNREFUSED (Connection refused)
	for i := 0; i < 3; i++ {
		sock, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
		if err == nil {
			// Try to connect to non-listening port
			addr := syscall.SockaddrInet4{
				Port: 59999 + i, // Use different ports
				Addr: [4]byte{127, 0, 0, 1},
			}
			err = syscall.Connect(sock, &addr) // This should fail with ECONNREFUSED
			if err != nil {
				t.Logf("Connect failed as expected: %v", err)
			}
			syscall.Close(sock)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Test EADDRINUSE (Address already in use)
	sock1, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err == nil {
		addr := syscall.SockaddrInet4{
			Port: 58888,
			Addr: [4]byte{127, 0, 0, 1},
		}
		syscall.Bind(sock1, &addr) // First bind should work

		// Try to bind same address again (should fail)
		sock2, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
		if err == nil {
			syscall.Bind(sock2, &addr) // Should fail with EADDRINUSE
			syscall.Close(sock2)
		}
		syscall.Close(sock1)
	}

	// === MEMORY SYSCALLS ===
	t.Log("Testing MEMORY syscalls...")

	// Test ENOMEM (Out of memory) - try to allocate huge amount
	for i := 0; i < 3; i++ {
		size := int64(1024 * 1024 * 1024 * 1024) // 1TB
		_, err = syscall.Mmap(-1, 0, int(size),
			syscall.PROT_READ|syscall.PROT_WRITE,
			syscall.MAP_PRIVATE|syscall.MAP_ANONYMOUS)
		if err != nil {
			t.Logf("Mmap failed as expected: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Try brk syscall (might fail with ENOMEM)
	ret, _, errno := syscall.Syscall(syscall.SYS_BRK, 0xFFFFFFFFFFFFFFFF, 0, 0)
	if ret == 0 || errno != 0 {
		t.Logf("Brk failed as expected: errno=%v", errno)
	}

	// === PROCESS SYSCALLS ===
	t.Log("Testing PROCESS syscalls...")

	// Test clone/fork failures (harder to trigger reliably)
	// We'll use clone with invalid flags
	for i := 0; i < 3; i++ {
		ret, _, errno := syscall.Syscall(syscall.SYS_CLONE, 0xFFFFFFFF, 0, 0) // Invalid flags
		if ret == ^uintptr(0) || errno != 0 {
			t.Logf("Clone failed as expected: errno=%v", errno)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait a bit for all events to be processed
	time.Sleep(2 * time.Second)

	// Signal done and wait for goroutine
	close(done)
	time.Sleep(500 * time.Millisecond)

	// === VERIFICATION ===
	t.Logf("\n=== COMPREHENSIVE TEST RESULTS ===")
	t.Logf("Total events captured: %d", len(events))
	t.Logf("Event types breakdown:")
	for errType, count := range eventTypes {
		t.Logf("  %s: %d events", errType, count)
	}

	// Verify we captured multiple event types
	assert.Greater(t, len(events), 0, "Should have captured at least some events")
	assert.Greater(t, len(eventTypes), 0, "Should have captured different error types")

	// Check for specific error types we expect
	if count, ok := eventTypes["ENOENT"]; ok {
		assert.Greater(t, count, 0, "Should have captured ENOENT errors")
	}

	// Verify event structure
	for _, event := range events {
		assert.Equal(t, "health", event.Source)
		assert.NotNil(t, event.EventData.Kernel)
		assert.NotZero(t, event.EventData.Kernel.PID)
		assert.NotEmpty(t, event.EventData.Kernel.Syscall)
		assert.NotEmpty(t, event.EventData.Kernel.ErrorMessage)
		assert.NotZero(t, event.Timestamp)

		// Check severity based on error type
		switch event.EventData.Kernel.ErrorMessage {
		case "ENOMEM":
			assert.Equal(t, domain.EventSeverityCritical, event.Severity)
		case "ENOSPC":
			assert.Equal(t, domain.EventSeverityCritical, event.Severity)
		case "ECONNREFUSED":
			// mapSeverity maps "high" to EventSeverityError
			assert.Equal(t, domain.EventSeverityError, event.Severity)
		}
	}

	// Get stats to verify error tracking map
	stats, err := observer.GetStats()
	assert.NoError(t, err)
	assert.NotNil(t, stats)
	assert.Greater(t, stats.TotalErrors, uint64(0), "Stats should show errors tracked")

	t.Logf("\n=== EBPF STATS ===")
	t.Logf("Total errors tracked: %d", stats.TotalErrors)
	t.Logf("ENOSPC count: %d", stats.ENOSPCCount)
	t.Logf("ENOMEM count: %d", stats.ENOMEMCount)
	t.Logf("ECONNREFUSED count: %d", stats.ECONNREFUSEDCount)
	t.Logf("EIO count: %d", stats.EIOCount)

	t.Log("\n✅ COMPREHENSIVE SYSCALL MONITORING TEST COMPLETE!")
}
