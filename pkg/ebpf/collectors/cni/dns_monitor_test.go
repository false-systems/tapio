package cni

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/events/opinionated"
)

func TestDNSQuery_Validation(t *testing.T) {
	query := &DNSQuery{
		QueryID:      12345,
		TransactionID: "dns-001",
		QueryName:    "kubernetes.default.svc.cluster.local",
		QueryType:    "A",
		QueryClass:   "IN",
		SourceIP:     net.ParseIP("10.244.1.10"),
		ServerIP:     net.ParseIP("10.96.0.10"),
		ResponseCode: 0,
		Success:      true,
		Latency:      50 * time.Millisecond,
		QueryTime:    time.Now().Add(-50 * time.Millisecond),
		ResponseTime: time.Now(),
	}

	assert.Equal(t, uint16(12345), query.QueryID)
	assert.Equal(t, "dns-001", query.TransactionID)
	assert.Equal(t, "kubernetes.default.svc.cluster.local", query.QueryName)
	assert.Equal(t, "A", query.QueryType)
	assert.True(t, query.Success)
	assert.Equal(t, 50*time.Millisecond, query.Latency)
}

func TestDNSCollector_DetermineSeverity(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name     string
		query    *DNSQuery
		expected opinionated.EventSeverity
	}{
		{
			name: "blocked query",
			query: &DNSQuery{
				Blocked: true,
				Success: false,
			},
			expected: opinionated.SeverityHigh,
		},
		{
			name: "NXDOMAIN response",
			query: &DNSQuery{
				Success:      false,
				ResponseCode: 3, // NXDOMAIN
			},
			expected: opinionated.SeverityMedium,
		},
		{
			name: "server failure",
			query: &DNSQuery{
				Success:      false,
				ResponseCode: 2, // SERVFAIL
			},
			expected: opinionated.SeverityHigh,
		},
		{
			name: "high latency query",
			query: &DNSQuery{
				Success: true,
				Latency: 600 * time.Millisecond,
			},
			expected: opinionated.SeverityMedium,
		},
		{
			name: "successful query",
			query: &DNSQuery{
				Success: true,
				Latency: 50 * time.Millisecond,
			},
			expected: opinionated.SeverityInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			severity := collector.determineDNSSeverity(tt.query)
			assert.Equal(t, tt.expected, severity)
		})
	}
}

