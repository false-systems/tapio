//go:build linux

package network

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/intelligence"
)

// setupNegativeTest is a no-op now that we use Prometheus metrics directly
func setupNegativeTest(t *testing.T) {
	t.Helper()
	// No-op: observer uses Prometheus metrics directly via base.Deps
}

// TestNegative_StateToEventType_InvalidStates tests behavior with invalid TCP states
func TestNegative_StateToEventType_InvalidStates(t *testing.T) {
	tests := []struct {
		name     string
		oldState uint8
		newState uint8
		want     string
	}{
		{
			name:     "Both states invalid",
			oldState: 255,
			newState: 255,
			want:     "tcp_state_change",
		},
		{
			name:     "Invalid old state",
			oldState: 200,
			newState: TCP_ESTABLISHED,
			want:     "connection_established",
		},
		{
			name:     "Invalid new state",
			oldState: TCP_SYN_SENT,
			newState: 100,
			want:     "tcp_state_change",
		},
		{
			name:     "Same state",
			oldState: TCP_ESTABLISHED,
			newState: TCP_ESTABLISHED,
			want:     "connection_established",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stateToEventType(tt.oldState, tt.newState, "", nil)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestNegative_ConvertIPv4_EdgeCases tests IPv4 conversion edge cases
func TestNegative_ConvertIPv4_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		ip   uint32
		want string
	}{
		{
			name: "Zero IP",
			ip:   0x00000000,
			want: "0.0.0.0",
		},
		{
			name: "Max IP",
			ip:   0xFFFFFFFF,
			want: "255.255.255.255",
		},
		{
			name: "Broadcast",
			ip:   0xFFFFFFFF,
			want: "255.255.255.255",
		},
		{
			name: "One",
			ip:   0x01000000,
			want: "0.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertIPv4(tt.ip)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestNegative_ConvertIPv6_EdgeCases tests IPv6 conversion edge cases
func TestNegative_ConvertIPv6_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		ip   [16]byte
		want string
	}{
		{
			name: "All zeros",
			ip:   [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			want: "0:0:0:0:0:0:0:0",
		},
		{
			name: "All ones",
			ip:   [16]byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255},
			want: "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
		},
		{
			name: "Mixed values",
			ip:   [16]byte{0, 1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0, 7, 0, 8},
			want: "1:2:3:4:5:6:7:8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertIPv6(tt.ip)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestNegative_ExtractComm_EdgeCases tests process name extraction edge cases
func TestNegative_ExtractComm_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		comm [16]byte
		want string
	}{
		{
			name: "Empty array",
			comm: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			want: "",
		},
		{
			name: "No null terminator",
			comm: [16]byte{'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p'},
			want: "abcdefghijklmnop",
		},
		{
			name: "Null at start",
			comm: [16]byte{0, 'a', 'b', 'c'},
			want: "",
		},
		{
			name: "Single character",
			comm: [16]byte{'a', 0},
			want: "a",
		},
		{
			name: "Special characters",
			comm: [16]byte{'-', '_', '.', 0},
			want: "-_.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractComm(tt.comm)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestNegative_NetworkEventBPF_ZeroValues tests struct with all zero values
func TestNegative_NetworkEventBPF_ZeroValues(t *testing.T) {
	var evt NetworkEventBPF

	// Verify zero values are handled correctly
	assert.Equal(t, uint32(0), evt.PID)
	assert.Equal(t, uint32(0), evt.SrcIP)
	assert.Equal(t, uint32(0), evt.DstIP)
	assert.Equal(t, uint16(0), evt.SrcPort)
	assert.Equal(t, uint16(0), evt.DstPort)
	assert.Equal(t, uint16(0), evt.Family)
	assert.Equal(t, uint8(0), evt.Protocol)
	assert.Equal(t, uint8(0), evt.OldState)
	assert.Equal(t, uint8(0), evt.NewState)

	// Process zero-value event
	eventType := stateToEventType(evt.OldState, evt.NewState, "", nil)
	assert.Equal(t, "tcp_state_change", eventType)

	srcIP := convertIPv4(evt.SrcIP)
	assert.Equal(t, "0.0.0.0", srcIP)

	comm := extractComm(evt.Comm)
	assert.Equal(t, "", comm)
}

// TestNegative_New_ZeroConfig tests observer creation with zero config
func TestNegative_New_ZeroConfig(t *testing.T) {
	reg := prometheus.NewRegistry()
	emitter, err := intelligence.New(intelligence.Config{Tier: intelligence.TierDebug})
	require.NoError(t, err)
	deps := base.NewDeps(reg, emitter)

	// Zero config should work (defaults applied internally)
	observer := New(Config{}, deps)
	require.NotNil(t, observer)
	assert.Equal(t, "network", observer.name)
}

// TestNegative_EventConversion_MalformedData tests handling of malformed event data
func TestNegative_EventConversion_MalformedData(t *testing.T) {
	setupNegativeTest(t)

	tests := []struct {
		name     string
		bpfEvent NetworkEventBPF
	}{
		{
			name: "Invalid protocol",
			bpfEvent: NetworkEventBPF{
				Protocol: 255,
				Family:   2,
			},
		},
		{
			name: "Invalid family",
			bpfEvent: NetworkEventBPF{
				Protocol: IPPROTO_TCP,
				Family:   99,
			},
		},
		{
			name: "Mismatched family and IP data",
			bpfEvent: NetworkEventBPF{
				Family: 2, // AF_INET (IPv4)
				SrcIP:  0, // But no IPv4 data
				// IPv6 data populated instead
				SrcIPv6: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			},
		},
		{
			name: "Port zero",
			bpfEvent: NetworkEventBPF{
				SrcIP:    0x0100007f,
				SrcPort:  0,
				DstPort:  0,
				Protocol: IPPROTO_TCP,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// These should not panic, just handle gracefully
			eventType := stateToEventType(tt.bpfEvent.OldState, tt.bpfEvent.NewState, "", nil)
			assert.NotEmpty(t, eventType)

			srcIP := ""
			if tt.bpfEvent.Family == 2 {
				srcIP = convertIPv4(tt.bpfEvent.SrcIP)
				assert.NotEmpty(t, srcIP)
			}

			comm := extractComm(tt.bpfEvent.Comm)
			// Ignore: prevent compiler optimization
			_, _ = srcIP, comm // Ignore: prevent compiler optimization
		})
	}
}

// TestNegative_TCPStateTransitions_Invalid tests invalid TCP state transitions
func TestNegative_TCPStateTransitions_Invalid(t *testing.T) {
	invalidTransitions := []struct {
		name     string
		oldState uint8
		newState uint8
	}{
		{name: "LISTEN to ESTABLISHED", oldState: TCP_LISTEN, newState: TCP_ESTABLISHED},
		{name: "FIN_WAIT1 to SYN_SENT", oldState: TCP_FIN_WAIT1, newState: TCP_SYN_SENT},
		{name: "CLOSE_WAIT to LISTEN", oldState: TCP_CLOSE_WAIT, newState: TCP_LISTEN},
	}

	for _, tt := range invalidTransitions {
		t.Run(tt.name, func(t *testing.T) {
			// Invalid transitions should still return a valid event type
			eventType := stateToEventType(tt.oldState, tt.newState, "", nil)
			assert.NotEmpty(t, eventType)
			// Most will map to generic "tcp_state_change"
			assert.Contains(t, []string{"tcp_state_change", "connection_established", "listen_started", "listen_stopped", "connection_closed"}, eventType)
		})
	}
}

// TestNegative_ConcurrentAccess tests concurrent access to helper functions
func TestNegative_ConcurrentAccess(t *testing.T) {
	setupNegativeTest(t)

	const goroutines = 100
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer func() { done <- true }()

			// Generate varied input to test concurrent access
			ip := uint32(0x0100007f + n)
			ipv6 := [16]byte{byte(n), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
			comm := [16]byte{'t', 'e', 's', 't', byte('0' + n%10), 0}

			// Call all helper functions concurrently
			_ = stateToEventType(TCP_SYN_SENT, TCP_ESTABLISHED, "", nil) // Ignore: prevent compiler optimization
			_ = convertIPv4(ip)                                          // Ignore: prevent compiler optimization
			_ = convertIPv6(ipv6)                                        // Ignore: prevent compiler optimization
			_ = extractComm(comm)                                        // Ignore: prevent compiler optimization
		}(i)
	}

	// Wait for all goroutines with timeout
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// TestNegative_LargeEventBatch tests handling of large event batches
func TestNegative_LargeEventBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large batch test in short mode")
	}

	setupNegativeTest(t)

	// Simulate processing a million events
	const eventCount = 1000000
	processed := 0

	for i := 0; i < eventCount; i++ {
		evt := NetworkEventBPF{
			PID:      uint32(i),
			SrcIP:    uint32(0x0100007f + (i % 256)),
			DstIP:    uint32(0xc0a80100 + (i % 256)),
			SrcPort:  uint16(50000 + (i % 10000)),
			DstPort:  80,
			Family:   2,
			Protocol: IPPROTO_TCP,
			OldState: TCP_SYN_SENT,
			NewState: TCP_ESTABLISHED,
			Comm:     [16]byte{'a', 'p', 'p', 0},
		}

		// Process event
		_ = stateToEventType(evt.OldState, evt.NewState, "", nil) // Ignore: prevent compiler optimization
		_ = convertIPv4(evt.SrcIP)                                // Ignore: prevent compiler optimization
		_ = convertIPv4(evt.DstIP)                                // Ignore: prevent compiler optimization
		_ = extractComm(evt.Comm)                                 // Ignore: prevent compiler optimization

		processed++
	}

	require.Equal(t, eventCount, processed)
}

// TestNegative_ObserverCreation_NilDeps tests observer creation with nil deps
func TestNegative_ObserverCreation_NilDeps(t *testing.T) {
	// New() with nil deps should still work (metrics become nil, handled internally)
	observer := New(Config{}, nil)
	require.NotNil(t, observer)
	assert.Equal(t, "network", observer.name)
	assert.Nil(t, observer.deps)
}
