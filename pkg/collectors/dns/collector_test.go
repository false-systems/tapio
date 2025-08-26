package dns

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				Name:       "test-dns",
				BufferSize: 1000,
				EnableEBPF: false, // disable for unit tests
			},
			wantErr: false,
		},
		{
			name:    "default config",
			cfg:     DefaultConfig(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.name, tt.cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, collector)
			} else {
				require.NoError(t, err)
				require.NotNil(t, collector)

				// Verify collector properties
				assert.Equal(t, tt.name, collector.Name())
				assert.NotNil(t, collector.logger)
				assert.NotNil(t, collector.tracer)
				assert.NotNil(t, collector.stats)
				assert.Equal(t, tt.cfg.BufferSize, cap(collector.events))

				// Verify OTEL metrics are initialized
				assert.NotNil(t, collector.eventsProcessed)
				assert.NotNil(t, collector.errorsTotal)
				assert.NotNil(t, collector.processingTime)
				assert.NotNil(t, collector.bufferUsage)
				assert.NotNil(t, collector.droppedEvents)
			}
		})
	}
}

func TestCollectorInterfaces(t *testing.T) {
	cfg := Config{
		Name:       "test-dns",
		BufferSize: 100,
		EnableEBPF: false,
	}

	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	// Test that collector implements required interfaces
	var _ domain.Collector = collector
	var _ domain.CollectorWithStats = collector
	var _ domain.HealthChecker = collector
}

func TestCollectorLifecycle(t *testing.T) {
	cfg := Config{
		Name:       "test-dns",
		BufferSize: 100,
		EnableEBPF: false, // disable eBPF for unit tests
	}

	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	// Initially not healthy
	assert.False(t, collector.IsHealthy())

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Should be healthy after start
	assert.True(t, collector.IsHealthy())

	// Test events channel
	assert.NotNil(t, collector.Events())

	// Stop collector
	err = collector.Stop()
	require.NoError(t, err)

	// Should not be healthy after stop
	assert.False(t, collector.IsHealthy())
}

func TestCollectorStatistics(t *testing.T) {
	cfg := Config{
		Name:       "test-dns",
		BufferSize: 100,
		EnableEBPF: false,
	}

	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	// Get initial statistics
	stats := collector.Statistics()
	require.NotNil(t, stats)

	// Get DNS-specific stats
	dnsStats := collector.GetDNSStats()
	require.NotNil(t, dnsStats)

	// Verify initial state
	assert.Equal(t, int64(0), stats.EventsProcessed)
	assert.Equal(t, int64(0), stats.ErrorCount)
	assert.Equal(t, int64(0), dnsStats.EventsDropped)
	assert.Equal(t, 0.0, dnsStats.BufferUtilization)
	assert.False(t, dnsStats.EBPFAttached)

	// Update stats and verify
	collector.updateStats(10, 2, 1)
	stats = collector.Statistics()
	assert.Equal(t, int64(10), stats.EventsProcessed)
	assert.Equal(t, int64(1), stats.ErrorCount)

	dnsStats = collector.GetDNSStats()
	assert.Equal(t, int64(2), dnsStats.EventsDropped)
}

func TestCollectorHealth(t *testing.T) {
	cfg := Config{
		Name:       "test-dns",
		BufferSize: 100,
		EnableEBPF: false,
	}

	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	// Test health when not started
	health := collector.Health()
	require.NotNil(t, health)
	assert.Equal(t, domain.HealthUnhealthy, health.Status)
	assert.Contains(t, health.Message, "not running")
	assert.Equal(t, "test", health.Component)

	// Start collector
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Test health when running
	health = collector.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)
	assert.Contains(t, health.Message, "actively monitoring")

	// Test degraded health with high buffer utilization
	// Fill buffer to 95%
	eventsChannel := collector.events
	for i := 0; i < 95; i++ {
		select {
		case eventsChannel <- &domain.CollectorEvent{
			EventID:   fmt.Sprintf("test-%d", i),
			Timestamp: time.Now(),
			Source:    "test",
			Type:      domain.EventTypeDNS,
			EventData: domain.EventDataContainer{
				DNS: &domain.DNSData{
					QueryName: "test.example.com",
				},
			},
		}:
		default:
			break
		}
	}

	health = collector.Health()
	assert.Equal(t, domain.HealthDegraded, health.Status)
	assert.Contains(t, health.Message, "high buffer utilization")

	// Cleanup
	collector.Stop()
}

