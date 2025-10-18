package network

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

// TestDNSMonitor_DetectProblem tests the monitor's detectProblem method with metrics
func TestDNSMonitor_DetectProblem(t *testing.T) {
	tests := []struct {
		name         string
		responseCode string
		latency      time.Duration
		expected     string
	}{
		{
			name:         "NXDOMAIN",
			responseCode: "NXDOMAIN",
			latency:      50 * time.Millisecond,
			expected:     "dns_nxdomain",
		},
		{
			name:         "SERVFAIL",
			responseCode: "SERVFAIL",
			latency:      30 * time.Millisecond,
			expected:     "dns_servfail",
		},
		{
			name:         "slow query",
			responseCode: "NOERROR",
			latency:      150 * time.Millisecond,
			expected:     "dns_slow_query",
		},
		{
			name:         "success",
			responseCode: "NOERROR",
			latency:      10 * time.Millisecond,
			expected:     "dns_success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor, err := NewDNSMonitor()
			require.NoError(t, err)
			monitor.Start()
			defer monitor.Stop()

			ctx := context.Background()
			query := &DNSQuery{
				QueryID:   123,
				Timestamp: time.Now(),
			}
			response := &DNSResponse{
				QueryID:      123,
				ResponseCode: tt.responseCode,
				Latency:      tt.latency,
			}

			problem := monitor.detectProblem(ctx, query, response)
			assert.Equal(t, tt.expected, problem)
		})
	}
}

// TestNormalizeQueryType tests DNS query type normalization
func TestNormalizeQueryType(t *testing.T) {
	tests := []struct {
		qtype    dnsmessage.Type
		expected string
	}{
		{dnsmessage.TypeA, "A"},
		{dnsmessage.TypeAAAA, "AAAA"},
		{dnsmessage.TypeCNAME, "CNAME"},
		{dnsmessage.TypeMX, "MX"},
		{dnsmessage.TypeNS, "NS"},
		{dnsmessage.TypePTR, "PTR"},
		{dnsmessage.TypeSOA, "SOA"},
		{dnsmessage.TypeSRV, "SRV"},
		{dnsmessage.TypeTXT, "TXT"},
		{dnsmessage.Type(999), "999"}, // Unknown type
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := normalizeQueryType(tt.qtype)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRcodeToString tests DNS response code conversion
func TestRcodeToString(t *testing.T) {
	tests := []struct {
		rcode    dnsmessage.RCode
		expected string
	}{
		{dnsmessage.RCodeSuccess, "NOERROR"},
		{dnsmessage.RCodeFormatError, "FORMERR"},
		{dnsmessage.RCodeServerFailure, "SERVFAIL"},
		{dnsmessage.RCodeNameError, "NXDOMAIN"},
		{dnsmessage.RCodeNotImplemented, "NOTIMP"},
		{dnsmessage.RCodeRefused, "REFUSED"},
		{dnsmessage.RCode(99), "RCODE_99"}, // Unknown rcode
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := rcodeToString(tt.rcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseDNSQuery_AllTypes tests parsing various DNS query types
func TestParseDNSQuery_AllTypes(t *testing.T) {
	types := []dnsmessage.Type{
		dnsmessage.TypeMX,
		dnsmessage.TypeNS,
		dnsmessage.TypePTR,
		dnsmessage.TypeSOA,
		dnsmessage.TypeSRV,
		dnsmessage.TypeTXT,
	}

	for _, qtype := range types {
		t.Run(normalizeQueryType(qtype), func(t *testing.T) {
			packet := buildTestDNSQuery(t, "example.com", qtype, 12345)
			query, err := parseDNSQuery(packet)
			require.NoError(t, err)
			assert.NotEmpty(t, query.DomainName)
			assert.Equal(t, normalizeQueryType(qtype), query.QueryType)
		})
	}
}

// TestParseDNSResponse_AllRcodes tests parsing various DNS response codes
func TestParseDNSResponse_AllRcodes(t *testing.T) {
	rcodes := []dnsmessage.RCode{
		dnsmessage.RCodeFormatError,
		dnsmessage.RCodeNotImplemented,
		dnsmessage.RCodeRefused,
	}

	for _, rcode := range rcodes {
		t.Run(rcodeToString(rcode), func(t *testing.T) {
			packet := buildTestDNSResponse(t, 12345, rcode, nil)
			response, err := parseDNSResponse(packet)
			require.NoError(t, err)
			assert.Equal(t, rcodeToString(rcode), response.ResponseCode)
		})
	}
}

// buildTestDNSQuery creates a DNS query packet for testing
func buildTestDNSQuery(t *testing.T, domain string, qtype dnsmessage.Type, queryID uint16) []byte {
	t.Helper()

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

// buildTestDNSResponse creates a DNS response packet for testing
func buildTestDNSResponse(t *testing.T, queryID uint16, rcode dnsmessage.RCode, answers []string) []byte {
	t.Helper()

	msg := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:       queryID,
			Response: true,
			OpCode:   0,
			RCode:    rcode,
		},
	}

	packet, err := msg.Pack()
	require.NoError(t, err)
	return packet
}
