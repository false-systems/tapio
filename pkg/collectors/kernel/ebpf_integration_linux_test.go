//go:build integration && linux
// +build integration,linux

package kernel

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors/kernel/bpf"
	"go.uber.org/zap"
)

// TestEBPFProgramLoading tests eBPF program loading and attachment
func TestEBPFProgramLoading(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("eBPF program loading test requires root privileges")
	}

	// Test eBPF support detection
	supported := bpf.IsSupported()
	t.Logf("eBPF support detected: %v", supported)

	if !supported {
		t.Skip("eBPF not supported on this system")
	}

	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "ebpf-loading-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test successful loading
	err = collector.Start(ctx)
	require.NoError(t, err, "eBPF programs should load successfully")

	// Verify eBPF state is properly initialized
	require.NotNil(t, collector.ebpfState, "eBPF state should be initialized")

	state := collector.ebpfState.(*ebpfState)
	require.NotNil(t, state.objs, "eBPF objects should be loaded")
	require.NotNil(t, state.reader, "Ring buffer reader should be created")
	require.NotEmpty(t, state.links, "At least some eBPF programs should be attached")

	t.Logf("Successfully loaded %d eBPF programs", len(state.links))

	// Test cleanup
	err = collector.Stop()
	require.NoError(t, err, "eBPF cleanup should succeed")
}

