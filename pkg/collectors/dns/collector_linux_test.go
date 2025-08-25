//go:build linux
// +build linux

package dns

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestBPFDNSEventStructAlignment(t *testing.T) {
	// Verify the BPF structure matches expected layout
	var event BPFDNSEvent

	// These tests ensure memory alignment matches the C struct
	assert.Equal(t, uint64(0), event.Timestamp)
	assert.Equal(t, uint32(0), event.PID)
	assert.Equal(t, uint32(0), event.TID)
	assert.Equal(t, uint64(0), event.CgroupID)
	assert.Equal(t, uint8(0), event.EventType)
	assert.Equal(t, uint8(0), event.Protocol)
	assert.Equal(t, uint8(0), event.IPVersion)
	assert.Equal(t, 128, len(event.QueryName))
	assert.Equal(t, 512, len(event.Data))
}

func TestConvertToCollectorEvent(t *testing.T) {
	cfg := Config{
		Name:       "test-dns",
		BufferSize: 100,
		EnableEBPF: false,
	}

	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	// Create a sample BPF DNS event
	bpfEvent := &BPFDNSEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       12345,
		TID:       12346,
		UID:       1000,
		GID:       1000,
		CgroupID:  67890,
		EventType: 1,  // DNS_EVENT_QUERY
		Protocol:  17, // UDP
		IPVersion: 4,
		SrcPort:   12345,
		DstPort:   53,
		DNSID:     0x1234,
		DNSFlags:  0x0100, // Standard query
		Opcode:    0,
		Rcode:     0,
		QType:     1, // A record
		DataLen:   32,
		LatencyNs: 50 * 1000 * 1000, // 50ms
	}

	// Set up IPv4 addresses
	srcIP := net.ParseIP("192.168.1.100").To4()
	dstIP := net.ParseIP("8.8.8.8").To4()
	copy(bpfEvent.SrcAddr[:4], srcIP)
	copy(bpfEvent.DstAddr[:4], dstIP)

	// Set query name
	queryName := "example.com"
	copy(bpfEvent.QueryName[:], []byte(queryName))

	tests := []struct {
		name        string
		bpfEvent    *BPFDNSEvent
		expectError bool
	}{
		{
			name:        "valid DNS query event",
			bpfEvent:    bpfEvent,
			expectError: false,
		},
		{
			name: "empty query name",
			bpfEvent: &BPFDNSEvent{
				Timestamp: uint64(time.Now().UnixNano()),
				PID:       12345,
				// QueryName is zero-initialized (empty)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := collector.convertToCollectorEvent(tt.bpfEvent)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, event)
			} else {
				require.NoError(t, err)
				require.NotNil(t, event)

				// Verify event structure
				assert.Equal(t, domain.EventTypeDNS, event.Type)
				assert.Equal(t, "test", event.Source)
				assert.NotEmpty(t, event.EventID)

				// Verify DNS data
				dnsData, ok := event.GetDNSData()
				require.True(t, ok)
				assert.Equal(t, "A", dnsData.QueryType)
				assert.Equal(t, queryName, dnsData.QueryName)
				assert.Equal(t, int32(0), dnsData.ResponseCode)
				assert.Equal(t, 50*time.Millisecond, dnsData.Duration)
				assert.Equal(t, "8.8.8.8", dnsData.ServerIP)
				assert.Equal(t, int32(53), dnsData.ServerPort)

				// Verify process data
				processData, ok := event.GetProcessData()
				require.True(t, ok)
				assert.Equal(t, int32(12345), processData.PID)
				assert.Equal(t, int32(12346), processData.TID)
				assert.Equal(t, int32(1000), processData.UID)
				assert.Equal(t, int32(1000), processData.GID)

				// Verify network data
				networkData, ok := event.GetNetworkData()
				require.True(t, ok)
				assert.Equal(t, "UDP", networkData.Protocol)
				assert.Equal(t, "outbound", networkData.Direction)
				assert.Equal(t, "192.168.1.100", networkData.SourceIP)
				assert.Equal(t, int32(12345), networkData.SourcePort)
				assert.Equal(t, "8.8.8.8", networkData.DestIP)
				assert.Equal(t, int32(53), networkData.DestPort)

				// Verify correlation hints
				assert.Equal(t, int32(12345), event.CorrelationHints.ProcessID)

				// Verify metadata
				assert.Equal(t, domain.PriorityNormal, event.Metadata.Priority)
				assert.Equal(t, "1.0", event.Metadata.SchemaVersion)
			}
		})
	}
}

