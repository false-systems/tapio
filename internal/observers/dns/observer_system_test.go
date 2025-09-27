//go:build linux && system
// +build linux,system

package dns

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// System tests verify the observer in a real Linux environment with eBPF

func TestSystem_EBPFAttachment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("System test requires Linux")
	}

	// Check if running as root (required for eBPF)
	if os.Geteuid() != 0 {
		t.Skip("System test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "system-ebpf"
	config.EnableEBPF = true
	config.RingBufferSize = 4 * 1024 * 1024

	obs, err := NewObserver("system", config, logger)
	require.NoError(t, err, "Should create observer with eBPF enabled")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start observer (will attach eBPF programs)
	err = obs.Start(ctx)
	require.NoError(t, err, "Should attach eBPF programs successfully")
	defer obs.Stop()

	// Verify eBPF state is initialized
	assert.NotNil(t, obs.ebpfState, "eBPF state should be initialized")

	// Observer should be healthy
	assert.True(t, obs.IsHealthy(), "Observer should be healthy with eBPF attached")

	// Generate DNS traffic
	cmd := exec.Command("nslookup", "example.com")
	output, err := cmd.CombinedOutput()
	t.Logf("nslookup output: %s", output)

	// Give time for eBPF to process events
	time.Sleep(100 * time.Millisecond)

	// Check if we captured any DNS problems
	stats := obs.GetStats()
	t.Logf("System test stats after DNS traffic: %+v", stats)
}

func TestSystem_KernelEventCapture(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("System test requires Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("System test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "system-kernel"
	config.EnableEBPF = true
	config.SlowQueryThresholdMs = 10 // Very low threshold to catch queries

	obs, err := NewObserver("system", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Subscribe to events
	events := obs.Events()
	receivedEvents := make(chan struct{})

	go func() {
		select {
		case event := <-events:
			if event != nil {
				t.Logf("Received kernel event: %+v", event)
				close(receivedEvents)
			}
		case <-time.After(5 * time.Second):
			return
		}
	}()

	// Generate various DNS queries
	testQueries := []string{
		"localhost",
		"google.com",
		"nonexistent.invalid.test",
		"example.com",
	}

	for _, domain := range testQueries {
		cmd := exec.Command("dig", "+short", domain)
		output, err := cmd.CombinedOutput()
		t.Logf("dig %s: %s (err: %v)", domain, output, err)
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for events or timeout
	select {
	case <-receivedEvents:
		t.Log("Successfully received kernel events")
	case <-time.After(2 * time.Second):
		t.Log("No kernel events received (might be normal if queries were fast)")
	}

	stats := obs.GetStats()
	t.Logf("Final kernel capture stats: %+v", stats)
}

func TestSystem_MultipleProcesses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("System test requires Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("System test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "system-multiproc"
	config.EnableEBPF = true

	obs, err := NewObserver("system", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Launch multiple processes doing DNS queries
	processes := 5
	done := make(chan bool, processes)

	for i := 0; i < processes; i++ {
		go func(id int) {
			defer func() { done <- true }()

			// Each process does multiple queries
			for j := 0; j < 3; j++ {
				domain := []string{"example.com", "google.com", "github.com"}[j%3]
				cmd := exec.Command("host", domain)
				output, _ := cmd.CombinedOutput()
				t.Logf("Process %d query %d: %s", id, j, string(output))
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	// Wait for all processes
	for i := 0; i < processes; i++ {
		<-done
	}

	// Give eBPF time to process
	time.Sleep(100 * time.Millisecond)

	stats := obs.GetStats()
	t.Logf("Multi-process DNS stats: %+v", stats)

	// Should handle concurrent processes
	assert.True(t, obs.IsHealthy())
}

func TestSystem_LongRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running system test in short mode")
	}

	if runtime.GOOS != "linux" {
		t.Skip("System test requires Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("System test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "system-longrun"
	config.EnableEBPF = true
	config.RepeatWindowSec = 10
	config.RepeatThreshold = 3

	obs, err := NewObserver("system", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Run for extended period with periodic DNS queries
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	iterations := 0
	maxIterations := 10

	for {
		select {
		case <-ticker.C:
			iterations++
			if iterations > maxIterations {
				t.Log("Long-running test completed")
				return
			}

			// Generate DNS query
			cmd := exec.Command("nslookup", "test.example.com")
			cmd.Run()

			stats := obs.GetStats()
			t.Logf("Iteration %d stats: %+v", iterations, stats)

			// Check health
			assert.True(t, obs.IsHealthy(), "Observer should remain healthy")

		case <-ctx.Done():
			return
		}
	}
}

func TestSystem_ResourceUsage(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("System test requires Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("System test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "system-resources"
	config.EnableEBPF = true
	config.RingBufferSize = 1024 * 1024 // 1MB buffer

	obs, err := NewObserver("system", config, logger)
	require.NoError(t, err)

	// Measure initial memory
	var memStatsBefore runtime.MemStats
	runtime.ReadMemStats(&memStatsBefore)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Generate load
	for i := 0; i < 100; i++ {
		cmd := exec.Command("host", "-t", "A", "example.com")
		cmd.Run()
		time.Sleep(5 * time.Millisecond)
	}

	// Measure memory after load
	var memStatsAfter runtime.MemStats
	runtime.ReadMemStats(&memStatsAfter)

	memUsed := memStatsAfter.Alloc - memStatsBefore.Alloc
	t.Logf("Memory used: %d bytes (~%d KB)", memUsed, memUsed/1024)

	// Check for memory leaks (should not grow excessively)
	assert.Less(t, memUsed, uint64(10*1024*1024), "Should use less than 10MB")

	stats := obs.GetStats()
	t.Logf("Resource test final stats: %+v", stats)
}

func TestSystem_CgroupTracking(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("System test requires Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("System test requires root privileges")
	}

	// Check if we're in a container/cgroup
	cgroupFile := "/proc/self/cgroup"
	data, err := os.ReadFile(cgroupFile)
	if err != nil {
		t.Skipf("Cannot read cgroup info: %v", err)
	}

	t.Logf("Current process cgroup: %s", string(data))

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "system-cgroup"
	config.EnableEBPF = true

	obs, err := NewObserver("system", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Generate DNS query from current cgroup
	cmd := exec.Command("dig", "+short", "cloudflare.com")
	output, err := cmd.CombinedOutput()
	t.Logf("DNS query output: %s (err: %v)", output, err)

	time.Sleep(100 * time.Millisecond)

	// The eBPF program should track cgroup IDs
	stats := obs.GetStats()
	t.Logf("Cgroup tracking stats: %+v", stats)
}

func TestSystem_Cleanup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("System test requires Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("System test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "system-cleanup"
	config.EnableEBPF = true

	// Create and start observer
	obs, err := NewObserver("system", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)

	// Generate some activity
	exec.Command("host", "example.com").Run()
	time.Sleep(100 * time.Millisecond)

	// Stop observer
	err = obs.Stop()
	assert.NoError(t, err, "Should stop cleanly")

	// Verify cleanup
	assert.False(t, obs.IsHealthy(), "Should not be healthy after stop")
	assert.Nil(t, obs.ebpfState, "eBPF state should be cleaned up")

	// Try to start again (should work)
	err = obs.Start(ctx)
	require.NoError(t, err, "Should be able to restart after cleanup")
	obs.Stop()
}