// TestNetworkEventCapture tests actual network event capture
func TestNetworkEventCapture(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Network event capture test requires root privileges")
	}

	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "network-capture-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer func() {
		collector.Stop()
	}()

	// Monitor events
	eventsCh := collector.Events()
	var networkEvents []KernelEvent
	var totalEvents int64

	done := make(chan bool)
	go func() {
		defer close(done)
		timeout := time.After(30 * time.Second)
		for {
			select {
			case rawEvent := <-eventsCh:
				atomic.AddInt64(&totalEvents, 1)

				// Try to decode the event
				if len(rawEvent.Data) >= int(unsafe.Sizeof(KernelEvent{})) {
					var kernelEvent KernelEvent
					err := binary.Read(
						strings.NewReader(string(rawEvent.Data)),
						binary.LittleEndian,
						&kernelEvent,
					)
					if err == nil {
						// Check if this looks like a network event
						if kernelEvent.EventType == EventTypeNetwork {
							networkEvents = append(networkEvents, kernelEvent)
						}
					}
				}

				// Stop after collecting some events
				if len(networkEvents) >= 3 || atomic.LoadInt64(&totalEvents) >= 50 {
					return
				}
			case <-timeout:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Generate real network activity
	generateRealNetworkActivity(t)

	<-done

	totalCount := atomic.LoadInt64(&totalEvents)
	t.Logf("Captured %d total events, %d network events", totalCount, len(networkEvents))

	// We should have captured some events
	assert.Greater(t, totalCount, int64(0), "Should capture some kernel events")
}

// TestProcessEventCapture tests process event capture
func TestProcessEventCapture(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Process event capture test requires root privileges")
	}

	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "process-capture-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer func() {
		collector.Stop()
	}()

	// Monitor for process events
	eventsCh := collector.Events()
	var processEvents []KernelEvent
	var totalEvents int64

	done := make(chan bool)
	go func() {
		defer close(done)
		timeout := time.After(20 * time.Second)
		for {
			select {
			case rawEvent := <-eventsCh:
				atomic.AddInt64(&totalEvents, 1)

				// Try to decode the event
				if len(rawEvent.Data) >= int(unsafe.Sizeof(KernelEvent{})) {
					var kernelEvent KernelEvent
					err := binary.Read(
						strings.NewReader(string(rawEvent.Data)),
						binary.LittleEndian,
						&kernelEvent,
					)
					if err == nil {
						// Check if this looks like a process event
						if kernelEvent.EventType == EventTypeProcess && kernelEvent.PID > 0 {
							processEvents = append(processEvents, kernelEvent)
						}
					}
				}

				// Stop after collecting some process events
				if len(processEvents) >= 3 || atomic.LoadInt64(&totalEvents) >= 100 {
					return
				}
			case <-timeout:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Generate real process activity
	generateRealProcessActivity(t)

	<-done

	totalCount := atomic.LoadInt64(&totalEvents)
	t.Logf("Captured %d total events, %d process events", totalCount, len(processEvents))

	// Verify process events
	for i, event := range processEvents {
		if i >= 5 { // Check first 5
			break
		}
		assert.Greater(t, event.PID, uint32(0), "Process event should have valid PID")
		assert.NotZero(t, event.Timestamp, "Process event should have timestamp")

		// Convert comm to string
		commBytes := event.Comm[:]
		// Find null terminator
		nullIdx := len(commBytes)
		for i, b := range commBytes {
			if b == 0 {
				nullIdx = i
				break
			}
		}
		comm := string(commBytes[:nullIdx])
		t.Logf("Process event: PID=%d, Comm=%s", event.PID, comm)
	}
}

// TestFileEventCapture tests file operation event capture
func TestFileEventCapture(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("File event capture test requires root privileges")
	}

	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "file-capture-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer func() {
		collector.Stop()
	}()

	// Monitor for file events
	eventsCh := collector.Events()
	var fileEvents []KernelEvent
	var totalEvents int64

	done := make(chan bool)
	go func() {
		defer close(done)
		timeout := time.After(15 * time.Second)
		for {
			select {
			case rawEvent := <-eventsCh:
				atomic.AddInt64(&totalEvents, 1)

				// Try to decode the event
				if len(rawEvent.Data) >= int(unsafe.Sizeof(KernelEvent{})) {
					var kernelEvent KernelEvent
					err := binary.Read(
						strings.NewReader(string(rawEvent.Data)),
						binary.LittleEndian,
						&kernelEvent,
					)
					if err == nil && kernelEvent.EventType == EventTypeFile {
						fileEvents = append(fileEvents, kernelEvent)
					}
				}

				// Stop after collecting some file events
				if len(fileEvents) >= 5 || atomic.LoadInt64(&totalEvents) >= 200 {
					return
				}
			case <-timeout:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Generate file I/O activity
	generateRealFileActivity(t)

	<-done

	totalCount := atomic.LoadInt64(&totalEvents)
	t.Logf("Captured %d total events, %d file events", totalCount, len(fileEvents))

	// Should have captured some events
	assert.Greater(t, totalCount, int64(0), "Should capture some kernel events")
}

// TestContainerInfoExtraction tests container information extraction from real cgroups
func TestContainerInfoExtraction(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "container-info-test"

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Test with real cgroup paths if available
	testPaths := []string{
		"/proc/self/cgroup",
		"/proc/1/cgroup",
	}

	for _, path := range testPaths {
		if _, err := os.Stat(path); err != nil {
			continue
		}

		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if strings.Contains(line, "::") {
				parts := strings.SplitN(line, "::", 2)
				if len(parts) == 2 {
					cgroupPath := parts[1]
					t.Logf("Testing cgroup path: %s", cgroupPath)

					// Test container ID extraction
					containerID := collector.extractContainerID(cgroupPath)
					if containerID != "" {
						t.Logf("Extracted container ID: %s", containerID)
						assert.Len(t, containerID, 64, "Container ID should be 64 characters")
					}

					// Test pod UID extraction
					podUID := collector.extractPodUID(cgroupPath)
					if podUID != "" {
						t.Logf("Extracted pod UID: %s", podUID)
						assert.True(t, len(podUID) >= 36, "Pod UID should be at least 36 characters")
					}
				}
			}
		}
	}
}

// TestCORECompatibility tests CO-RE compatibility features
func TestCORECompatibility(t *testing.T) {
	compatibility, err := NewCoreCompatibility()
	require.NoError(t, err)
	require.NotNil(t, compatibility)

	// Test kernel version detection
	version := compatibility.GetKernelVersion()
	t.Logf("Detected kernel version: %s", version.String())
	assert.Greater(t, version.Major, 0, "Should detect valid kernel major version")

	// Test feature detection
	features := compatibility.GetFeatures()
	t.Logf("Kernel features: BTF=%v, RingBuffer=%v, BPF_LSM=%v, CgroupV2=%v",
		features.HasBTF, features.HasRingBuffer, features.HasBPFLSM, features.HasCgroupV2)

	// Test compatibility checks
	ringBufferSupported := compatibility.IsCompatible("ring_buffer")
	t.Logf("Ring buffer supported: %v", ringBufferSupported)

	btfSupported := compatibility.IsCompatible("btf")
	t.Logf("BTF supported: %v", btfSupported)

	// Test program compatibility
	compatiblePrograms := compatibility.GetCompatiblePrograms()
	t.Logf("Compatible eBPF programs: %v", compatiblePrograms)
	assert.NotEmpty(t, compatiblePrograms, "Should have some compatible programs")

	// Test fallback strategies
	if !ringBufferSupported {
		fallback := compatibility.GetFallbackStrategy("ring_buffer")
		assert.Equal(t, "use_perf_buffer", fallback)
	}
}

// TestConcurrentCollectors tests multiple collectors running simultaneously
func TestConcurrentCollectors(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Concurrent collectors test requires root privileges")
	}

	logger := zap.NewNop()
	const numCollectors = 3

	collectors := make([]*Collector, numCollectors)
	contexts := make([]context.Context, numCollectors)
	cancels := make([]context.CancelFunc, numCollectors)

	// Create collectors
	for i := 0; i < numCollectors; i++ {
		config := DefaultConfig()
		config.Name = "concurrent-test-" + string(rune('A'+i))

		collector, err := NewCollectorWithConfig(config, logger)
		require.NoError(t, err, "Collector %d should be created", i)

		collectors[i] = collector
		contexts[i], cancels[i] = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancels[i]()
	}

	// Start all collectors
	for i, collector := range collectors {
		err := collector.Start(contexts[i])
		require.NoError(t, err, "Collector %d should start", i)
	}

	// Let them run and collect events
	time.Sleep(5 * time.Second)

	// Generate activity
	generateRealProcessActivity(t)

	// Check health of all collectors
	for i, collector := range collectors {
		assert.True(t, collector.IsHealthy(), "Collector %d should be healthy", i)
	}

	// Stop all collectors
	for i, collector := range collectors {
		err := collector.Stop()
		require.NoError(t, err, "Collector %d should stop", i)
	}

	t.Log("Successfully ran multiple concurrent collectors")
}

// TestMemoryUsage tests memory usage of the collector
func TestMemoryUsage(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Memory usage test requires root privileges")
	}

	logger := zap.NewNop()
	config := DefaultConfig()
	config.Name = "memory-usage-test"
	config.ResourceLimits.EventQueueSize = 5000 // Moderate buffer size

	collector, err := NewCollectorWithConfig(config, logger)
	require.NoError(t, err)

	// Measure initial memory
	var m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)
	initialAlloc := m1.Alloc

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Let it run and collect events
	time.Sleep(10 * time.Second)

	// Generate continuous activity
	go func() {
		for i := 0; i < 1000; i++ {
			generateRealFileActivity(t)
			time.Sleep(10 * time.Millisecond)
			if ctx.Err() != nil {
				break
			}
		}
	}()

	// Consume events to prevent buffer buildup
	eventsCh := collector.Events()
	go func() {
		for {
			select {
			case <-eventsCh:
				// Just consume events
			case <-ctx.Done():
				return
			}
		}
	}()

	// Let it run with activity
	time.Sleep(20 * time.Second)

	// Measure memory after running
	var m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m2)
	runningAlloc := m2.Alloc

	collector.Stop()

	// Measure memory after stopping
	runtime.GC()
	var m3 runtime.MemStats
	runtime.ReadMemStats(&m3)
	finalAlloc := m3.Alloc

	t.Logf("Memory usage - Initial: %d KB, Running: %d KB, Final: %d KB",
		initialAlloc/1024, runningAlloc/1024, finalAlloc/1024)

	// Memory should be reasonable (less than 50MB increase during run)
	memoryIncrease := runningAlloc - initialAlloc
	assert.Less(t, memoryIncrease, uint64(50*1024*1024),
		"Memory increase should be less than 50MB during run")

	// Memory should be mostly cleaned up after stop (allow some overhead)
	memoryAfterStop := finalAlloc - initialAlloc
	assert.Less(t, memoryAfterStop, uint64(20*1024*1024),
		"Memory should be mostly cleaned up after stop")
}