func TestConvertQueryType(t *testing.T) {
	cfg := Config{
		Name:       "test-dns",
		BufferSize: 100,
		EnableEBPF: false,
	}

	_, err := NewCollector("test", cfg)
	require.NoError(t, err)

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
		{999, "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			// Skip this test - convertQueryType is now platform-specific
			t.Skip("convertQueryType is now platform-specific (Linux only)")
		})
	}
}

func TestExtractContainerID(t *testing.T) {
	cfg := Config{
		Name:       "test-dns",
		BufferSize: 100,
		EnableEBPF: false,
	}

	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name      string
		cgroupID  uint64
		setupMock func(*Collector)
		expected  string
	}{
		{
			name:     "zero cgroup ID",
			cgroupID: 0,
			expected: "",
		},
		{
			name:     "non-zero cgroup ID but no container",
			cgroupID: 12345,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupMock != nil {
				tt.setupMock(collector)
			}

			result := collector.extractContainerID(tt.cgroupID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsHexString(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"abc123", true},
		{"ABC123", true},
		{"0123456789abcdef", true},
		{"xyz123", false},
		{"123g456", false},
		{"", true}, // empty string is valid hex
		{"ABCDEF0123456789", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isHexString(tt.input)
			assert.Equal(t, tt.expected, result, "isHexString(%q) = %v, want %v", tt.input, result, tt.expected)
		})
	}
}

