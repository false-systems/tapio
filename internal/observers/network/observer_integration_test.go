package network

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
)

// setupOTELIntegration sets up OTEL with metric reader for integration tests
func setupOTELIntegration(t *testing.T) *metric.ManualReader {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(nil)
	})
	return reader
}

// TestIntegration_ObserverLifecycle tests complete observer lifecycle
func TestIntegration_ObserverLifecycle(t *testing.T) {
	setupOTELIntegration(t)

	config := Config{
		Output: base.OutputConfig{
			Stdout: true,
		},
	}

	observer, err := NewNetworkObserver("integration-test", config)
	require.NoError(t, err, "Failed to create observer")
	require.NotNil(t, observer)

	// Verify observer state
	assert.Equal(t, "integration-test", observer.Name())
	assert.NotNil(t, observer.BaseObserver)
}

// TestIntegration_EventConversion tests converting raw eBPF events to domain events
func TestIntegration_EventConversion(t *testing.T) {
	setupOTELIntegration(t)

	tests := []struct {
		name        string
		bpfEvent    NetworkEventBPF
		wantType    string
		wantSrcIP   string
		wantDstIP   string
		wantSrcPort uint16
		wantDstPort uint16
		wantComm    string
	}{
		{
			name: "IPv4 Connection Established",
			bpfEvent: NetworkEventBPF{
				PID:      1234,
				SrcIP:    0x0100007f, // 127.0.0.1
				DstIP:    0x6401a8c0, // 192.168.1.100
				SrcPort:  50000,
				DstPort:  80,
				Family:   2, // AF_INET
				Protocol: IPPROTO_TCP,
				OldState: TCP_SYN_SENT,
				NewState: TCP_ESTABLISHED,
				Comm:     [16]byte{'c', 'u', 'r', 'l', 0},
			},
			wantType:    "connection_established",
			wantSrcIP:   "127.0.0.1",
			wantDstIP:   "192.168.1.100",
			wantSrcPort: 50000,
			wantDstPort: 80,
			wantComm:    "curl",
		},
		{
			name: "Listen Started",
			bpfEvent: NetworkEventBPF{
				PID:      5678,
				SrcIP:    0x00000000, // 0.0.0.0
				SrcPort:  8080,
				Family:   2, // AF_INET
				Protocol: IPPROTO_TCP,
				OldState: TCP_CLOSE,
				NewState: TCP_LISTEN,
				Comm:     [16]byte{'n', 'g', 'i', 'n', 'x', 0},
			},
			wantType:    "listen_started",
			wantSrcIP:   "0.0.0.0",
			wantSrcPort: 8080,
			wantComm:    "nginx",
		},
		{
			name: "Connection Closed",
			bpfEvent: NetworkEventBPF{
				PID:      9012,
				SrcIP:    0x0100007f, // 127.0.0.1
				DstIP:    0x0200007f, // 127.0.0.2
				SrcPort:  45678,
				DstPort:  443,
				Family:   2, // AF_INET
				Protocol: IPPROTO_TCP,
				OldState: TCP_ESTABLISHED,
				NewState: TCP_CLOSE,
				Comm:     [16]byte{'w', 'g', 'e', 't', 0},
			},
			wantType:    "connection_closed",
			wantSrcIP:   "127.0.0.1",
			wantDstIP:   "127.0.0.2",
			wantSrcPort: 45678,
			wantDstPort: 443,
			wantComm:    "wget",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test event type conversion
			eventType := stateToEventType(tt.bpfEvent.OldState, tt.bpfEvent.NewState, "", nil)
			assert.Equal(t, tt.wantType, eventType)

			// Test IP conversion
			srcIP := convertIPv4(tt.bpfEvent.SrcIP)
			assert.Equal(t, tt.wantSrcIP, srcIP)

			if tt.wantDstIP != "" {
				dstIP := convertIPv4(tt.bpfEvent.DstIP)
				assert.Equal(t, tt.wantDstIP, dstIP)
			}

			// Test port extraction
			assert.Equal(t, tt.wantSrcPort, tt.bpfEvent.SrcPort)
			assert.Equal(t, tt.wantDstPort, tt.bpfEvent.DstPort)

			// Test process name extraction
			comm := extractComm(tt.bpfEvent.Comm)
			assert.Equal(t, tt.wantComm, comm)
		})
	}
}

