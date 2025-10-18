package network

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/net/dns/dnsmessage"
)

// DNSMonitor tracks DNS queries and responses
type DNSMonitor struct {
	// Query tracking (match responses to queries)
	pendingQueries sync.Map // key: query_id → value: *DNSQuery

	// Problem counters
	nxdomainCount  atomic.Int64
	timeoutCount   atomic.Int64
	slowQueryCount atomic.Int64
	servfailCount  atomic.Int64

	// OTEL metrics
	dnsQueriesTotal     metric.Int64Counter
	dnsErrorsTotal      metric.Int64Counter
	dnsLatencyHistogram metric.Float64Histogram

	// Cleanup
	cleanupInterval time.Duration
	queryTimeout    time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// DNSQuery represents a parsed DNS query
type DNSQuery struct {
	QueryID    uint16
	DomainName string
	QueryType  string
	SrcIP      string
	DstIP      string
	Timestamp  time.Time
}

// DNSResponse represents a parsed DNS response
type DNSResponse struct {
	QueryID      uint16
	ResponseCode string
	Answers      []string
	Latency      time.Duration
}

// NewDNSMonitor creates a new DNS monitor
func NewDNSMonitor() (*DNSMonitor, error) {
	meter := otel.Meter("tapio.observer.network.dns")

	dnsQueriesTotal, err := meter.Int64Counter(
		"dns_queries_total",
		metric.WithDescription("Total number of DNS queries observed"),
		metric.WithUnit("{queries}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create dns_queries_total counter: %w", err)
	}

	dnsErrorsTotal, err := meter.Int64Counter(
		"dns_errors_total",
		metric.WithDescription("Total number of DNS errors (NXDOMAIN, SERVFAIL, timeouts)"),
		metric.WithUnit("{errors}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create dns_errors_total counter: %w", err)
	}

	dnsLatencyHistogram, err := meter.Float64Histogram(
		"dns_latency_ms",
		metric.WithDescription("DNS query latency in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create dns_latency_ms histogram: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &DNSMonitor{
		dnsQueriesTotal:     dnsQueriesTotal,
		dnsErrorsTotal:      dnsErrorsTotal,
		dnsLatencyHistogram: dnsLatencyHistogram,
		cleanupInterval:     10 * time.Second,
		queryTimeout:        5 * time.Second,
		ctx:                 ctx,
		cancel:              cancel,
	}, nil
}

// Start begins the DNS monitor cleanup goroutine
func (m *DNSMonitor) Start() {
	m.wg.Add(1)
	go m.cleanupLoop()
}

// Stop stops the DNS monitor and waits for cleanup
func (m *DNSMonitor) Stop() {
	m.cancel()
	m.wg.Wait()
}

// cleanupLoop periodically removes stale queries
func (m *DNSMonitor) cleanupLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.cleanupStaleQueries()
		}
	}
}

// cleanupStaleQueries removes queries older than queryTimeout
func (m *DNSMonitor) cleanupStaleQueries() {
	now := time.Now()
	m.pendingQueries.Range(func(key, value interface{}) bool {
		query := value.(*DNSQuery)
		if now.Sub(query.Timestamp) > m.queryTimeout {
			m.pendingQueries.Delete(key)
			m.timeoutCount.Add(1)
			m.dnsErrorsTotal.Add(m.ctx, 1)
		}
		return true
	})
}

// parseDNSQuery parses a DNS query packet
func parseDNSQuery(packet []byte) (*DNSQuery, error) {
	var parser dnsmessage.Parser
	header, err := parser.Start(packet)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DNS message: %w", err)
	}

	question, err := parser.Question()
	if err != nil {
		return nil, fmt.Errorf("failed to parse DNS question: %w", err)
	}

	return &DNSQuery{
		QueryID:    header.ID,
		DomainName: question.Name.String(),
		QueryType:  normalizeQueryType(question.Type),
	}, nil
}

// parseDNSResponse parses a DNS response packet
func parseDNSResponse(packet []byte) (*DNSResponse, error) {
	var parser dnsmessage.Parser
	header, err := parser.Start(packet)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DNS message: %w", err)
	}

	response := &DNSResponse{
		QueryID:      header.ID,
		ResponseCode: rcodeToString(header.RCode),
		Answers:      []string{},
	}

	// Skip questions
	if err := parser.SkipAllQuestions(); err != nil {
		return nil, fmt.Errorf("failed to skip questions: %w", err)
	}

	// Parse answers
	for {
		answer, err := parser.Answer()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse answer: %w", err)
		}

		// Extract IP address from A record
		if aRecord, ok := answer.Body.(*dnsmessage.AResource); ok {
			ip := fmt.Sprintf("%d.%d.%d.%d",
				aRecord.A[0], aRecord.A[1], aRecord.A[2], aRecord.A[3])
			response.Answers = append(response.Answers, ip)
		}

		// Extract IPv6 address from AAAA record
		if aaaaRecord, ok := answer.Body.(*dnsmessage.AAAAResource); ok {
			ip := fmt.Sprintf("%x:%x:%x:%x:%x:%x:%x:%x",
				uint16(aaaaRecord.AAAA[0])<<8|uint16(aaaaRecord.AAAA[1]),
				uint16(aaaaRecord.AAAA[2])<<8|uint16(aaaaRecord.AAAA[3]),
				uint16(aaaaRecord.AAAA[4])<<8|uint16(aaaaRecord.AAAA[5]),
				uint16(aaaaRecord.AAAA[6])<<8|uint16(aaaaRecord.AAAA[7]),
				uint16(aaaaRecord.AAAA[8])<<8|uint16(aaaaRecord.AAAA[9]),
				uint16(aaaaRecord.AAAA[10])<<8|uint16(aaaaRecord.AAAA[11]),
				uint16(aaaaRecord.AAAA[12])<<8|uint16(aaaaRecord.AAAA[13]),
				uint16(aaaaRecord.AAAA[14])<<8|uint16(aaaaRecord.AAAA[15]))
			response.Answers = append(response.Answers, ip)
		}
	}

	return response, nil
}

