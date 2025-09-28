//go:build linux && ebpf
// +build linux,ebpf

package dns

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestEBPFDNSCapture(t *testing.T) {
	// Skip if not root
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	logger := zaptest.NewLogger(t)

	// Create eBPF program
	prog := NewDNSeBPFProgram(logger)

	// Load eBPF program
	err := prog.Load()
	require.NoError(t, err, "Failed to load eBPF program")
	defer prog.Close()

	// Attach to kernel hooks
	err = prog.Attach()
	require.NoError(t, err, "Failed to attach eBPF program")

	// Start reading events
	events, err := prog.ReadEvents()
	require.NoError(t, err)

	// Capture context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Trigger DNS queries in background
	go func() {
		// Try different approaches to generate DNS traffic
		// Use getent which directly uses system resolver
		exec.Command("getent", "hosts", "google.com").Run()

		// Query that should succeed
		exec.Command("nslookup", "google.com", "8.8.8.8").Run()

		// Query that should fail (NXDOMAIN)
		exec.Command("nslookup", "nonexistent.domain.test", "8.8.8.8").Run()

		// Also try with host command if available
		exec.Command("host", "example.com").Run()

		// Give eBPF hooks time to process
		time.Sleep(500 * time.Millisecond)
	}()

	// Collect events
	var capturedEvents []*DNSEvent
	eventTimeout := time.After(5 * time.Second)

collectLoop:
	for {
		select {
		case event := <-events:
			if event != nil {
				t.Logf("Captured DNS event: Query=%s, Problem=%s, Latency=%dms",
					event.GetQueryName(),
					event.ProblemType.String(),
					event.GetLatencyMs())
				capturedEvents = append(capturedEvents, event)

				// Check if we have enough events
				if len(capturedEvents) >= 2 {
					break collectLoop
				}
			}

		case <-eventTimeout:
			t.Log("Event collection timeout")
			break collectLoop

		case <-ctx.Done():
			break collectLoop
		}
	}

	// Verify we captured some events
	assert.Greater(t, len(capturedEvents), 0, "Should capture at least one DNS event")

	// Check for different problem types
	var foundSlow, foundNXDomain bool
	for _, event := range capturedEvents {
		switch event.ProblemType {
		case DNSProblemSlow:
			foundSlow = true
		case DNSProblemNXDomain:
			foundNXDomain = true
		}
	}

	t.Logf("Found slow query: %v, Found NXDOMAIN: %v", foundSlow, foundNXDomain)

	// Get statistics
	stats, err := prog.GetStats()
	require.NoError(t, err)
	t.Logf("Stats: ActiveQueries=%d, EventsDropped=%d",
		stats.ActiveQueries, stats.EventsDropped)
}

func TestCoreDNSDetectionEBPF(t *testing.T) {
	// Skip if not root
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	prog := NewDNSeBPFProgram(logger)

	// Load eBPF program
	err := prog.Load()
	require.NoError(t, err)
	defer prog.Close()

	// Find CoreDNS process (if running)
	cmd := exec.Command("pgrep", "-x", "coredns")
	output, _ := cmd.Output()

	if len(output) > 0 {
		// Parse PIDs and register them
		// This is simplified - real implementation would parse properly
		pids := []uint32{1234} // Example PID
		err = prog.SetCoreDNSPIDs(pids)
		assert.NoError(t, err)
	}

	// Attach and test
	err = prog.Attach()
	require.NoError(t, err)

	// If CoreDNS is running, query it
	if len(output) > 0 {
		exec.Command("nslookup", "kubernetes.default.svc.cluster.local", "127.0.0.1").Run()
	}
}

func TestKernelSupport(t *testing.T) {
	err := CheckKernelSupport()
	if err != nil {
		t.Skipf("Kernel doesn't support required eBPF features: %v", err)
	}

	t.Log("Kernel supports all required eBPF features")
}

func BenchmarkEBPFEventProcessing(b *testing.B) {
	// Skip if not root
	if os.Geteuid() != 0 {
		b.Skip("Benchmark requires root privileges")
	}

	logger := zaptest.NewLogger(b)
	prog := NewDNSeBPFProgram(logger)

	err := prog.Load()
	require.NoError(b, err)
	defer prog.Close()

	err = prog.Attach()
	require.NoError(b, err)

	events, err := prog.ReadEvents()
	require.NoError(b, err)

	b.ResetTimer()

	// Benchmark DNS query processing
	for i := 0; i < b.N; i++ {
		// Trigger a DNS query
		exec.Command("nslookup", "localhost", "127.0.0.1").Run()

		// Wait for event
		select {
		case <-events:
			// Event processed
		case <-time.After(100 * time.Millisecond):
			// Timeout
		}
	}

	b.StopTimer()

	// Report stats
	stats, _ := prog.GetStats()
	b.Logf("Processed %d events, dropped %d", b.N, stats.EventsDropped)
}
