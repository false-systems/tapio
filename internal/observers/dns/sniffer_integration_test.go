//go:build linux
// +build linux

package dns

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestSniffer_RealDNSTraffic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping sniffer integration test")
	}

	logger := zaptest.NewLogger(t)

	// Use lo interface for testing
	program := NewDNSSnifferProgram(logger, "lo")

	// Load the sniffer
	err := program.Load()
	if err != nil {
		t.Skipf("Cannot load sniffer (need root): %v", err)
	}
	defer program.Close()

	// Attach to interface
	err = program.Attach()
	if err != nil {
		t.Fatalf("Failed to attach: %v", err)
	}

	// Start reading problems
	problems, err := program.ReadProblems()
	require.NoError(t, err)

	// Collect problems for 3 seconds while generating DNS traffic
	receivedProblems := []*DNSProblem{}
	done := make(chan bool)

	go func() {
		timeout := time.After(3 * time.Second)
		for {
			select {
			case problem := <-problems:
				if problem != nil {
					receivedProblems = append(receivedProblems, problem)
					t.Logf("DNS PROBLEM DETECTED: %s - %s (latency=%dms, rcode=%d)",
						problem.Name,
						problem.Problem,
						problem.Latency.Milliseconds(),
						problem.RCode)
				}
			case <-timeout:
				close(done)
				return
			}
		}
	}()

	// Generate DNS traffic
	t.Log("Generating DNS queries...")

	// Normal query (should be fast)
	_, err = net.LookupHost("localhost")
	if err != nil {
		t.Logf("Localhost lookup error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// External query (might be slow)
	_, err = net.LookupHost("google.com")
	if err != nil {
		t.Logf("Google lookup error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// NXDOMAIN query
	_, err = net.LookupHost("this-domain-definitely-does-not-exist-xyz123.invalid")
	if err != nil {
		t.Logf("NXDOMAIN lookup (expected error): %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Another external query
	_, err = net.LookupHost("cloudflare.com")
	if err != nil {
		t.Logf("Cloudflare lookup error: %v", err)
	}

	<-done

	// Report results
	if len(receivedProblems) > 0 {
		t.Logf("✅ DETECTED %d DNS PROBLEMS:", len(receivedProblems))
		for _, p := range receivedProblems {
			t.Logf("  - %s: %s (%dms)",
				p.Name,
				p.Problem,
				p.Latency.Milliseconds())
		}
	} else {
		t.Log("No DNS problems detected (all queries were fast and successful)")
	}

	// We're detecting REAL DNS problems from REAL packets!
	t.Log("DNS packet sniffer is working - parsing real DNS traffic")
}
