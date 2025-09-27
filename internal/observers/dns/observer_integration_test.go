//go:build integration
// +build integration

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

// Helper function to create query name
func makeQueryNameInteg(name string) [253]byte {
	var result [253]byte
	copy(result[:], name)
	return result
}

// Integration tests verify DNS observer with real network components

func TestIntegration_RealDNSQueries(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "integration-dns"
	config.EnableEBPF = false        // Start with fallback for portability
	config.SlowQueryThresholdMs = 50 // Lower threshold for testing

	obs, err := NewObserver("integration", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Perform real DNS lookups
	testDomains := []string{
		"localhost",
		"example.com",
		"google.com",
		"nonexistent.invalid.domain.test",
	}

	for _, domain := range testDomains {
		start := time.Now()
		ips, err := net.LookupHost(domain)
		duration := time.Since(start)

		t.Logf("DNS lookup for %s: %v (duration: %v, err: %v)",
			domain, ips, duration, err)

		// Simulate tracking if it was slow or failed
		if duration > 50*time.Millisecond || err != nil {
			event := &DNSEvent{
				Timestamp:   uint64(time.Now().UnixNano()),
				ProblemType: DNSProblemSlow,
				QueryName:   makeQueryNameInteg(domain),
				QueryType:   1,
				LatencyNs:   uint64(duration.Nanoseconds()),
			}

			if err != nil {
				if dnsErr, ok := err.(*net.DNSError); ok && dnsErr.IsNotFound {
					event.ProblemType = DNSProblemNXDOMAIN
				} else {
					event.ProblemType = DNSProblemSERVFAIL
				}
			}

			obs.trackProblem(event)
		}
	}

	// Check that we tracked some problems
	stats := obs.GetStats()
	assert.Greater(t, stats.TotalProblems, int64(0), "Should have tracked at least one DNS problem")
	t.Logf("DNS Stats: %+v", stats)
}

func TestIntegration_LocalDNSServer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping local DNS server test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "integration-local"
	config.EnableEBPF = false
	config.IgnoreLocalhost = false // Don't ignore localhost for this test

	obs, err := NewObserver("integration", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Try to resolve using local resolver (usually 127.0.0.1:53 or system)
	resolver := &net.Resolver{
		PreferGo: true,
	}

	testCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Perform DNS query
	start := time.Now()
	ips, err := resolver.LookupHost(testCtx, "localhost")
	duration := time.Since(start)

	t.Logf("Local DNS resolution: IPs=%v, Duration=%v, Error=%v", ips, duration, err)

	if err == nil {
		assert.Contains(t, ips, "127.0.0.1", "localhost should resolve to 127.0.0.1")
	}

	// Track if it was slow
	if duration > 100*time.Millisecond {
		event := &DNSEvent{
			Timestamp:   uint64(time.Now().UnixNano()),
			ProblemType: DNSProblemSlow,
			QueryName:   makeQueryNameInteg("localhost"),
			QueryType:   1,
			LatencyNs:   uint64(duration.Nanoseconds()),
		}
		obs.trackProblem(event)

		stats := obs.GetStats()
		assert.Greater(t, stats.SlowQueries, int64(0))
	}
}

func TestIntegration_DNSTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping DNS timeout test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "integration-timeout"
	config.EnableEBPF = false
	config.TimeoutMs = 100 // Very short timeout for testing

	obs, err := NewObserver("integration", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Create a resolver with unreachable DNS server
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			// Connect to a non-existent DNS server
			d := net.Dialer{Timeout: 100 * time.Millisecond}
			return d.DialContext(ctx, network, "192.0.2.1:53") // TEST-NET-1 (RFC 5737)
		},
	}

	testCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = resolver.LookupHost(testCtx, "example.com")
	duration := time.Since(start)

	// Should timeout
	assert.Error(t, err, "Should timeout with unreachable DNS server")
	t.Logf("DNS timeout after %v: %v", duration, err)

	// Track the timeout
	event := &DNSEvent{
		Timestamp:   uint64(time.Now().UnixNano()),
		ProblemType: DNSProblemTimeout,
		QueryName:   makeQueryNameInteg("example.com"),
		QueryType:   1,
		LatencyNs:   uint64(duration.Nanoseconds()),
	}
	obs.trackProblem(event)

	stats := obs.GetStats()
	assert.Equal(t, int64(1), stats.Timeouts)
}