// ProcessQuery processes a DNS query packet
func (m *DNSMonitor) ProcessQuery(packet []byte, srcIP, dstIP string, timestamp time.Time) error {
	query, err := parseDNSQuery(packet)
	if err != nil {
		return fmt.Errorf("failed to parse DNS query: %w", err)
	}

	// Add source/destination IPs and timestamp
	query.SrcIP = srcIP
	query.DstIP = dstIP
	query.Timestamp = timestamp

	// Store query for matching with response
	m.pendingQueries.Store(query.QueryID, query)
	m.dnsQueriesTotal.Add(m.ctx, 1)

	return nil
}

// ProcessResponse processes a DNS response packet and matches it with a query
func (m *DNSMonitor) ProcessResponse(packet []byte, timestamp time.Time) (string, error) {
	response, err := parseDNSResponse(packet)
	if err != nil {
		return "", fmt.Errorf("failed to parse DNS response: %w", err)
	}

	// Find matching query
	value, found := m.pendingQueries.LoadAndDelete(response.QueryID)
	if !found {
		return "dns_unmatched_response", nil
	}

	query := value.(*DNSQuery)
	response.Latency = timestamp.Sub(query.Timestamp)

	// Record latency metric
	m.dnsLatencyHistogram.Record(m.ctx, float64(response.Latency.Milliseconds()))

	// Detect problems
	problem := m.detectProblem(query, response)

	// Increment error counter if not success
	if problem != "dns_success" {
		m.dnsErrorsTotal.Add(m.ctx, 1)
	}

	return problem, nil
}

// detectProblem detects DNS problems based on query and response
func (m *DNSMonitor) detectProblem(query *DNSQuery, response *DNSResponse) string {
	// NXDOMAIN - domain doesn't exist
	if response.ResponseCode == "NXDOMAIN" {
		m.nxdomainCount.Add(1)
		return "dns_nxdomain"
	}

	// SERVFAIL - DNS server error
	if response.ResponseCode == "SERVFAIL" {
		m.servfailCount.Add(1)
		return "dns_servfail"
	}

	// Slow query (>100ms)
	if response.Latency > 100*time.Millisecond {
		m.slowQueryCount.Add(1)
		return "dns_slow_query"
	}

	// Success
	return "dns_success"
}

// detectDNSProblem is a standalone helper for testing (no state mutation)
func detectDNSProblem(query *DNSQuery, response *DNSResponse) string {
	if response.ResponseCode == "NXDOMAIN" {
		return "dns_nxdomain"
	}
	if response.ResponseCode == "SERVFAIL" {
		return "dns_servfail"
	}
	if response.Latency > 100*time.Millisecond {
		return "dns_slow_query"
	}
	return "dns_success"
}

// normalizeQueryType converts DNS type to short string (TypeA -> A)
func normalizeQueryType(qtype dnsmessage.Type) string {
	switch qtype {
	case dnsmessage.TypeA:
		return "A"
	case dnsmessage.TypeAAAA:
		return "AAAA"
	case dnsmessage.TypeCNAME:
		return "CNAME"
	case dnsmessage.TypeMX:
		return "MX"
	case dnsmessage.TypeNS:
		return "NS"
	case dnsmessage.TypePTR:
		return "PTR"
	case dnsmessage.TypeSOA:
		return "SOA"
	case dnsmessage.TypeSRV:
		return "SRV"
	case dnsmessage.TypeTXT:
		return "TXT"
	default:
		return qtype.String()
	}
}

// rcodeToString converts DNS response code to string
func rcodeToString(rcode dnsmessage.RCode) string {
	switch rcode {
	case dnsmessage.RCodeSuccess:
		return "NOERROR"
	case dnsmessage.RCodeFormatError:
		return "FORMERR"
	case dnsmessage.RCodeServerFailure:
		return "SERVFAIL"
	case dnsmessage.RCodeNameError:
		return "NXDOMAIN"
	case dnsmessage.RCodeNotImplemented:
		return "NOTIMP"
	case dnsmessage.RCodeRefused:
		return "REFUSED"
	default:
		return fmt.Sprintf("RCODE_%d", rcode)
	}
}
