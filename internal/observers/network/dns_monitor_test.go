package network

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

// TestParseDNSQuery tests DNS query packet parsing
func TestParseDNSQuery(t *testing.T) {
	// Given: DNS query packet for "example.com" A record
	packet := buildDNSQueryPacket(t, "example.com", dnsmessage.TypeA, 12345)

	// When: Parse packet
	query, err := parseDNSQuery(packet)

	// Then: Extract domain name, type, and query ID
	require.NoError(t, err)
	assert.Equal(t, "example.com.", query.DomainName) // DNS names end with .
	assert.Equal(t, "A", query.QueryType)
	assert.Equal(t, uint16(12345), query.QueryID)
}

// TestParseDNSQuery_AAAA tests IPv6 DNS query parsing
func TestParseDNSQuery_AAAA(t *testing.T) {
	// Given: DNS query packet for AAAA record (IPv6)
	packet := buildDNSQueryPacket(t, "kubernetes.default.svc.cluster.local", dnsmessage.TypeAAAA, 54321)

	// When: Parse packet
	query, err := parseDNSQuery(packet)

	// Then: Extract domain name and AAAA type
	require.NoError(t, err)
	assert.Equal(t, "kubernetes.default.svc.cluster.local.", query.DomainName)
	assert.Equal(t, "AAAA", query.QueryType)
}

// TestParseDNSQuery_InvalidPacket tests error handling for invalid packets
func TestParseDNSQuery_InvalidPacket(t *testing.T) {
	// Given: Invalid DNS packet (garbage data)
	packet := []byte{0xFF, 0xFF, 0xFF, 0xFF}

	// When: Parse packet
	query, err := parseDNSQuery(packet)

	// Then: Return error
	assert.Error(t, err)
	assert.Nil(t, query)
	assert.Contains(t, err.Error(), "failed to parse DNS message")
}

// TestParseDNSResponse_Success tests successful DNS response parsing
func TestParseDNSResponse_Success(t *testing.T) {
	// Given: DNS response with NOERROR and 1 answer (10.0.0.1)
	packet := buildDNSResponsePacket(t, 12345, dnsmessage.RCodeSuccess, []string{"10.0.0.1"})

	// When: Parse packet
	response, err := parseDNSResponse(packet)

	// Then: Extract response code and answers
	require.NoError(t, err)
	assert.Equal(t, uint16(12345), response.QueryID)
	assert.Equal(t, "NOERROR", response.ResponseCode)
	assert.Equal(t, []string{"10.0.0.1"}, response.Answers)
}

// TestParseDNSResponse_NXDOMAIN tests NXDOMAIN response parsing
func TestParseDNSResponse_NXDOMAIN(t *testing.T) {
	// Given: DNS response with NXDOMAIN (domain doesn't exist)
	packet := buildDNSResponsePacket(t, 12345, dnsmessage.RCodeNameError, nil)

	// When: Parse packet
	response, err := parseDNSResponse(packet)

	// Then: Detect NXDOMAIN
	require.NoError(t, err)
	assert.Equal(t, "NXDOMAIN", response.ResponseCode)
	assert.Empty(t, response.Answers)
}

// TestParseDNSResponse_SERVFAIL tests SERVFAIL response parsing
func TestParseDNSResponse_SERVFAIL(t *testing.T) {
	// Given: DNS response with SERVFAIL (DNS server error)
	packet := buildDNSResponsePacket(t, 12345, dnsmessage.RCodeServerFailure, nil)

	// When: Parse packet
	response, err := parseDNSResponse(packet)

	// Then: Detect SERVFAIL
	require.NoError(t, err)
	assert.Equal(t, "SERVFAIL", response.ResponseCode)
}

// TestDetectDNSProblem_NXDOMAIN tests NXDOMAIN problem detection
func TestDetectDNSProblem_NXDOMAIN(t *testing.T) {
	// Given: Query and NXDOMAIN response
	query := &DNSQuery{
		QueryID:    123,
		DomainName: "nonexistent.svc.cluster.local.",
		Timestamp:  time.Now(),
	}
	response := &DNSResponse{
		QueryID:      123,
		ResponseCode: "NXDOMAIN",
		Latency:      50 * time.Millisecond,
	}

	// When: Detect problem
	problem := detectDNSProblem(query, response)

	// Then: Identify as NXDOMAIN
	assert.Equal(t, "dns_nxdomain", problem)
}

// TestDetectDNSProblem_SlowQuery tests slow query detection
func TestDetectDNSProblem_SlowQuery(t *testing.T) {
	// Given: Query with 150ms latency (threshold: 100ms)
	query := &DNSQuery{
		QueryID:   123,
		Timestamp: time.Now().Add(-150 * time.Millisecond),
	}
	response := &DNSResponse{
		QueryID:      123,
		ResponseCode: "NOERROR",
		Latency:      150 * time.Millisecond,
	}

	// When: Detect problem
	problem := detectDNSProblem(query, response)

	// Then: Identify as slow query
	assert.Equal(t, "dns_slow_query", problem)
}

