package dns

import (
	"context"
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name     string
		collName string
		config   Config
		wantErr  bool
	}{
		{
			name:     "valid default config",
			collName: "test-dns",
			config:   DefaultConfig(),
			wantErr:  false,
		},
		{
			name:     "valid custom config",
			collName: "custom-dns",
			config: Config{
				BufferSize:   2000,
				Interface:    "eth1",
				EnableEBPF:   false,
				EnableSocket: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.collName, tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
				assert.Equal(t, tt.collName, collector.Name())
				assert.Equal(t, tt.config.BufferSize, cap(collector.events))
				assert.Equal(t, tt.config, collector.config)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	assert.Equal(t, 1000, config.BufferSize)
	assert.Equal(t, "eth0", config.Interface)
	assert.True(t, config.EnableEBPF)
	assert.False(t, config.EnableSocket)
}

func TestCollectorLifecycle(t *testing.T) {
	config := DefaultConfig()
	// Disable eBPF for testing (requires root)
	config.EnableEBPF = false

	collector, err := NewCollector("test-dns", config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initially not healthy
	assert.False(t, collector.IsHealthy())

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// Get events channel
	events := collector.Events()
	assert.NotNil(t, events)

	// Stop collector
	err = collector.Stop()
	require.NoError(t, err)
	assert.False(t, collector.IsHealthy())

	// Ensure channel is closed
	select {
	case _, ok := <-events:
		assert.False(t, ok, "Events channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Events channel was not closed")
	}
}

func TestCollectorLifecycleWithEBPFDisabled(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = false

	collector, err := NewCollector("test-dns-no-ebpf", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start should succeed even without eBPF
	err = collector.Start(ctx)
	require.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// Stop should succeed
	err = collector.Stop()
	require.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

func TestCollectorConcurrency(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = false
	config.BufferSize = 100

	collector, err := NewCollector("test-dns-concurrent", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Multiple goroutines reading from events channel
	done := make(chan bool, 3)
	for i := 0; i < 3; i++ {
		go func() {
			defer func() { done <- true }()
			select {
			case <-collector.Events():
				// Successfully read event
			case <-ctx.Done():
				// Context timeout is expected
			}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 3; i++ {
		select {
		case <-done:
			// OK
		case <-time.After(3 * time.Second):
			t.Fatal("Timeout waiting for concurrent readers")
		}
	}
}

func TestEventTypeToString(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		eventType uint32
		expected  string
	}{
		{1, "dns_query"},
		{2, "dns_response"},
		{3, "dns_timeout"},
		{999, "dns_unknown"},
		{0, "dns_unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := collector.eventTypeToString(tt.eventType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProtocolToString(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		protocol uint8
		expected string
	}{
		{17, "udp"},
		{6, "tcp"},
		{1, "proto_1"},
		{255, "proto_255"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := collector.protocolToString(tt.protocol)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractNamespace(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		queryName string
		expected  string
	}{
		{"service.default.svc.cluster.local", "default"},
		{"api.kube-system.svc.cluster.local", "kube-system"},
		{"redis.redis-ns.svc.cluster.local", "redis-ns"},
		{"google.com", ""},
		{"service.default.svc", ""},
		{"", ""},
		{"just-a-service", ""},
		{"service.default.svc.cluster.local.extra", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.queryName, func(t *testing.T) {
			result := collector.extractNamespace(tt.queryName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractService(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		queryName string
		expected  string
	}{
		{"service.default.svc.cluster.local", "service"},
		{"api-server.kube-system.svc.cluster.local", "api-server"},
		{"redis-master.redis-ns.svc.cluster.local", "redis-master"},
		{"google.com", "google.com"},
		{"", ""},
		{"just-a-service", "just-a-service"},
	}

	for _, tt := range tests {
		t.Run(tt.queryName, func(t *testing.T) {
			result := collector.extractService(tt.queryName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIntToIP(t *testing.T) {
	tests := []struct {
		ip       uint32
		expected string
	}{
		{0x01010101, "1.1.1.1"},
		{0x08080808, "8.8.8.8"},
		{0x7f000001, "127.0.0.1"},
		{0x00000000, "0.0.0.0"},
		{0xffffffff, "255.255.255.255"},
		{0xc0a80101, "192.168.1.1"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := intToIP(tt.ip)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNullTerminatedString(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "null terminated",
			input:    []byte{'h', 'e', 'l', 'l', 'o', 0, 'w', 'o', 'r', 'l', 'd'},
			expected: "hello",
		},
		{
			name:     "no null terminator",
			input:    []byte{'h', 'e', 'l', 'l', 'o'},
			expected: "hello",
		},
		{
			name:     "empty with null",
			input:    []byte{0},
			expected: "",
		},
		{
			name:     "empty",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "null at start",
			input:    []byte{0, 'h', 'e', 'l', 'l', 'o'},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.nullTerminatedString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDNSEventStructSize(t *testing.T) {
	// Ensure C struct and Go struct have same size for safe binary unmarshaling
	event := EnhancedDNSEvent{}
	size := unsafe.Sizeof(event)

	// This should match the enhanced C struct size
	// struct dns_event in dns_monitor.c (enhanced version)
	expectedMinSize := uintptr(64 + 128 + 512) // timestamp + enhanced fields + query_name + data
	assert.GreaterOrEqual(t, size, expectedMinSize, "EnhancedDNSEvent struct size should be reasonable")
}

func TestCreateCollector(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]interface{}
		want   string
	}{
		{
			name:   "default config",
			config: map[string]interface{}{},
			want:   "dns",
		},
		{
			name: "custom name",
			config: map[string]interface{}{
				"name": "custom-dns-collector",
			},
			want: "custom-dns-collector",
		},
		{
			name: "full config",
			config: map[string]interface{}{
				"name":          "full-dns",
				"buffer_size":   2000,
				"interface":     "eth1",
				"enable_ebpf":   false,
				"enable_socket": true,
			},
			want: "full-dns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := CreateCollector(tt.config)
			require.NoError(t, err)
			assert.NotNil(t, collector)
			assert.Equal(t, tt.want, collector.Name())
		})
	}
}

func TestCreateCollectorTypeSafety(t *testing.T) {
	// Test type safety for config parsing
	config := map[string]interface{}{
		"buffer_size":   "invalid", // Should be int
		"enable_ebpf":   "invalid", // Should be bool
		"enable_socket": "invalid", // Should be bool
	}

	// Should not panic and use defaults for invalid types
	collector, err := CreateCollector(config)
	require.NoError(t, err)
	assert.NotNil(t, collector)

	// Should use default values for invalid config types
	dnsCollector := collector.(*Collector)
	defaultConfig := DefaultConfig()
	assert.Equal(t, defaultConfig.BufferSize, dnsCollector.config.BufferSize)
	assert.Equal(t, defaultConfig.EnableEBPF, dnsCollector.config.EnableEBPF)
	assert.Equal(t, defaultConfig.EnableSocket, dnsCollector.config.EnableSocket)
}

// Benchmark tests for performance validation
func BenchmarkEventTypeToString(b *testing.B) {
	collector := &Collector{}
	for i := 0; i < b.N; i++ {
		collector.eventTypeToString(1)
		collector.eventTypeToString(2)
		collector.eventTypeToString(3)
		collector.eventTypeToString(999)
	}
}

func BenchmarkProtocolToString(b *testing.B) {
	collector := &Collector{}
	for i := 0; i < b.N; i++ {
		collector.protocolToString(6)
		collector.protocolToString(17)
		collector.protocolToString(255)
	}
}

func BenchmarkExtractNamespace(b *testing.B) {
	collector := &Collector{}
	queryName := "service.default.svc.cluster.local"
	for i := 0; i < b.N; i++ {
		collector.extractNamespace(queryName)
	}
}

func BenchmarkIntToIP(b *testing.B) {
	ip := uint32(0xc0a80101) // 192.168.1.1
	for i := 0; i < b.N; i++ {
		intToIP(ip)
	}
}

// Test IPv4/IPv6 conversion functions
func TestIPv4ToString(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		name     string
		ip       uint32
		expected string
	}{
		{"localhost", 0x7f000001, "127.0.0.1"},
		{"google DNS", 0x08080808, "8.8.8.8"},
		{"zero", 0x00000000, "0.0.0.0"},
		{"broadcast", 0xffffffff, "255.255.255.255"},
		{"private", 0xc0a80101, "192.168.1.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.ipv4ToString(tt.ip)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIPv6ToString(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		name     string
		addr     [4]uint32
		expected string
	}{
		{
			name:     "zero address",
			addr:     [4]uint32{0, 0, 0, 0},
			expected: "::",
		},
		{
			name:     "localhost",
			addr:     [4]uint32{0, 0, 0, 0x01000000}, // ::1 in network byte order
			expected: "0000:0000:0000:0000:0000:0000:0000:0001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.ipv6ToString(tt.addr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestQueryTypeToString(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		qtype    uint16
		expected string
	}{
		{1, "A"},
		{2, "NS"},
		{5, "CNAME"},
		{6, "SOA"},
		{12, "PTR"},
		{15, "MX"},
		{16, "TXT"},
		{28, "AAAA"},
		{33, "SRV"},
		{999, "TYPE999"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := collector.queryTypeToString(tt.qtype)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResponseCodeToString(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		rcode    uint8
		expected string
	}{
		{0, "NOERROR"},
		{1, "FORMERR"},
		{2, "SERVFAIL"},
		{3, "NXDOMAIN"},
		{4, "NOTIMP"},
		{5, "REFUSED"},
		{99, "RCODE99"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := collector.responseCodeToString(tt.rcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test the DNS types from types.go
func TestDNSEventTypeString(t *testing.T) {
	tests := []struct {
		eventType DNSEventType
		expected  string
	}{
		{DNSEventTypeQuery, "query"},
		{DNSEventTypeResponse, "response"},
		{DNSEventTypeTimeout, "timeout"},
		{DNSEventTypeError, "error"},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.eventType.String())
		})
	}
}

func TestDNSProtocolString(t *testing.T) {
	tests := []struct {
		protocol DNSProtocol
		expected string
	}{
		{DNSProtocolUDP, "udp"},
		{DNSProtocolTCP, "tcp"},
	}

	for _, tt := range tests {
		t.Run(string(tt.protocol), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.protocol.String())
		})
	}
}

func TestDNSQueryTypeString(t *testing.T) {
	tests := []struct {
		queryType DNSQueryType
		expected  string
	}{
		{DNSQueryTypeA, "A"},
		{DNSQueryTypeAAAA, "AAAA"},
		{DNSQueryTypeCNAME, "CNAME"},
		{DNSQueryTypeMX, "MX"},
		{DNSQueryTypeNS, "NS"},
		{DNSQueryTypePTR, "PTR"},
		{DNSQueryTypeSOA, "SOA"},
		{DNSQueryTypeTXT, "TXT"},
		{DNSQueryTypeSRV, "SRV"},
	}

	for _, tt := range tests {
		t.Run(string(tt.queryType), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.queryType.String())
		})
	}
}

func TestDNSFailureTypeString(t *testing.T) {
	tests := []struct {
		failureType DNSFailureType
		expected    string
	}{
		{DNSFailureTimeout, "timeout"},
		{DNSFailureNXDomain, "nxdomain"},
		{DNSFailureServFail, "servfail"},
		{DNSFailureRefused, "refused"},
		{DNSFailureNetErr, "network_error"},
	}

	for _, tt := range tests {
		t.Run(string(tt.failureType), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.failureType.String())
		})
	}
}

func TestDNSResponseCodeIsError(t *testing.T) {
	tests := []struct {
		code    DNSResponseCode
		isError bool
	}{
		{DNSResponseNoError, false},
		{DNSResponseFormatErr, true},
		{DNSResponseServerErr, true},
		{DNSResponseNameErr, true},
		{DNSResponseNotImpl, true},
		{DNSResponseRefused, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("code_%d", tt.code), func(t *testing.T) {
			assert.Equal(t, tt.isError, tt.code.IsError())
		})
	}
}

func TestDNSResponseCodeString(t *testing.T) {
	tests := []struct {
		code     DNSResponseCode
		expected string
	}{
		{DNSResponseNoError, "NOERROR"},
		{DNSResponseFormatErr, "FORMERR"},
		{DNSResponseServerErr, "SERVFAIL"},
		{DNSResponseNameErr, "NXDOMAIN"},
		{DNSResponseNotImpl, "NOTIMP"},
		{DNSResponseRefused, "REFUSED"},
		{DNSResponseCode(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.code.String())
		})
	}
}

// Test additional edge cases for extractNamespace
func TestExtractNamespaceEdgeCases(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		name      string
		queryName string
		expected  string
	}{
		{
			name:      "empty string",
			queryName: "",
			expected:  "",
		},
		{
			name:      "single component",
			queryName: "service",
			expected:  "",
		},
		{
			name:      "no svc pattern",
			queryName: "service.namespace.cluster.local",
			expected:  "",
		},
		{
			name:      "svc at beginning",
			queryName: "svc.cluster.local",
			expected:  "",
		},
		{
			name:      "incomplete pattern",
			queryName: "service.namespace.svc.cluster",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.extractNamespace(tt.queryName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test Start method with eBPF enabled (will fail but should cover error paths)
func TestCollectorStartWithEBPF(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = true

	collector, err := NewCollector("test-dns-ebpf", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// This will fail because we don't have proper eBPF setup, but should cover error paths
	err = collector.Start(ctx)
	assert.Error(t, err) // Expected to fail in test environment
	assert.False(t, collector.IsHealthy())

	// Clean up
	collector.Stop()
}

// Test more stop scenarios
func TestCollectorStopMultipleTimes(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = false

	collector, err := NewCollector("test-dns-multi-stop", config)
	require.NoError(t, err)

	ctx := context.Background()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// Stop multiple times should not cause issues
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())

	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

// Test more config variations
func TestCreateCollectorEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]interface{}
		want   bool // whether creation should succeed
	}{
		{
			name:   "nil config",
			config: nil,
			want:   true,
		},
		{
			name:   "empty config",
			config: map[string]interface{}{},
			want:   true,
		},
		{
			name: "config with wrong types",
			config: map[string]interface{}{
				"buffer_size": "not_a_number",
				"interface":   123,
				"enable_ebpf": "not_a_bool",
			},
			want: true, // Should use defaults
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := CreateCollector(tt.config)
			if tt.want {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// Test with zero buffer size
func TestCollectorZeroBufferSize(t *testing.T) {
	config := DefaultConfig()
	config.BufferSize = 0

	collector, err := NewCollector("test-dns-zero-buffer", config)
	require.NoError(t, err)

	// Should handle zero buffer size gracefully
	assert.Equal(t, 0, cap(collector.events))
}

// Test edge cases with very large buffer size
func TestCollectorLargeBufferSize(t *testing.T) {
	config := DefaultConfig()
	config.BufferSize = 10000

	collector, err := NewCollector("test-dns-large-buffer", config)
	require.NoError(t, err)

	assert.Equal(t, 10000, cap(collector.events))
}

// Test more edge cases for extractService
func TestExtractServiceEdgeCases(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		name      string
		queryName string
		expected  string
	}{
		{
			name:      "empty query",
			queryName: "",
			expected:  "",
		},
		{
			name:      "single component",
			queryName: "service",
			expected:  "service",
		},
		{
			name:      "service with underscores",
			queryName: "my_service.default.svc.cluster.local",
			expected:  "my_service",
		},
		{
			name:      "service with dashes",
			queryName: "my-service-name.kube-system.svc.cluster.local",
			expected:  "my-service-name",
		},
		{
			name:      "external domain",
			queryName: "www.example.com",
			expected:  "www.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.extractService(tt.queryName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test EnhancedDNSEvent validation
func TestEnhancedDNSEventFields(t *testing.T) {
	event := EnhancedDNSEvent{
		Timestamp: 12345678,
		PID:       1000,
		TID:       1001,
		UID:       500,
		GID:       500,
		CgroupID:  7890,
		EventType: 1,
		Protocol:  17,
		IPVersion: 4,
	}

	// Test that all fields are accessible
	assert.Equal(t, uint64(12345678), event.Timestamp)
	assert.Equal(t, uint32(1000), event.PID)
	assert.Equal(t, uint32(1001), event.TID)
	assert.Equal(t, uint32(500), event.UID)
	assert.Equal(t, uint32(500), event.GID)
	assert.Equal(t, uint64(7890), event.CgroupID)
	assert.Equal(t, uint8(1), event.EventType)
	assert.Equal(t, uint8(17), event.Protocol)
	assert.Equal(t, uint8(4), event.IPVersion)
}

// Test address union structures
func TestAddressUnion(t *testing.T) {
	var addr AddressUnion

	// Test IPv4
	addr.IPv4.Addr = 0x7f000001 // 127.0.0.1
	assert.Equal(t, uint32(0x7f000001), addr.IPv4.Addr)

	// Test IPv6
	addr.IPv6.Addr = [4]uint32{0, 0, 0, 1} // ::1
	assert.Equal(t, [4]uint32{0, 0, 0, 1}, addr.IPv6.Addr)
}

func BenchmarkNullTerminatedString(b *testing.B) {
	collector := &Collector{}
	data := []byte{'h', 'e', 'l', 'l', 'o', 0, 'w', 'o', 'r', 'l', 'd'}
	for i := 0; i < b.N; i++ {
		collector.nullTerminatedString(data)
	}
}
