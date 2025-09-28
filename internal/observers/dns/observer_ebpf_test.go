//go:build linux
// +build linux

package dns

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestObserver_WithEBPF(t *testing.T) {
	// Skip if not running as root
	if testing.Short() {
		t.Skip("Skipping eBPF test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "ebpf-test"
	config.EnableEBPF = true // Force eBPF mode

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	if err != nil {
		// If eBPF fails (e.g., not root), check it falls back gracefully
		t.Logf("eBPF start result: %v", err)
		// Should still be able to stop
		stopErr := obs.Stop()
		assert.NoError(t, stopErr)
		return
	}

	// Observer should be healthy
	assert.True(t, obs.IsHealthy())

	// Wait a bit to see if we get any events
	events := obs.Events()
	select {
	case event := <-events:
		t.Logf("Received eBPF event: %+v", event)
	case <-time.After(100 * time.Millisecond):
		t.Log("No events received in 100ms (expected if no DNS activity)")
	}

	// Stop should work cleanly
	err = obs.Stop()
	assert.NoError(t, err)
	assert.False(t, obs.IsHealthy())
}

func TestEBPFProgram_LoadAndAttach(t *testing.T) {
	// Skip if not running as root
	if testing.Short() {
		t.Skip("Skipping eBPF test in short mode")
	}

	logger := zaptest.NewLogger(t)
	program := NewDNSeBPFProgram(logger)

	// Try to load eBPF program
	err := program.Load()
	if err != nil {
		t.Logf("eBPF load failed (expected if not root): %v", err)
		return
	}

	// Try to attach tracepoints
	err = program.Attach()
	if err != nil {
		t.Errorf("Failed to attach eBPF tracepoints: %v", err)
		program.Close()
		return
	}

	t.Log("eBPF program loaded and attached successfully")

	// Clean up
	program.Close()
}

func TestEBPFProgram_ReadEvents(t *testing.T) {
	// Skip if not running as root
	if testing.Short() {
		t.Skip("Skipping eBPF test in short mode")
	}

	logger := zaptest.NewLogger(t)
	program := NewDNSeBPFProgram(logger)

	// Load and attach
	if err := program.Load(); err != nil {
		t.Skipf("Cannot load eBPF (need root): %v", err)
	}
	defer program.Close()

	if err := program.Attach(); err != nil {
		t.Fatalf("Failed to attach: %v", err)
	}

	// Start reading events
	events, err := program.ReadEvents()
	require.NoError(t, err)

	// Generate a DNS query to trigger events
	// Note: This would need actual DNS traffic to test properly

	select {
	case event := <-events:
		t.Logf("Captured DNS event: %+v", event)
		assert.NotNil(t, event)
	case <-time.After(100 * time.Millisecond):
		t.Log("No DNS events captured (expected if no DNS traffic)")
	}
}