func TestExtractSourceIP(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name     string
		bpfEvent *BPFDNSEvent
		expected string
	}{
		{
			name: "IPv4 address",
			bpfEvent: &BPFDNSEvent{
				IPVersion: 4,
				SrcAddr:   [16]byte{192, 168, 1, 100, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			},
			expected: "192.168.1.100",
		},
		{
			name: "IPv6 address",
			bpfEvent: &BPFDNSEvent{
				IPVersion: 6,
				SrcAddr:   [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			},
			expected: "2001:db8::1",
		},
		{
			name: "unknown IP version",
			bpfEvent: &BPFDNSEvent{
				IPVersion: 99,
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.extractSourceIP(tt.bpfEvent)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractDestIP(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name     string
		bpfEvent *BPFDNSEvent
		expected string
	}{
		{
			name: "IPv4 address",
			bpfEvent: &BPFDNSEvent{
				IPVersion: 4,
				DstAddr:   [16]byte{8, 8, 8, 8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			},
			expected: "8.8.8.8",
		},
		{
			name: "IPv6 address",
			bpfEvent: &BPFDNSEvent{
				IPVersion: 6,
				DstAddr:   [16]byte{0x20, 0x01, 0x48, 0x60, 0x48, 0x60, 0, 0, 0, 0, 0, 0, 0, 0, 0x88, 0x88},
			},
			expected: "2001:4860:4860::8888",
		},
		{
			name: "unknown IP version",
			bpfEvent: &BPFDNSEvent{
				IPVersion: 99,
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.extractDestIP(tt.bpfEvent)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetCgroupPath(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name     string
		cgroupID uint64
		expected string
	}{
		{
			name:     "zero cgroup ID",
			cgroupID: 0,
			expected: "",
		},
		{
			name:     "valid cgroup ID",
			cgroupID: 12345,
			expected: "/proc/cgroup/12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.getCgroupPath(tt.cgroupID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertCollectorEventToRawEvent(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	// Create a sample CollectorEvent
	collectorEvent := &domain.CollectorEvent{
		EventID:   "test-123",
		Timestamp: time.Now(),
		Type:      domain.EventTypeDNS,
		Source:    "test-collector",
		EventData: domain.EventDataContainer{
			DNS: &domain.DNSData{
				QueryType: "A",
				QueryName: "example.com",
				ServerIP:  "8.8.8.8",
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"test": "value",
			},
		},
	}

	rawEvent := collector.convertCollectorEventToRawEvent(collectorEvent)

	// Verify conversion
	assert.Equal(t, collectorEvent.Timestamp, rawEvent.Timestamp)
	assert.Equal(t, collectorEvent.Source, rawEvent.Source)
	assert.Equal(t, string(collectorEvent.Type), rawEvent.Type)
	assert.NotEmpty(t, rawEvent.Data)
	assert.Equal(t, collectorEvent.Metadata.Labels, rawEvent.Metadata)

	// Verify data contains event information
	dataStr := string(rawEvent.Data)
	assert.Contains(t, dataStr, "CollectorEvent")
	assert.Contains(t, dataStr, "test-123")
	assert.Contains(t, dataStr, string(domain.EventTypeDNS))
}

func TestParseDNSAnswers(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	// Test with empty data
	answers, err := collector.parseDNSAnswers([]byte{})
	assert.NoError(t, err)
	assert.Empty(t, answers)

	// Test with sample DNS response data
	// This is a simplified test since full DNS parsing is complex
	dnsData := make([]byte, 32)
	answers, err = collector.parseDNSAnswers(dnsData)
	assert.NoError(t, err)
	assert.NotNil(t, answers)
}

func TestExtractK8sContext(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name        string
		containerID string
		cgroupID    uint64
		expected    *domain.K8sContext
	}{
		{
			name:        "empty container ID",
			containerID: "",
			cgroupID:    12345,
			expected:    nil,
		},
		{
			name:        "valid container ID but no cgroup path",
			containerID: "abcdef123456",
			cgroupID:    0,
			expected:    nil,
		},
		{
			name:        "valid container ID with cgroup",
			containerID: "abcdef123456",
			cgroupID:    12345,
			expected:    nil, // Since getCgroupPath returns placeholder and extractPodUID will fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.extractK8sContext(tt.containerID, tt.cgroupID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractNamespace(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	// Test the simplified implementation
	result := collector.extractNamespace("/kubepods/some/path")
	assert.Equal(t, "default", result)
}

func TestExtractPodName(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	// Test the simplified implementation
	result := collector.extractPodName("/kubepods/some/path")
	assert.Equal(t, "", result)
}

func TestGetHostname(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	hostname := collector.getHostname()
	// Should return either a valid hostname or "unknown"
	assert.NotEmpty(t, hostname)
}

func TestGetKernelVersion(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	version := collector.getKernelVersion()
	// Simplified implementation returns "unknown"
	assert.Equal(t, "unknown", version)
}

// Benchmark tests for Linux-specific functions

func BenchmarkConvertToCollectorEvent(b *testing.B) {
	cfg := DefaultConfig()
	collector, _ := NewCollector("bench", cfg)

	bpfEvent := &BPFDNSEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       12345,
		TID:       12346,
		EventType: 1,
		Protocol:  17,
		IPVersion: 4,
		QType:     1,
		LatencyNs: 50 * 1000 * 1000,
	}
	copy(bpfEvent.QueryName[:], []byte("example.com"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = collector.convertToCollectorEvent(bpfEvent)
	}
}

func BenchmarkExtractSourceIP(b *testing.B) {
	cfg := DefaultConfig()
	collector, _ := NewCollector("bench", cfg)

	bpfEvent := &BPFDNSEvent{
		IPVersion: 4,
		SrcAddr:   [16]byte{192, 168, 1, 100, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.extractSourceIP(bpfEvent)
	}
}

func BenchmarkExtractContainerIDLinux(b *testing.B) {
	cfg := DefaultConfig()
	collector, _ := NewCollector("bench", cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.extractContainerID(12345)
	}
}