// TestDetectDNSProblem_SERVFAIL tests SERVFAIL problem detection
func TestDetectDNSProblem_SERVFAIL(t *testing.T) {
	// Given: Query and SERVFAIL response
	query := &DNSQuery{QueryID: 123, Timestamp: time.Now()}
	response := &DNSResponse{
		QueryID:      123,
		ResponseCode: "SERVFAIL",
		Latency:      30 * time.Millisecond,
	}

	// When: Detect problem
	problem := detectDNSProblem(query, response)

	// Then: Identify as SERVFAIL
	assert.Equal(t, "dns_servfail", problem)
}

// TestDetectDNSProblem_Success tests successful query (no problem)
func TestDetectDNSProblem_Success(t *testing.T) {
	// Given: Fast successful query
	query := &DNSQuery{QueryID: 123, Timestamp: time.Now()}
	response := &DNSResponse{
		QueryID:      123,
		ResponseCode: "NOERROR",
		Latency:      10 * time.Millisecond,
		Answers:      []string{"10.0.0.1"},
	}

	// When: Detect problem
	problem := detectDNSProblem(query, response)

	// Then: No problem (success)
	assert.Equal(t, "dns_success", problem)
}

// TestDNSMonitor_Lifecycle tests monitor start/stop
func TestDNSMonitor_Lifecycle(t *testing.T) {
	// Given: New DNS monitor
	monitor, err := NewDNSMonitor()
	require.NoError(t, err)
	require.NotNil(t, monitor)

	// When: Start and stop monitor
	monitor.Start()
	time.Sleep(50 * time.Millisecond) // Let cleanup goroutine start
	monitor.Stop()

	// Then: No errors, clean shutdown
	assert.NotNil(t, monitor.cleanupCtx)
}

// TestDNSMonitor_ProcessQueryResponse tests query/response matching
func TestDNSMonitor_ProcessQueryResponse(t *testing.T) {
	// Given: DNS monitor
	monitor, err := NewDNSMonitor()
	require.NoError(t, err)
	monitor.Start()
	defer monitor.Stop()

	// Given: DNS query packet
	queryPacket := buildDNSQueryPacket(t, "example.com", dnsmessage.TypeA, 12345)
	queryTime := time.Now()

	// When: Process query
	ctx := context.Background()
	err = monitor.ProcessQuery(ctx, queryPacket, "10.0.0.1", "8.8.8.8", queryTime)
	require.NoError(t, err)

	// Then: Query is stored with composite key
	key := queryKey{id: 12345, srcIP: "10.0.0.1", dstIP: "8.8.8.8"}
	_, found := monitor.pendingQueries.Load(key)
	assert.True(t, found, "Query should be stored in pending queries")

	// Given: DNS response packet (swapped IPs - response goes back to source)
	responsePacket := buildDNSResponsePacket(t, 12345, dnsmessage.RCodeSuccess, []string{"93.184.216.34"})
	responseTime := queryTime.Add(20 * time.Millisecond)

	// When: Process response (srcIP and dstIP swapped)
	problem, err := monitor.ProcessResponse(ctx, responsePacket, "8.8.8.8", "10.0.0.1", responseTime)
	require.NoError(t, err)

	// Then: Response matched and problem detected
	assert.Equal(t, "dns_success", problem)

	// Then: Query removed from pending
	_, found = monitor.pendingQueries.Load(key)
	assert.False(t, found, "Query should be removed after response")
}

// TestDNSMonitor_UnmatchedResponse tests response without query
func TestDNSMonitor_UnmatchedResponse(t *testing.T) {
	// Given: DNS monitor
	monitor, err := NewDNSMonitor()
	require.NoError(t, err)
	monitor.Start()
	defer monitor.Stop()

	// Given: DNS response packet (no matching query)
	responsePacket := buildDNSResponsePacket(t, 54321, dnsmessage.RCodeSuccess, []string{"1.2.3.4"})

	// When: Process response (no matching query for this ID+IPs)
	ctx := context.Background()
	problem, err := monitor.ProcessResponse(ctx, responsePacket, "1.1.1.1", "2.2.2.2", time.Now())
	require.NoError(t, err)

	// Then: Unmatched response detected
	assert.Equal(t, "dns_unmatched_response", problem)
}

