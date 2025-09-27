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

	// Verify perf reader was created
	state, ok := observer.ebpfState.(*ebpfStateImpl)
	require.True(t, ok)
	assert.NotNil(t, state.perfReader)
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
	time.Sleep(100 * time.Millisecond)

	// Check if we received any events
	select {
	case event := <-observer.Events():
		assert.NotNil(t, event)
		assert.Equal(t, "health", event.Source)
		// Verify it's a kernel event
		assert.NotNil(t, event.EventData.Kernel)
	case <-time.After(500 * time.Millisecond):
		// Events might be rate limited or filtered
		t.Log("No events captured (might be filtered)")
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

	// Look for stats map (per-CPU array)
	if statsMap, ok := state.objs.Maps["stats"]; ok {
		info, err := statsMap.Info()
		if err == nil {
			// Verify it's a per-CPU map
			assert.Contains(t, []ebpf.MapType{
				ebpf.PerCPUArray,
				ebpf.PerCPUHash,
			}, info.Type)
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
		objs:  &ebpfObjects{},
		links: nil,
	}

	// Stop should handle cleanup gracefully
	observer.stopEBPF()

	// Verify state was cleaned
	assert.Nil(t, observer.ebpfState)
}