func TestDNSCollector_SuspiciousDomainDetection(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name     string
		domain   string
		expected bool
	}{
		{
			name:     "malware domain",
			domain:   "malware-site.com",
			expected: true,
		},
		{
			name:     "phishing domain",
			domain:   "phishing-attack.net",
			expected: true,
		},
		{
			name:     "very long domain",
			domain:   "this-is-an-extremely-long-domain-name-that-exceeds-normal-limits-and-might-indicate-suspicious-activity.com",
			expected: true,
		},
		{
			name:     "too many subdomains",
			domain:   "a.b.c.d.e.f.g.h.i.j.k.example.com",
			expected: true,
		},
		{
			name:     "normal kubernetes domain",
			domain:   "kubernetes.default.svc.cluster.local",
			expected: false,
		},
		{
			name:     "normal external domain",
			domain:   "github.com",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isSuspiciousDomain(tt.domain)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDNSCollector_ExternalDNSDetection(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name     string
		serverIP net.IP
		expected bool
	}{
		{
			name:     "cluster DNS",
			serverIP: net.ParseIP("10.96.0.10"),
			expected: false,
		},
		{
			name:     "EKS DNS",
			serverIP: net.ParseIP("169.254.20.10"),
			expected: false,
		},
		{
			name:     "Google DNS",
			serverIP: net.ParseIP("8.8.8.8"),
			expected: true,
		},
		{
			name:     "Cloudflare DNS",
			serverIP: net.ParseIP("1.1.1.1"),
			expected: true,
		},
		{
			name:     "private range DNS",
			serverIP: net.ParseIP("192.168.1.1"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isExternalDNSServer(tt.serverIP)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDNSCollector_AllowedExternalDNS(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name      string
		queryName string
		expected  bool
	}{
		{
			name:      "google domain",
			queryName: "www.google.com",
			expected:  true,
		},
		{
			name:      "AWS domain",
			queryName: "s3.amazonaws.com",
			expected:  true,
		},
		{
			name:      "docker registry",
			queryName: "index.docker.io",
			expected:  true,
		},
		{
			name:      "kubernetes official",
			queryName: "kubernetes.io",
			expected:  true,
		},
		{
			name:      "suspicious domain",
			queryName: "suspicious-site.evil",
			expected:  false,
		},
		{
			name:      "unknown domain",
			queryName: "random-site.com",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isAllowedExternalDNS(tt.queryName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDNSCollector_DNSTunnelingDetection(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name      string
		queryName string
		expected  bool
	}{
		{
			name:      "normal domain",
			queryName: "kubernetes.default.svc.cluster.local",
			expected:  false,
		},
		{
			name:      "long subdomain with base64",
			queryName: "dGVzdC1kYXRhLWV4ZmlsdHJhdGlvbi1hdHRlbXB0.evil.com",
			expected:  true,
		},
		{
			name:      "too many subdomains",
			queryName: "a.b.c.d.e.f.g.h.i.j.k.l.example.com",
			expected:  true,
		},
		{
			name:      "very long label",
			queryName: "this-is-a-very-long-subdomain-that-exceeds-the-dns-label-limit-of-63-characters-and-should-be-flagged.com",
			expected:  true,
		},
		{
			name:      "suspicious TXT query pattern",
			queryName: "long-txt-query-pattern-that-might-be-used-for-tunneling.evil.com",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isPotentialDNSTunneling(tt.queryName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDNSCollector_Base64Detection(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "base64 string",
			input:    "dGVzdCBzdHJpbmc=",
			expected: true,
		},
		{
			name:     "base64 without padding",
			input:    "dGVzdCBzdHJpbmc",
			expected: true,
		},
		{
			name:     "normal text",
			input:    "kubernetes",
			expected: false,
		},
		{
			name:     "mixed characters",
			input:    "test123!@#",
			expected: false,
		},
		{
			name:     "mostly base64",
			input:    "dGVzdCBzdHJpbmc!",
			expected: false, // 1 non-base64 char out of 16 = 6.25% > 10%
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.looksLikeBase64(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDNSCollector_ProcessDNSQuery(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval:   5 * time.Second,
		EnableDNSMonitoring: true,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	// Mock event channel
	eventCh := make(chan interface{}, 10)
	collector.eventChan = eventCh

	// Add mock pod for enrichment
	collector.podIndex = map[string]*PodInfo{
		"10.244.1.10": {
			Name:      "test-pod",
			Namespace: "default",
			IP:        net.ParseIP("10.244.1.10"),
		},
	}

	query := &DNSQuery{
		QueryID:      12345,
		TransactionID: "dns-test",
		QueryName:    "kubernetes.default.svc.cluster.local",
		QueryType:    "A",
		QueryClass:   "IN",
		SourceIP:     net.ParseIP("10.244.1.10"),
		ServerIP:     net.ParseIP("10.96.0.10"),
		ResponseCode: 0,
		ResponseIPs:  []net.IP{net.ParseIP("10.96.0.1")},
		TTL:          30,
		Success:      true,
		Latency:      50 * time.Millisecond,
		QueryTime:    time.Now().Add(-50 * time.Millisecond),
		ResponseTime: time.Now(),
		Labels:       map[string]string{"service": "coredns"},
	}

	collector.processDNSQuery(query)

	// Verify event was generated
	select {
	case event := <-eventCh:
		opinionatedEvent, ok := event.(*opinionated.OpinionatedEvent)
		require.True(t, ok)
		
		assert.Contains(t, opinionatedEvent.Id, "dns-")
		assert.Equal(t, "dns.query", opinionatedEvent.EventType)
		assert.Equal(t, "default", opinionatedEvent.Namespace)
		assert.Equal(t, "test-pod", opinionatedEvent.PodName)
		
		// Check DNS-specific attributes
		assert.Equal(t, "kubernetes.default.svc.cluster.local", opinionatedEvent.Attributes["dns.query_name"])
		assert.Equal(t, "A", opinionatedEvent.Attributes["dns.query_type"])
		assert.Equal(t, 0, opinionatedEvent.Attributes["dns.response_code"])
		assert.Equal(t, true, opinionatedEvent.Attributes["dns.success"])
		assert.Equal(t, int64(50), opinionatedEvent.Attributes["dns.latency_ms"])
		
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected DNS event but none was generated")
	}

	// Verify metrics were updated
	collector.metrics.mutex.RLock()
	assert.Greater(t, collector.metrics.DNSQueries, uint64(0))
	collector.metrics.mutex.RUnlock()
}

func TestDNSCollector_ProcessBlockedQuery(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval:   5 * time.Second,
		EnableDNSMonitoring: true,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	eventCh := make(chan interface{}, 10)
	collector.eventChan = eventCh

	query := &DNSQuery{
		QueryID:      12346,
		TransactionID: "dns-blocked",
		QueryName:    "malware-site.com",
		QueryType:    "A",
		SourceIP:     net.ParseIP("10.244.1.10"),
		ServerIP:     net.ParseIP("10.96.0.10"),
		ResponseCode: 2, // SERVFAIL
		Success:      false,
		Blocked:      true,
		PolicyName:   "security-policy",
		Error:        "blocked by security policy",
		Latency:      10 * time.Millisecond,
		QueryTime:    time.Now(),
		Labels:       map[string]string{"blocked": "true", "reason": "malware"},
	}

	collector.processDNSQuery(query)

	// Verify event was generated with error severity
	select {
	case event := <-eventCh:
		opinionatedEvent, ok := event.(*opinionated.OpinionatedEvent)
		require.True(t, ok)
		
		assert.Equal(t, opinionated.SeverityError, opinionatedEvent.Severity)
		assert.Contains(t, opinionatedEvent.Message, "[BLOCKED]")
		assert.Equal(t, true, opinionatedEvent.Attributes["dns.blocked"])
		assert.Equal(t, "security-policy", opinionatedEvent.Attributes["dns.policy_name"])
		assert.Equal(t, "blocked by security policy", opinionatedEvent.Attributes["dns.error"])
		
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected blocked DNS event but none was generated")
	}
}

func TestDNSCollector_AnomalyDetection(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	eventCh := make(chan interface{}, 10)
	collector.eventChan = eventCh

	tests := []struct {
		name          string
		query         *DNSQuery
		expectedTags  []string
	}{
		{
			name: "high latency query",
			query: &DNSQuery{
				QueryName: "slow-dns.com",
				Success:   true,
				Latency:   1500 * time.Millisecond,
			},
			expectedTags: []string{"HIGH_LATENCY"},
		},
		{
			name: "suspicious domain",
			query: &DNSQuery{
				QueryName: "malware-site.com",
				Success:   true,
				Latency:   50 * time.Millisecond,
			},
			expectedTags: []string{"SUSPICIOUS_DOMAIN"},
		},
		{
			name: "external DNS",
			query: &DNSQuery{
				QueryName: "unknown-external.com",
				ServerIP:  net.ParseIP("8.8.8.8"),
				Success:   true,
				Latency:   50 * time.Millisecond,
			},
			expectedTags: []string{"EXTERNAL_DNS"},
		},
		{
			name: "potential DNS tunneling",
			query: &DNSQuery{
				QueryName: "dGVzdC1kYXRhLWV4ZmlsdHJhdGlvbi1hdHRlbXB0.evil.com",
				Success:   true,
				Latency:   50 * time.Millisecond,
			},
			expectedTags: []string{"POTENTIAL_DNS_TUNNELING"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set defaults
			if tt.query.QueryID == 0 {
				tt.query.QueryID = 12345
			}
			if tt.query.TransactionID == "" {
				tt.query.TransactionID = "test-transaction"
			}
			if tt.query.QueryType == "" {
				tt.query.QueryType = "A"
			}
			if tt.query.SourceIP == nil {
				tt.query.SourceIP = net.ParseIP("10.244.1.10")
			}
			if tt.query.ServerIP == nil {
				tt.query.ServerIP = net.ParseIP("10.96.0.10")
			}
			if tt.query.QueryTime.IsZero() {
				tt.query.QueryTime = time.Now()
			}

			collector.processDNSQuery(tt.query)

			// Verify event contains expected anomaly tags
			select {
			case event := <-eventCh:
				opinionatedEvent, ok := event.(*opinionated.OpinionatedEvent)
				require.True(t, ok)
				
				for _, tag := range tt.expectedTags {
					assert.Contains(t, opinionatedEvent.Message, fmt.Sprintf("[%s]", tag))
				}
				
			case <-time.After(100 * time.Millisecond):
				t.Fatalf("Expected DNS anomaly event for %s but none was generated", tt.name)
			}
		})
	}
}

func TestDNSStats_UpdateMetrics(t *testing.T) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(t, err)

	// Test successful query
	successQuery := &DNSQuery{
		Success: true,
		Latency: 50 * time.Millisecond,
	}
	collector.updateDNSMetrics(successQuery)

	// Test failed query
	failedQuery := &DNSQuery{
		Success: false,
		Latency: 100 * time.Millisecond,
	}
	collector.updateDNSMetrics(failedQuery)

	collector.metrics.mutex.RLock()
	assert.Equal(t, uint64(2), collector.metrics.DNSQueries)
	assert.Equal(t, uint64(1), collector.metrics.DNSFailures)
	assert.Equal(t, 0.5, collector.metrics.DNSFailureRate)
	assert.Equal(t, 100*time.Millisecond, collector.metrics.DNSLatencyP95)
	collector.metrics.mutex.RUnlock()
}

// Benchmark tests for DNS processing performance
func BenchmarkDNSCollector_ProcessDNSQuery(b *testing.B) {
	config := &CNICollectorConfig{
		CollectionInterval:   5 * time.Second,
		EnableDNSMonitoring: true,
	}

	collector, err := NewCNICollector(config)
	require.NoError(b, err)

	eventCh := make(chan interface{}, 1000)
	collector.eventChan = eventCh

	query := &DNSQuery{
		QueryID:      12345,
		TransactionID: "benchmark-test",
		QueryName:    "test.example.com",
		QueryType:    "A",
		SourceIP:     net.ParseIP("10.244.1.10"),
		ServerIP:     net.ParseIP("10.96.0.10"),
		Success:      true,
		Latency:      50 * time.Millisecond,
		QueryTime:    time.Now(),
		ResponseTime: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		query.TransactionID = fmt.Sprintf("benchmark-%d", i)
		collector.processDNSQuery(query)
	}
}

func BenchmarkDNSCollector_SuspiciousDomainCheck(b *testing.B) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(b, err)

	domains := []string{
		"kubernetes.default.svc.cluster.local",
		"malware-site.com",
		"github.com",
		"very-long-suspicious-domain-name-that-might-indicate-malicious-activity.evil.com",
		"normal-service.default.svc.cluster.local",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		domain := domains[i%len(domains)]
		collector.isSuspiciousDomain(domain)
	}
}

func BenchmarkDNSCollector_DNSTunnelingCheck(b *testing.B) {
	config := &CNICollectorConfig{
		CollectionInterval: 5 * time.Second,
	}

	collector, err := NewCNICollector(config)
	require.NoError(b, err)

	domains := []string{
		"kubernetes.default.svc.cluster.local",
		"dGVzdC1kYXRhLWV4ZmlsdHJhdGlvbi1hdHRlbXB0.evil.com",
		"a.b.c.d.e.f.g.h.i.j.k.l.example.com",
		"normal-service.default.svc.cluster.local",
		"very-long-txt-query-pattern-for-dns-tunneling.malicious.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		domain := domains[i%len(domains)]
		collector.isPotentialDNSTunneling(domain)
	}
}