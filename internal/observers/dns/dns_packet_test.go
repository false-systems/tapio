package dns

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test DNS packet parsing for both UDP and TCP

func TestParseDNSQueryUDP(t *testing.T) {
	tests := []struct {
		name        string
		packet      []byte
		expected    *DNSPacket
		expectError bool
	}{
		{
			name:   "valid A record query",
			packet: buildDNSQuery("example.com", DNSTypeA, 0x1234, false),
			expected: &DNSPacket{
				Header: DNSHeader{
					ID:      0x1234,
					Flags:   0x0100, // Standard query
					QDCount: 1,
				},
				QueryName: "example.com",
				QueryType: DNSTypeA,
				Protocol:  ProtocolUDP,
			},
			expectError: false,
		},
		{
			name:   "valid AAAA query",
			packet: buildDNSQuery("ipv6.example.com", DNSTypeAAAA, 0x5678, false),
			expected: &DNSPacket{
				Header: DNSHeader{
					ID:      0x5678,
					Flags:   0x0100,
					QDCount: 1,
				},
				QueryName: "ipv6.example.com",
				QueryType: DNSTypeAAAA,
				Protocol:  ProtocolUDP,
			},
			expectError: false,
		},
		{
			name:   "CoreDNS cluster.local query",
			packet: buildDNSQuery("service.default.svc.cluster.local", DNSTypeA, 0x9ABC, false),
			expected: &DNSPacket{
				Header: DNSHeader{
					ID:      0x9ABC,
					Flags:   0x0100,
					QDCount: 1,
				},
				QueryName:    "service.default.svc.cluster.local",
				QueryType:    DNSTypeA,
				Protocol:     ProtocolUDP,
				IsCoreDNS:    true,
				K8sService:   "service",
				K8sNamespace: "default",
			},
			expectError: false,
		},
		{
			name:        "malformed packet - too short",
			packet:      []byte{0x12, 0x34},
			expected:    nil,
			expectError: true,
		},
		{
			name:        "malformed packet - invalid header",
			packet:      []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			expected:    nil,
			expectError: true,
		},
		{
			name:   "query with EDNS0",
			packet: buildDNSQueryWithEDNS0("large.example.com", DNSTypeA, 0xDEAD, 4096),
			expected: &DNSPacket{
				Header: DNSHeader{
					ID:      0xDEAD,
					Flags:   0x0100,
					QDCount: 1,
					ARCount: 1,
				},
				QueryName: "large.example.com",
				QueryType: DNSTypeA,
				Protocol:  ProtocolUDP,
				EDNS0Size: 4096,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewDNSParser()
			result, err := parser.ParseUDP(tt.packet)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected.Header.ID, result.Header.ID)
				assert.Equal(t, tt.expected.QueryName, result.QueryName)
				assert.Equal(t, tt.expected.QueryType, result.QueryType)

				if tt.expected.IsCoreDNS {
					assert.True(t, result.IsCoreDNS)
					assert.Equal(t, tt.expected.K8sService, result.K8sService)
					assert.Equal(t, tt.expected.K8sNamespace, result.K8sNamespace)
				}
			}
		})
	}
}

func TestParseDNSQueryTCP(t *testing.T) {
	tests := []struct {
		name        string
		packet      []byte
		expected    *DNSPacket
		expectError bool
	}{
		{
			name:   "valid TCP DNS query with length prefix",
			packet: buildTCPDNSQuery("tcp.example.com", DNSTypeA, 0x1234),
			expected: &DNSPacket{
				Header: DNSHeader{
					ID:      0x1234,
					Flags:   0x0100,
					QDCount: 1,
				},
				QueryName: "tcp.example.com",
				QueryType: DNSTypeA,
				Protocol:  ProtocolTCP,
				TCPLength: calculateDNSLength("tcp.example.com"),
			},
			expectError: false,
		},
		{
			name:   "TCP zone transfer query (AXFR)",
			packet: buildTCPDNSQuery("zone.example.com", DNSTypeAXFR, 0xBEEF),
			expected: &DNSPacket{
				Header: DNSHeader{
					ID:      0xBEEF,
					Flags:   0x0100,
					QDCount: 1,
				},
				QueryName: "zone.example.com",
				QueryType: DNSTypeAXFR,
				Protocol:  ProtocolTCP,
			},
			expectError: false,
		},
		{
			name:        "TCP packet missing length prefix",
			packet:      buildDNSQuery("bad.example.com", DNSTypeA, 0x1111, false),
			expected:    nil,
			expectError: true,
		},
		{
			name:        "TCP packet with wrong length",
			packet:      buildBadTCPPacket(),
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewDNSParser()
			result, err := parser.ParseTCP(tt.packet)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected.Header.ID, result.Header.ID)
				assert.Equal(t, tt.expected.QueryName, result.QueryName)
				assert.Equal(t, tt.expected.QueryType, result.QueryType)
				assert.Equal(t, ProtocolTCP, result.Protocol)
			}
		})
	}
}

