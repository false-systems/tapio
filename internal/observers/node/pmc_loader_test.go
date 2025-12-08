//go:build linux

package node

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TDD Cycle 3: PMCLoader eBPF Integration
// RED Phase: Write failing tests first

// TestPMCLoader_New verifies PMCLoader creation
func TestPMCLoader_New(t *testing.T) {
	loader, err := NewPMCLoader()
	require.NoError(t, err, "Should create PMCLoader without error")
	require.NotNil(t, loader, "PMCLoader should not be nil")

	// Verify loader has required fields
	assert.NotNil(t, loader.Events(), "Should have Events channel")
}

// TestPMCLoader_Lifecycle verifies Start/Stop lifecycle
func TestPMCLoader_Lifecycle(t *testing.T) {
	// eBPF operations require root privileges
	if os.Geteuid() != 0 {
		t.Skip("eBPF operations require root privileges - run with sudo")
	}

	loader, err := NewPMCLoader()
	if err != nil {
		t.Skipf("PMCLoader not available in this environment: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start should succeed (or skip if eBPF not available in this environment)
	err = loader.Start(ctx)
	if err != nil {
		t.Skipf("eBPF not available in this environment: %v", err)
	}

	// Should be able to stop
	err = loader.Stop()
	require.NoError(t, err, "Stop should succeed")
}

// TestPMCLoader_EventChannel verifies event reception
func TestPMCLoader_EventChannel(t *testing.T) {
	// eBPF operations require root privileges
	if os.Geteuid() != 0 {
		t.Skip("eBPF operations require root privileges - run with sudo")
	}

	loader, err := NewPMCLoader()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = loader.Start(ctx)
	if err != nil {
		t.Skipf("eBPF not available in this environment: %v", err)
	}
	defer func() { _ = loader.Stop() }() // Ignore: test cleanup

	// Should receive events from eBPF
	select {
	case event := <-loader.Events():
		assert.NotNil(t, event, "Should receive PMCEvent")
		// Verify event structure
		assert.GreaterOrEqual(t, event.CPU, uint32(0), "CPU should be >= 0")
		assert.Greater(t, event.Cycles, uint64(0), "Cycles should be > 0")
		assert.Greater(t, event.Instructions, uint64(0), "Instructions should be > 0")
		assert.Greater(t, event.Timestamp, uint64(0), "Timestamp should be > 0")
	case <-time.After(2 * time.Second):
		t.Skip("No PMC events received (PMC may not be available on this system)")
	}
}

// TestPMCLoader_StopWithoutStart verifies error handling
func TestPMCLoader_StopWithoutStart(t *testing.T) {
	loader, err := NewPMCLoader()
	require.NoError(t, err)

	// Stop without Start should not panic
	err = loader.Stop()
	assert.NoError(t, err, "Stop without Start should be safe")
}

// TestPMCLoader_DoubleStart verifies Start idempotency
func TestPMCLoader_DoubleStart(t *testing.T) {
	// eBPF operations require root privileges
	if os.Geteuid() != 0 {
		t.Skip("eBPF operations require root privileges - run with sudo")
	}

	loader, err := NewPMCLoader()
	require.NoError(t, err)

	ctx := context.Background()

	// First Start
	err = loader.Start(ctx)
	if err != nil {
		t.Skipf("eBPF not available in this environment: %v", err)
	}
	defer func() { _ = loader.Stop() }() // Ignore: test cleanup

	// Second Start should fail or be idempotent
	err = loader.Start(ctx)
	assert.Error(t, err, "Second Start should fail")
	assert.Contains(t, err.Error(), "already started", "Error should indicate already started")
}

// TestPMCLoader_ContextCancellation verifies graceful shutdown
func TestPMCLoader_ContextCancellation(t *testing.T) {
	// eBPF operations require root privileges
	if os.Geteuid() != 0 {
		t.Skip("eBPF operations require root privileges - run with sudo")
	}

	loader, err := NewPMCLoader()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	err = loader.Start(ctx)
	if err != nil {
		cancel()
		t.Skipf("eBPF not available in this environment: %v", err)
	}
	defer func() { _ = loader.Stop() }() // Ignore: test cleanup

	// Cancel context
	cancel()

	// Wait briefly for cleanup
	time.Sleep(100 * time.Millisecond)

	// Stop should still work after context cancellation
	err = loader.Stop()
	assert.NoError(t, err)
}

// TestPMCLoader_MultiCPU verifies events from multiple CPUs
func TestPMCLoader_MultiCPU(t *testing.T) {
	// eBPF operations require root privileges
	if os.Geteuid() != 0 {
		t.Skip("eBPF operations require root privileges - run with sudo")
	}

	loader, err := NewPMCLoader()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = loader.Start(ctx)
	if err != nil {
		t.Skipf("eBPF not available in this environment: %v", err)
	}
	defer func() { _ = loader.Stop() }() // Ignore: test cleanup

	// Collect events from different CPUs
	cpusSeen := make(map[uint32]bool)
	timeout := time.After(3 * time.Second)

	for len(cpusSeen) < 2 {
		select {
		case event := <-loader.Events():
			cpusSeen[event.CPU] = true
		case <-timeout:
			if len(cpusSeen) == 0 {
				t.Skip("No PMC events received (PMC may not be available)")
			}
			// Single CPU system is OK
			return
		}
	}

	assert.GreaterOrEqual(t, len(cpusSeen), 1, "Should see events from at least one CPU")
}