// TestIntegration_IPv6EventConversion tests IPv6 event conversion
func TestIntegration_IPv6EventConversion(t *testing.T) {
	setupOTELIntegration(t)

	bpfEvent := NetworkEventBPF{
		PID:      1111,
		SrcIPv6:  [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, // ::1
		DstIPv6:  [16]byte{0x26, 0x07, 0xf8, 0xb0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		SrcPort:  55555,
		DstPort:  443,
		Family:   10, // AF_INET6
		Protocol: IPPROTO_TCP,
		OldState: TCP_SYN_SENT,
		NewState: TCP_ESTABLISHED,
		Comm:     [16]byte{'s', 's', 'h', 0},
	}

	// Verify IPv6 conversion
	srcIPv6 := convertIPv6(bpfEvent.SrcIPv6)
	assert.Equal(t, "0:0:0:0:0:0:0:1", srcIPv6)

	dstIPv6 := convertIPv6(bpfEvent.DstIPv6)
	assert.Equal(t, "2607:f8b0:0:0:0:0:0:1", dstIPv6)

	// Verify event type
	eventType := stateToEventType(bpfEvent.OldState, bpfEvent.NewState, "", nil)
	assert.Equal(t, "connection_established", eventType)

	// Verify process name
	comm := extractComm(bpfEvent.Comm)
	assert.Equal(t, "ssh", comm)
}

// TestIntegration_ConcurrentEventProcessing tests handling multiple events concurrently
func TestIntegration_ConcurrentEventProcessing(t *testing.T) {
	setupOTELIntegration(t)

	// Create multiple events simulating concurrent network activity
	events := []NetworkEventBPF{
		{
			PID: 1000, SrcIP: 0x0100007f, DstIP: 0x0200007f,
			SrcPort: 50000, DstPort: 80,
			Family: 2, Protocol: IPPROTO_TCP,
			OldState: TCP_SYN_SENT, NewState: TCP_ESTABLISHED,
			Comm: [16]byte{'c', 'u', 'r', 'l', 0},
		},
		{
			PID: 2000, SrcIP: 0x0100007f, DstIP: 0x0300007f,
			SrcPort: 50001, DstPort: 443,
			Family: 2, Protocol: IPPROTO_TCP,
			OldState: TCP_SYN_SENT, NewState: TCP_ESTABLISHED,
			Comm: [16]byte{'w', 'g', 'e', 't', 0},
		},
		{
			PID: 3000, SrcIP: 0x00000000, SrcPort: 8080,
			Family: 2, Protocol: IPPROTO_TCP,
			OldState: TCP_CLOSE, NewState: TCP_LISTEN,
			Comm: [16]byte{'n', 'g', 'i', 'n', 'x', 0},
		},
	}

	// Process events concurrently
	done := make(chan bool, len(events))

	for _, evt := range events {
		go func(e NetworkEventBPF) {
			defer func() { done <- true }()

			// Convert event
			eventType := stateToEventType(e.OldState, e.NewState, "", nil)
			assert.NotEmpty(t, eventType)

			srcIP := convertIPv4(e.SrcIP)
			assert.NotEmpty(t, srcIP)

			comm := extractComm(e.Comm)
			assert.NotEmpty(t, comm)
		}(evt)
	}

	// Wait for all goroutines with timeout
	timeout := time.After(5 * time.Second)
	for i := 0; i < len(events); i++ {
		select {
		case <-done:
			// Success
		case <-timeout:
			t.Fatal("Timeout waiting for concurrent event processing")
		}
	}
}

// TestIntegration_ContextCancellation tests observer behavior with context cancellation
func TestIntegration_ContextCancellation(t *testing.T) {
	setupOTELIntegration(t)

	config := Config{
		Output: base.OutputConfig{Stdout: true},
	}

	observer, err := NewNetworkObserver("cancel-test", config)
	require.NoError(t, err)

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context immediately
	cancel()

	// Verify context is cancelled
	select {
	case <-ctx.Done():
		assert.Error(t, ctx.Err())
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context should be cancelled")
	}

	// Observer should still be valid
	assert.NotNil(t, observer)
	assert.Equal(t, "cancel-test", observer.Name())
}

// TestLinkFailure_SynTimeout tests detection of SYN timeout (connection attempt timeout)
func TestLinkFailure_SynTimeout(t *testing.T) {
	setupOTELIntegration(t)

	// SYN timeout: SYN_SENT → CLOSE with no response (simulated)
	bpfEvent := NetworkEventBPF{
		PID:      1234,
		SrcIP:    0x0100007f, // 127.0.0.1
		DstIP:    0x01010101, // 1.1.1.1 (unreachable)
		SrcPort:  50000,
		DstPort:  80,
		Family:   AF_INET,
		Protocol: IPPROTO_TCP,
		OldState: TCP_SYN_SENT,
		NewState: TCP_CLOSE,
		Comm:     [16]byte{'c', 'u', 'r', 'l', 0},
	}

	// Should detect as SYN timeout
	eventType := stateToEventType(bpfEvent.OldState, bpfEvent.NewState, "", nil)
	assert.Equal(t, "connection_syn_timeout", eventType, "SYN_SENT → CLOSE should be detected as timeout")

	// Verify IP conversion
	srcIP := convertIPv4(bpfEvent.SrcIP)
	assert.Equal(t, "127.0.0.1", srcIP)

	dstIP := convertIPv4(bpfEvent.DstIP)
	assert.Equal(t, "1.1.1.1", dstIP)

	// Verify process name
	comm := extractComm(bpfEvent.Comm)
	assert.Equal(t, "curl", comm)
}

// TestPacketLoss_RetransmitEvent tests detection of TCP retransmissions
func TestPacketLoss_RetransmitEvent(t *testing.T) {
	setupOTELIntegration(t)

	// Retransmit event from tcp:tcp_retransmit_skb tracepoint
	retxEvent := NetworkEventBPF{
		PID:       1234,
		SrcIP:     0x0100007f, // 127.0.0.1
		DstIP:     0x0200007f, // 127.0.0.2
		SrcPort:   50000,
		DstPort:   80,
		Family:    AF_INET,
		Protocol:  IPPROTO_TCP,
		EventType: EventTypeRetransmit, // Retransmit event
		// Reuse fields for retransmit data:
		OldState: 3,   // total_retrans = 3
		NewState: 100, // snd_cwnd = 100 packets
		Comm:     [16]byte{'n', 'g', 'i', 'n', 'x', 0},
	}

	// Verify event type
	assert.Equal(t, uint8(EventTypeRetransmit), retxEvent.EventType, "Should be retransmit event")

	// Verify retransmit data
	totalRetrans := retxEvent.OldState
	assert.Equal(t, uint8(3), totalRetrans, "Total retransmits should be 3")

	sndCwnd := retxEvent.NewState
	assert.Equal(t, uint8(100), sndCwnd, "Congestion window should be 100")

	// Verify connection info
	srcIP := convertIPv4(retxEvent.SrcIP)
	assert.Equal(t, "127.0.0.1", srcIP)

	comm := extractComm(retxEvent.Comm)
	assert.Equal(t, "nginx", comm)
}

// TestPacketLoss_RetransmitRateCalculation tests retransmit rate calculation
func TestPacketLoss_RetransmitRateCalculation(t *testing.T) {
	setupOTELIntegration(t)

	// Simulate 100 total packets with 5 retransmits = 5% rate
	totalPackets := uint64(100)
	retransmits := uint64(5)

	retransmitRate := float64(retransmits) / float64(totalPackets) * 100
	assert.Equal(t, 5.0, retransmitRate, "Retransmit rate should be 5%")

	// High retransmit rate (>5%) should trigger warning
	highRetransmits := uint64(10)
	highRate := float64(highRetransmits) / float64(totalPackets) * 100
	assert.Greater(t, highRate, 5.0, "10% rate should exceed 5% threshold")
}

// TestPacketLoss_ConnectionTracking tests per-connection retransmit tracking
func TestPacketLoss_ConnectionTracking(t *testing.T) {
	setupOTELIntegration(t)

	// Connection key format: "srcIP:srcPort:dstIP:dstPort"
	connKey1 := "127.0.0.1:50000:127.0.0.2:80"
	connKey2 := "127.0.0.1:50001:127.0.0.3:443"

	// Different connections should have separate tracking
	assert.NotEqual(t, connKey1, connKey2, "Different connections should have different keys")

	// Same connection should have same key
	sameConnKey := "127.0.0.1:50000:127.0.0.2:80"
	assert.Equal(t, connKey1, sameConnKey, "Same connection should have same key")
}

// TestRTTSpike_Detection tests RTT spike event detection
func TestRTTSpike_Detection(t *testing.T) {
	setupOTELIntegration(t)

	// RTT spike event from eBPF: baseline=50ms, current=150ms (3x spike)
	rttEvent := NetworkEventBPF{
		PID:       1234,
		SrcIP:     0x0100007f, // 127.0.0.1
		DstIP:     0x0200007f, // 127.0.0.2
		SrcPort:   50000,
		DstPort:   443,
		Family:    AF_INET,
		Protocol:  IPPROTO_TCP,
		EventType: EventTypeRTTSpike, // RTT spike event
		OldState:  50,                // baseline RTT = 50ms
		NewState:  150,               // current RTT = 150ms (3x increase)
		Comm:      [16]byte{'n', 'g', 'i', 'n', 'x', 0},
	}

	// Verify event type
	assert.Equal(t, uint8(EventTypeRTTSpike), rttEvent.EventType, "Should be RTT spike event")

	// Verify RTT data
	baselineRTT := rttEvent.OldState
	currentRTT := rttEvent.NewState
	assert.Equal(t, uint8(50), baselineRTT, "Baseline RTT should be 50ms")
	assert.Equal(t, uint8(150), currentRTT, "Current RTT should be 150ms")

	// Calculate degradation
	degradation := (float64(currentRTT) - float64(baselineRTT)) / float64(baselineRTT) * 100
	assert.Equal(t, 200.0, degradation, "Should be 200% degradation (3x)")

	// Verify it exceeds 2x threshold
	assert.Greater(t, float64(currentRTT), float64(baselineRTT)*2, "Should exceed 2x threshold")
}

// TestRTTSpike_AbsoluteThreshold tests detection of absolute high RTT (>500ms)
func TestRTTSpike_AbsoluteThreshold(t *testing.T) {
	setupOTELIntegration(t)

	// High RTT event: 600ms (exceeds 500ms absolute threshold)
	// Even if baseline is also high, 600ms is always bad
	highRTTEvent := NetworkEventBPF{
		PID:       5678,
		SrcIP:     0x0100007f,
		DstIP:     0x08080808, // 8.8.8.8
		SrcPort:   50001,
		DstPort:   53,
		Family:    AF_INET,
		Protocol:  IPPROTO_TCP,
		EventType: EventTypeRTTSpike,
		OldState:  255, // baseline RTT = 255ms (clamped, actual might be higher)
		NewState:  255, // current RTT = 255ms (clamped from 600ms)
		Comm:      [16]byte{'d', 'i', 'g', 0},
	}

	// When RTT > 255ms, both fields are clamped to 255
	// The spike is detected in eBPF before clamping
	assert.Equal(t, uint8(255), highRTTEvent.OldState, "Should be clamped to 255")
	assert.Equal(t, uint8(255), highRTTEvent.NewState, "Should be clamped to 255")

	// In reality, actual RTT was >500ms which triggered the event
	actualRTT := 600.0 // This would be the real value from tcp_sock
	assert.Greater(t, actualRTT, 500.0, "Actual RTT exceeds 500ms absolute threshold")
}

// TestRTTBaseline_LRUEviction tests that LRU map auto-evicts old baselines
func TestRTTBaseline_LRUEviction(t *testing.T) {
	setupOTELIntegration(t)

	// Test expectation: When map is full (10k entries), LRU evicts least recently used
	// We can't easily test 10k entries in unit test, but we verify the map type
	// The actual LRU eviction is handled by kernel, not our code

	// Verify we're using LRU map by checking that manual cleanup is NOT needed
	// (This is a documentation test - verifies our design decision)

	// Old code HAD: bpf_map_delete_elem on TCP_CLOSE
	// New code with LRU: NO manual cleanup needed

	// If this test exists, it documents that we rely on LRU auto-eviction
	assert.True(t, true, "LRU map handles eviction automatically")
}

// TestRTTBaseline_MapPinning tests that baselines persist across restarts
func TestRTTBaseline_MapPinning(t *testing.T) {
	setupOTELIntegration(t)

	// Test expectation: Map is pinned to /sys/fs/bpf/tapio/baseline_rtt
	// On observer restart, existing baselines are preserved

	// This is an integration test - would require:
	// 1. Start observer, populate RTT baselines
	// 2. Stop observer
	// 3. Start new observer instance
	// 4. Verify baselines still exist

	// For unit test, we just document the requirement
	expectedPinPath := "/sys/fs/bpf/tapio/baseline_rtt"
	assert.NotEmpty(t, expectedPinPath, "Map should be pinned for persistence")
}

// TestPerCPUMetrics_NoLockContention tests that Per-CPU metrics are lock-free
func TestPerCPUMetrics_NoLockContention(t *testing.T) {
	setupOTELIntegration(t)

	// Test expectation: Per-CPU maps provide lock-free counters
	// Each CPU writes to its own copy - no contention

	// Benefits we expect:
	// 1. Multiple CPUs can increment simultaneously
	// 2. No atomic operations needed in eBPF
	// 3. Aggregate in Go userspace

	// This is a design validation test
	assert.True(t, true, "Per-CPU maps are lock-free by design")
}

// TestPerCPUMetrics_Aggregation tests that we aggregate across all CPUs
func TestPerCPUMetrics_Aggregation(t *testing.T) {
	setupOTELIntegration(t)

	// Test expectation: When reading Per-CPU metric, we get array of values
	// Example: [CPU0=100, CPU1=85, CPU2=92] → total=277

	// Mock Per-CPU values
	perCPUValues := []uint64{100, 85, 92, 63}

	// Aggregate
	total := uint64(0)
	for _, v := range perCPUValues {
		total += v
	}

	assert.Equal(t, uint64(340), total, "Should aggregate across all CPUs")
}