func TestParseDNSResponse(t *testing.T) {
	tests := []struct {
		name        string
		packet      []byte
		expected    *DNSPacket
		expectError bool
	}{
		{
			name:   "successful A record response",
			packet: buildDNSResponse("example.com", DNSTypeA, 0x1234, DNSRCodeSuccess, []string{"93.184.216.34"}),
			expected: &DNSPacket{
				Header: DNSHeader{
					ID:      0x1234,
					Flags:   0x8180, // Response, no error
					QDCount: 1,
					ANCount: 1,
				},
				QueryName:    "example.com",
				QueryType:    DNSTypeA,
				ResponseCode: DNSRCodeSuccess,
				Answers:      []string{"93.184.216.34"},
			},
			expectError: false,
		},
		{
			name:   "NXDOMAIN response",
			packet: buildDNSResponse("nonexistent.example.com", DNSTypeA, 0x5678, DNSRCodeNXDomain, nil),
			expected: &DNSPacket{
				Header: DNSHeader{
					ID:      0x5678,
					Flags:   0x8183, // Response, NXDOMAIN
					QDCount: 1,
					ANCount: 0,
				},
				QueryName:    "nonexistent.example.com",
				QueryType:    DNSTypeA,
				ResponseCode: DNSRCodeNXDomain,
				Answers:      nil,
			},
			expectError: false,
		},
		{
			name:   "SERVFAIL response",
			packet: buildDNSResponse("failed.example.com", DNSTypeA, 0x9999, DNSRCodeServerFailure, nil),
			expected: &DNSPacket{
				Header: DNSHeader{
					ID:      0x9999,
					Flags:   0x8182, // Response, SERVFAIL
					QDCount: 1,
				},
				QueryName:    "failed.example.com",
				QueryType:    DNSTypeA,
				ResponseCode: DNSRCodeServerFailure,
			},
			expectError: false,
		},
		{
			name:   "truncated response",
			packet: buildTruncatedResponse("huge.example.com", DNSTypeANY, 0xAAAA),
			expected: &DNSPacket{
				Header: DNSHeader{
					ID:      0xAAAA,
					Flags:   0x8380, // Response, Truncated
					QDCount: 1,
				},
				QueryName:   "huge.example.com",
				QueryType:   DNSTypeANY,
				IsTruncated: true,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewDNSParser()
			result, err := parser.ParseResponse(tt.packet)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected.Header.ID, result.Header.ID)
				assert.Equal(t, tt.expected.QueryName, result.QueryName)
				assert.Equal(t, tt.expected.ResponseCode, result.ResponseCode)

				if tt.expected.IsTruncated {
					assert.True(t, result.IsTruncated)
				}
			}
		})
	}
}

func TestDetectCoreDNSQuery(t *testing.T) {
	tests := []struct {
		name         string
		queryName    string
		isCoreDNS    bool
		k8sService   string
		k8sNamespace string
	}{
		{
			name:         "standard k8s service",
			queryName:    "nginx.default.svc.cluster.local",
			isCoreDNS:    true,
			k8sService:   "nginx",
			k8sNamespace: "default",
		},
		{
			name:         "k8s pod IP",
			queryName:    "10-244-0-5.default.pod.cluster.local",
			isCoreDNS:    true,
			k8sService:   "",
			k8sNamespace: "default",
		},
		{
			name:         "k8s service with port",
			queryName:    "_http._tcp.nginx.default.svc.cluster.local",
			isCoreDNS:    true,
			k8sService:   "nginx",
			k8sNamespace: "default",
		},
		{
			name:         "kube-system service",
			queryName:    "kube-dns.kube-system.svc.cluster.local",
			isCoreDNS:    true,
			k8sService:   "kube-dns",
			k8sNamespace: "kube-system",
		},
		{
			name:         "external domain",
			queryName:    "google.com",
			isCoreDNS:    false,
			k8sService:   "",
			k8sNamespace: "",
		},
		{
			name:         "short k8s name",
			queryName:    "nginx.default.svc",
			isCoreDNS:    true,
			k8sService:   "nginx",
			k8sNamespace: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewCoreDNSDetector()
			result := detector.Detect(tt.queryName)

			assert.Equal(t, tt.isCoreDNS, result.IsCoreDNS)
			if tt.isCoreDNS {
				assert.Equal(t, tt.k8sService, result.K8sService)
				assert.Equal(t, tt.k8sNamespace, result.K8sNamespace)
			}
		})
	}
}

