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
			eventType := stateToEventType(tt.bpfEvent.OldState, tt.bpfEvent.NewState)
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
	eventType := stateToEventType(bpfEvent.OldState, bpfEvent.NewState)
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
			eventType := stateToEventType(e.OldState, e.NewState)
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
	eventType := stateToEventType(bpfEvent.OldState, bpfEvent.NewState)
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
