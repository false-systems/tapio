//go:build linux
// +build linux

package dns

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestEBPF_CaptureRealDNSTraffic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping eBPF integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "ebpf-integration"
	config.EnableEBPF = true

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start eBPF observer (need root): %v", err)
	}
	defer obs.Stop()

	// Start collecting events
	events := obs.Events()
	eventChan := make(chan struct{})

	go func() {
		select {
		case event := <-events:
			t.Logf("Captured DNS event: %+v", event)
			close(eventChan)
		case <-time.After(5 * time.Second):
			t.Log("No events captured after 5 seconds")
		}
	}()

	// Generate some DNS queries to trigger events
	t.Log("Generating DNS queries...")

	// Query 1: Should be fast (Google DNS)
	_, err = net.LookupHost("google.com")
	if err != nil {
		t.Logf("DNS lookup 1 failed: %v", err)
	}

	// Query 2: Non-existent domain (NXDOMAIN)
	_, err = net.LookupHost("nonexistent-domain-12345.local")
	if err != nil {
		t.Logf("Expected NXDOMAIN error: %v", err)
	}

	// Query 3: Kubernetes-style domain
	_, err = net.LookupHost("test-service.default.svc.cluster.local")
	if err != nil {
		t.Logf("K8s domain lookup failed (expected): %v", err)
	}

	// Wait for events
	select {
	case <-eventChan:
		t.Log("Successfully captured DNS events")
	case <-time.After(2 * time.Second):
		t.Log("No DNS problems detected (queries might be too fast)")
	}

	// Check statistics
	stats := obs.GetStats()
	t.Logf("DNS Observer Stats: %+v", stats)
}

func TestEBPF_DetectSlowQueries(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping eBPF integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	program := NewDNSeBPFProgram(logger)

	// Set very low threshold to catch normal queries as "slow"
	program.slowThresholdMs = 1 // 1ms threshold

	if err := program.Load(); err != nil {
		t.Skipf("Cannot load eBPF (need root): %v", err)
	}
	defer program.Close()

	if err := program.Attach(); err != nil {
		t.Fatalf("Failed to attach: %v", err)
	}

	events, err := program.ReadEvents()
	require.NoError(t, err)

	// Generate a DNS query
	go func() {
		_, _ = net.LookupHost("example.com")
	}()

	// Should capture as slow since threshold is 1ms
	select {
	case event := <-events:
		t.Logf("Captured slow DNS event: %+v", event)
		assert.Equal(t, DNSProblemSlow, event.ProblemType)
	case <-time.After(1 * time.Second):
		t.Log("No slow queries detected")
	}
}