// TestDNSMonitor_QueryIDCollision tests that same ID from different IPs doesn't collide
func TestDNSMonitor_QueryIDCollision(t *testing.T) {
	// Given: DNS monitor
	monitor, err := NewDNSMonitor()
	require.NoError(t, err)
	monitor.Start()
	defer monitor.Stop()

	// Given: Two queries with SAME ID but different IPs
	queryPacket1 := buildDNSQueryPacket(t, "example.com", dnsmessage.TypeA, 12345)
	queryPacket2 := buildDNSQueryPacket(t, "different.com", dnsmessage.TypeA, 12345) // SAME ID!

	queryTime := time.Now()
	ctx := context.Background()

	// When: Process both queries
	err = monitor.ProcessQuery(ctx, queryPacket1, "10.0.0.1", "8.8.8.8", queryTime)
	require.NoError(t, err)

	err = monitor.ProcessQuery(ctx, queryPacket2, "10.0.0.2", "8.8.8.8", queryTime) // Different srcIP
	require.NoError(t, err)

	// Then: Both queries stored (no collision!)
	key1 := queryKey{id: 12345, srcIP: "10.0.0.1", dstIP: "8.8.8.8"}
	key2 := queryKey{id: 12345, srcIP: "10.0.0.2", dstIP: "8.8.8.8"}

	_, found1 := monitor.pendingQueries.Load(key1)
	_, found2 := monitor.pendingQueries.Load(key2)

	assert.True(t, found1, "First query should be stored")
	assert.True(t, found2, "Second query should be stored (no collision)")

	// When: Responses arrive
	responsePacket := buildDNSResponsePacket(t, 12345, dnsmessage.RCodeSuccess, []string{"1.2.3.4"})

	// Response for first query
	problem1, err := monitor.ProcessResponse(ctx, responsePacket, "8.8.8.8", "10.0.0.1", queryTime.Add(10*time.Millisecond))
	require.NoError(t, err)
	assert.Equal(t, "dns_success", problem1)

	// Response for second query
	problem2, err := monitor.ProcessResponse(ctx, responsePacket, "8.8.8.8", "10.0.0.2", queryTime.Add(15*time.Millisecond))
	require.NoError(t, err)
	assert.Equal(t, "dns_success", problem2)

	// Then: Both queries matched correctly
	_, found1 = monitor.pendingQueries.Load(key1)
	_, found2 = monitor.pendingQueries.Load(key2)
	assert.False(t, found1, "First query should be removed")
	assert.False(t, found2, "Second query should be removed")
}

// TestDNSMonitor_StaleQueryCleanup tests timeout detection
func TestDNSMonitor_StaleQueryCleanup(t *testing.T) {
	// Given: DNS monitor with short timeout
	monitor, err := NewDNSMonitor()
	require.NoError(t, err)
	monitor.queryTimeout = 100 * time.Millisecond
	monitor.cleanupInterval = 50 * time.Millisecond

	// Given: Stale query (older than timeout)
	staleQuery := &DNSQuery{
		QueryID:   123,
		SrcIP:     "10.0.0.1",
		DstIP:     "8.8.8.8",
		Timestamp: time.Now().Add(-200 * time.Millisecond),
	}
	key := queryKey{id: 123, srcIP: "10.0.0.1", dstIP: "8.8.8.8"}
	monitor.pendingQueries.Store(key, staleQuery)

	// When: Run cleanup
	monitor.cleanupStaleQueries()

	// Then: Stale query removed
	_, found := monitor.pendingQueries.Load(key)
	assert.False(t, found, "Stale query should be removed")

	// Then: Timeout counter incremented
	assert.Equal(t, int64(1), monitor.timeoutCount.Load())
}

// buildDNSQueryPacket creates a DNS query packet for testing
func buildDNSQueryPacket(t *testing.T, domain string, qtype dnsmessage.Type, queryID uint16) []byte {
	t.Helper()

	// Ensure domain ends with a dot (canonical format)
	if domain[len(domain)-1] != '.' {
		domain = domain + "."
	}

	name, err := dnsmessage.NewName(domain)
	require.NoError(t, err)

	msg := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:               queryID,
			OpCode:           0,
			RecursionDesired: true,
		},
		Questions: []dnsmessage.Question{
			{
				Name:  name,
				Type:  qtype,
				Class: dnsmessage.ClassINET,
			},
		},
	}

	packet, err := msg.Pack()
	require.NoError(t, err)
	return packet
}

// buildDNSResponsePacket creates a DNS response packet for testing
func buildDNSResponsePacket(t *testing.T, queryID uint16, rcode dnsmessage.RCode, answers []string) []byte {
	t.Helper()

	msg := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:       queryID,
			Response: true,
			OpCode:   0,
			RCode:    rcode,
		},
	}

	// Add answers if provided
	for range answers {
		name, err := dnsmessage.NewName("example.com.")
		require.NoError(t, err)

		// Parse IP address (simplified - just store as string in test)
		msg.Answers = append(msg.Answers, dnsmessage.Resource{
			Header: dnsmessage.ResourceHeader{
				Name:  name,
				Type:  dnsmessage.TypeA,
				Class: dnsmessage.ClassINET,
				TTL:   300,
			},
			Body: &dnsmessage.AResource{
				A: [4]byte{10, 0, 0, 1}, // Simplified - real parser will extract properly
			},
		})
	}

	packet, err := msg.Pack()
	require.NoError(t, err)
	return packet
}