func TestCalculateLatency(t *testing.T) {
	tests := []struct {
		name            string
		queryTime       time.Time
		responseTime    time.Time
		expectedMs      float64
		expectedProblem DNSProblemType
	}{
		{
			name:            "fast query",
			queryTime:       time.Now(),
			responseTime:    time.Now().Add(10 * time.Millisecond),
			expectedMs:      10,
			expectedProblem: DNSProblemNone,
		},
		{
			name:            "slow query",
			queryTime:       time.Now(),
			responseTime:    time.Now().Add(150 * time.Millisecond),
			expectedMs:      150,
			expectedProblem: DNSProblemSlow,
		},
		{
			name:            "timeout",
			queryTime:       time.Now(),
			responseTime:    time.Now().Add(5 * time.Second),
			expectedMs:      5000,
			expectedProblem: DNSProblemTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calculator := NewLatencyCalculator(100, 5000) // 100ms slow, 5s timeout

			latencyMs := calculator.Calculate(tt.queryTime, tt.responseTime)
			problem := calculator.DetectProblem(latencyMs)

			assert.InDelta(t, tt.expectedMs, latencyMs, 1.0)
			assert.Equal(t, tt.expectedProblem, problem)
		})
	}
}

func TestTCPFragmentation(t *testing.T) {
	// Test handling of fragmented TCP DNS packets
	largeQuery := buildLargeDNSQuery(2048) // Larger than typical MTU

	fragments := fragmentTCPPacket(largeQuery, 512)
	require.Greater(t, len(fragments), 1)

	assembler := NewTCPAssembler()

	for i, fragment := range fragments[:len(fragments)-1] {
		complete, err := assembler.AddFragment(fragment)
		assert.NoError(t, err)
		assert.False(t, complete, "Should not be complete at fragment %d", i)
	}

	// Add final fragment
	complete, err := assembler.AddFragment(fragments[len(fragments)-1])
	assert.NoError(t, err)
	assert.True(t, complete)

	assembled := assembler.GetPacket()
	assert.Equal(t, largeQuery, assembled)
}

func TestTCPSessionTracking(t *testing.T) {
	tracker := NewTCPDNSTracker()

	// Create TCP connection
	conn := &TCPConnection{
		SrcIP:   "10.0.0.1",
		DstIP:   "8.8.8.8",
		SrcPort: 54321,
		DstPort: 53,
		Seq:     1000,
	}

	err := tracker.TrackSession(conn)
	require.NoError(t, err)

	// Send query
	query := buildDNSQuery("tcp.example.com", DNSTypeA, 0x1234, false)
	err = tracker.TrackQuery(conn, query, 1000)
	require.NoError(t, err)

	// Match response
	response := buildDNSResponse("tcp.example.com", DNSTypeA, 0x1234, DNSRCodeSuccess, []string{"1.2.3.4"})
	matched, latency := tracker.MatchResponse(conn, response, 1050)

	assert.True(t, matched)
	assert.Greater(t, latency, uint64(0))

	// Check cleanup
	tracker.CleanupStale(time.Minute)
	assert.Equal(t, 0, tracker.GetActiveSessions())
}

// Helper functions for building test packets

func buildDNSQuery(domain string, qtype uint16, id uint16, isResponse bool) []byte {
	// Implementation will be in dns_packet.go
	// This is a stub for testing
	return nil
}

func buildTCPDNSQuery(domain string, qtype uint16, id uint16) []byte {
	query := buildDNSQuery(domain, qtype, id, false)
	length := uint16(len(query))

	packet := make([]byte, 2+length)
	binary.BigEndian.PutUint16(packet[0:2], length)
	copy(packet[2:], query)

	return packet
}

func buildDNSResponse(domain string, qtype uint16, id uint16, rcode uint8, answers []string) []byte {
	// Implementation will be in dns_packet.go
	return nil
}

func buildDNSQueryWithEDNS0(domain string, qtype uint16, id uint16, bufferSize uint16) []byte {
	// Implementation will be in dns_packet.go
	return nil
}

func buildTruncatedResponse(domain string, qtype uint16, id uint16) []byte {
	// Implementation will be in dns_packet.go
	return nil
}

func buildBadTCPPacket() []byte {
	// Create packet with mismatched length
	packet := make([]byte, 100)
	binary.BigEndian.PutUint16(packet[0:2], 200) // Wrong length
	return packet
}

func buildLargeDNSQuery(size int) []byte {
	// Build a query that requires fragmentation
	return make([]byte, size)
}

func fragmentTCPPacket(packet []byte, mtu int) [][]byte {
	var fragments [][]byte
	for i := 0; i < len(packet); i += mtu {
		end := i + mtu
		if end > len(packet) {
			end = len(packet)
		}
		fragments = append(fragments, packet[i:end])
	}
	return fragments
}

func calculateDNSLength(domain string) uint16 {
	// Calculate expected DNS packet length for domain
	return uint16(12 + len(domain) + 5) // Header + domain + type/class
}