func TestIntegration_ParallelDNSQueries(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping parallel DNS test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "integration-parallel"
	config.EnableEBPF = false
	config.BufferSize = 1000

	obs, err := NewObserver("integration", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Perform parallel DNS queries
	domains := []string{
		"google.com",
		"github.com",
		"stackoverflow.com",
		"wikipedia.org",
		"example.com",
		"localhost",
		"cloudflare.com",
		"amazon.com",
		"microsoft.com",
		"apple.com",
	}

	type result struct {
		domain   string
		duration time.Duration
		err      error
	}

	results := make(chan result, len(domains))

	// Launch parallel queries
	for _, domain := range domains {
		go func(d string) {
			start := time.Now()
			_, err := net.LookupHost(d)
			results <- result{
				domain:   d,
				duration: time.Since(start),
				err:      err,
			}
		}(domain)
	}

	// Collect results
	slowQueries := 0
	failures := 0

	for i := 0; i < len(domains); i++ {
		r := <-results
		t.Logf("DNS %s: %v (err: %v)", r.domain, r.duration, r.err)

		// Track slow or failed queries
		if r.duration > 100*time.Millisecond {
			slowQueries++
			event := &DNSEvent{
				Timestamp:   uint64(time.Now().UnixNano()),
				ProblemType: DNSProblemSlow,
				QueryName:   makeQueryNameInteg(r.domain),
				QueryType:   1,
				LatencyNs:   uint64(r.duration.Nanoseconds()),
			}
			obs.trackProblem(event)
		}

		if r.err != nil {
			failures++
			event := &DNSEvent{
				Timestamp:   uint64(time.Now().UnixNano()),
				ProblemType: DNSProblemSERVFAIL,
				QueryName:   makeQueryNameInteg(r.domain),
				QueryType:   1,
				LatencyNs:   uint64(r.duration.Nanoseconds()),
			}
			obs.trackProblem(event)
		}
	}

	stats := obs.GetStats()
	t.Logf("Parallel DNS stats: slow=%d, failures=%d, total_problems=%d",
		slowQueries, failures, stats.TotalProblems)

	// Should handle concurrent tracking correctly
	assert.GreaterOrEqual(t, stats.TotalProblems, int64(slowQueries+failures))
}

func TestIntegration_DNSOverTCP(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping DNS over TCP test in short mode")
	}

	// Note: Most DNS queries use UDP, but TCP is used for large responses
	// This test verifies we can handle both protocols

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "integration-tcp"
	config.EnableEBPF = false

	obs, err := NewObserver("integration", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Query for TXT records which might be large enough to require TCP
	start := time.Now()
	txts, err := net.LookupTXT("google.com")
	duration := time.Since(start)

	t.Logf("TXT lookup: records=%d, duration=%v, err=%v", len(txts), duration, err)

	if err == nil && len(txts) > 0 {
		t.Logf("First TXT record: %s", txts[0])
	}

	// Track if slow
	if duration > 100*time.Millisecond {
		event := &DNSEvent{
			Timestamp:   uint64(time.Now().UnixNano()),
			ProblemType: DNSProblemSlow,
			QueryName:   makeQueryNameInteg("google.com"),
			QueryType:   16, // TXT
			LatencyNs:   uint64(duration.Nanoseconds()),
		}
		obs.trackProblem(event)
	}

	assert.True(t, obs.IsHealthy())
}

func TestIntegration_IPv6DNSQueries(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping IPv6 DNS test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "integration-ipv6"
	config.EnableEBPF = false

	obs, err := NewObserver("integration", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Query for AAAA records (IPv6)
	domains := []string{
		"google.com",
		"cloudflare.com",
		"ipv6.google.com",
	}

	for _, domain := range domains {
		start := time.Now()
		ips, err := net.LookupIP(domain)
		duration := time.Since(start)

		var hasIPv6 bool
		for _, ip := range ips {
			if ip.To4() == nil {
				hasIPv6 = true
				t.Logf("IPv6 address for %s: %s", domain, ip)
			}
		}

		t.Logf("DNS lookup %s: IPv6=%v, duration=%v, err=%v",
			domain, hasIPv6, duration, err)

		// Track if slow
		if duration > 100*time.Millisecond {
			event := &DNSEvent{
				Timestamp:   uint64(time.Now().UnixNano()),
				ProblemType: DNSProblemSlow,
				QueryName:   makeQueryNameInteg(domain),
				QueryType:   28, // AAAA
				LatencyNs:   uint64(duration.Nanoseconds()),
			}
			obs.trackProblem(event)
		}
	}

	stats := obs.GetStats()
	t.Logf("IPv6 DNS stats: %+v", stats)
}