// Helper functions for generating real system activity

func generateRealNetworkActivity(t *testing.T) {
	// Create real TCP connections
	go func() {
		for i := 0; i < 5; i++ {
			conn, err := net.DialTimeout("tcp", "localhost:22", time.Second)
			if err == nil {
				conn.Close()
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()

	// Create UDP activity
	go func() {
		for i := 0; i < 5; i++ {
			conn, err := net.Dial("udp", "8.8.8.8:53")
			if err == nil {
				conn.Write([]byte("test"))
				conn.Close()
			}
			time.Sleep(300 * time.Millisecond)
		}
	}()

	time.Sleep(3 * time.Second)
}

func generateRealProcessActivity(t *testing.T) {
	// Execute real commands that will trigger execve events
	commands := [][]string{
		{"/bin/echo", "integration-test"},
		{"/bin/date"},
		{"/usr/bin/whoami"},
		{"/bin/sleep", "0.1"},
	}

	for _, cmd := range commands {
		if _, err := os.Stat(cmd[0]); err == nil {
			go func(command []string) {
				for i := 0; i < 3; i++ {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					exec.CommandContext(ctx, command[0], command[1:]...).Run()
					cancel()
					time.Sleep(200 * time.Millisecond)
				}
			}(cmd)
		}
	}

	time.Sleep(3 * time.Second)
}

func generateRealFileActivity(t *testing.T) {
	tempDir := "/tmp/kernel_integration_test"
	os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	// Create and manipulate files
	for i := 0; i < 10; i++ {
		filename := filepath.Join(tempDir, "test_"+string(rune('a'+i)))

		// Create file (triggers openat)
		file, err := os.Create(filename)
		if err == nil {
			// Write data
			file.WriteString("integration test data " + string(rune('0'+i)))
			file.Sync()
			file.Close()

			// Read file back
			data, _ := os.ReadFile(filename)
			_ = data

			// Remove file
			os.Remove(filename)
		}

		time.Sleep(100 * time.Millisecond)
	}

	// Additional syscall activity
	for i := 0; i < 5; i++ {
		// stat() calls
		os.Stat("/proc/self/status")
		os.Stat("/proc/meminfo")

		// access() calls
		syscall.Access("/tmp", syscall.F_OK)

		time.Sleep(50 * time.Millisecond)
	}
}