func TestCalculateEventPriority(t *testing.T) {
	cfg := Config{
		Name:       "test-dns",
		BufferSize: 100,
		EnableEBPF: false,
	}

	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name     string
		bpfEvent *BPFDNSEvent
		expected domain.EventPriority
	}{
		{
			name: "normal query",
			bpfEvent: &BPFDNSEvent{
				Rcode:     0,
				LatencyNs: 50 * 1000 * 1000, // 50ms
			},
			expected: domain.PriorityNormal,
		},
		{
			name: "failed query",
			bpfEvent: &BPFDNSEvent{
				Rcode:     3, // NXDOMAIN
				LatencyNs: 50 * 1000 * 1000,
			},
			expected: domain.PriorityHigh,
		},
		{
			name: "slow query",
			bpfEvent: &BPFDNSEvent{
				Rcode:     0,
				LatencyNs: 150 * 1000 * 1000, // 150ms
			},
			expected: domain.PriorityHigh,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.calculateEventPriority(tt.bpfEvent)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPodUID(t *testing.T) {
	cfg := Config{
		Name:       "test-dns",
		BufferSize: 100,
		EnableEBPF: false,
	}

	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name       string
		cgroupPath string
		expected   string
	}{
		{
			name:       "valid pod UID",
			cgroupPath: "/kubepods/pod12345678_1234_1234_1234_123456789012/container",
			expected:   "12345678-1234-1234-1234-123456789012",
		},
		{
			name:       "no pod in path",
			cgroupPath: "/system.slice/docker.service",
			expected:   "",
		},
		{
			name:       "empty path",
			cgroupPath: "",
			expected:   "",
		},
		{
			name:       "invalid pod UID format",
			cgroupPath: "/kubepods/podshort/container",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.extractPodUID(tt.cgroupPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "dns", cfg.Name)
	assert.Equal(t, 10000, cfg.BufferSize)
	assert.True(t, cfg.EnableEBPF)
}

func TestCollectorName(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test-collector", cfg)
	require.NoError(t, err)

	assert.Equal(t, "test-collector", collector.Name())
}

// Benchmark tests for performance-critical functions

// BenchmarkConvertQueryType is now Linux-specific
// func BenchmarkConvertQueryType(b *testing.B) {
// 	cfg := DefaultConfig()
// 	collector, _ := NewCollector("bench", cfg)
//
// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		collector.convertQueryType(1) // A record
// 	}
// }

func BenchmarkIsHexString(b *testing.B) {
	testString := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isHexString(testString)
	}
}

func BenchmarkExtractContainerID(b *testing.B) {
	cfg := DefaultConfig()
	collector, _ := NewCollector("bench", cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.extractContainerID(12345)
	}
}

// Additional tests to improve coverage

func TestDNSTypes(t *testing.T) {
	// Test DNS event type strings
	assert.Equal(t, "query", DNSEventTypeQuery.String())
	assert.Equal(t, "response", DNSEventTypeResponse.String())
	assert.Equal(t, "timeout", DNSEventTypeTimeout.String())
	assert.Equal(t, "error", DNSEventTypeError.String())

	// Test DNS protocol strings
	assert.Equal(t, "udp", DNSProtocolUDP.String())
	assert.Equal(t, "tcp", DNSProtocolTCP.String())

	// Test DNS query type strings
	assert.Equal(t, "A", DNSQueryTypeA.String())
	assert.Equal(t, "AAAA", DNSQueryTypeAAAA.String())
	assert.Equal(t, "CNAME", DNSQueryTypeCNAME.String())

	// Test DNS failure type strings
	assert.Equal(t, "timeout", DNSFailureTimeout.String())
	assert.Equal(t, "nxdomain", DNSFailureNXDomain.String())
	assert.Equal(t, "servfail", DNSFailureServFail.String())

	// Test DNS response codes
	assert.False(t, DNSResponseNoError.IsError())
	assert.True(t, DNSResponseFormatErr.IsError())
	assert.True(t, DNSResponseServerErr.IsError())
	assert.True(t, DNSResponseNameErr.IsError())

	// Test response code strings
	assert.Equal(t, "NOERROR", DNSResponseNoError.String())
	assert.Equal(t, "FORMERR", DNSResponseFormatErr.String())
	assert.Equal(t, "SERVFAIL", DNSResponseServerErr.String())
	assert.Equal(t, "NXDOMAIN", DNSResponseNameErr.String())
	assert.Equal(t, "NOTIMP", DNSResponseNotImpl.String())
	assert.Equal(t, "REFUSED", DNSResponseRefused.String())
	assert.Equal(t, "UNKNOWN", DNSResponseCode(999).String())
}

func TestCollectorEBPFStubs(t *testing.T) {
	// Test eBPF stub methods for coverage (they should not fail)
	cfg := Config{
		Name:       "stub-test",
		BufferSize: 100,
		EnableEBPF: true, // enable to test eBPF path
	}

	collector, err := NewCollector("stub-test", cfg)
	require.NoError(t, err)

	// Test stub eBPF methods (they shouldn't crash)
	err = collector.startEBPF()
	assert.NoError(t, err) // Stubs should not return errors

	collector.stopEBPF() // Should not panic

	// Can't easily test readEBPFEvents without complex setup
}

func TestExtractContainerIDEdgeCases(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	// Test with some hypothetical cgroup paths
	// Since getCgroupPath returns a placeholder, test the parsing logic
	result := collector.extractContainerID(99999)
	assert.Equal(t, "", result) // Should be empty for placeholder path
}

func TestGetCgroupPath(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	path := collector.getCgroupPath(12345)
	assert.Equal(t, "/proc/cgroup/12345", path)

	path = collector.getCgroupPath(0)
	assert.Equal(t, "", path)
}
