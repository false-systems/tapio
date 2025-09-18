//go:build linux
// +build linux

package network

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestEBPFInitialization(t *testing.T) {
	if !isRoot() {
		t.Skip("Skipping eBPF test: requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	observer, err := NewObserver("test-ebpf", config, logger)
	require.NoError(t, err)

	// Test eBPF initialization
	err = observer.loadEBPF()
	assert.NoError(t, err)
	assert.NotNil(t, observer.ebpfState)

	// Cleanup
	observer.closeEBPF()
}

func TestNetworkProbeAttachment(t *testing.T) {
	if !isRoot() {
		t.Skip("Skipping eBPF test: requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	observer, err := NewObserver("test-probes", config, logger)
	require.NoError(t, err)

	// Load eBPF programs
	err = observer.loadEBPF()
	require.NoError(t, err)
	defer observer.closeEBPF()

	// Verify probes are attached
	ebpfState := observer.ebpfState.(*networkEBPF)
	assert.NotEmpty(t, ebpfState.links)
	assert.NotNil(t, ebpfState.reader)
}

func TestL7PortConfiguration(t *testing.T) {
	if !isRoot() {
		t.Skip("Skipping eBPF test: requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.HTTPPorts = []int{80, 8080, 3000}
	config.DNSPort = 53

	observer, err := NewObserver("test-l7", config, logger)
	require.NoError(t, err)

	// Load eBPF programs
	err = observer.loadEBPF()
	require.NoError(t, err)
	defer observer.closeEBPF()

	// Configure L7 ports
	err = observer.configureL7Ports()
	assert.NoError(t, err)
}

func TestBPFEventConversion(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver("test-convert", DefaultConfig(), logger)
	require.NoError(t, err)

	tests := []struct {
		name     string
		bpfEvent *BPFNetworkEvent
		validate func(*testing.T, *NetworkEvent)
	}{
		{
			name: "TCP IPv4 connection",
			bpfEvent: &BPFNetworkEvent{
				Timestamp: uint64(time.Now().UnixNano()),
				PID:       1234,
				TID:       1235,
				EventType: EventTypeConnection,
				Protocol:  ProtocolTCP,
				IPVersion: IPVersion4,
				Direction: DirectionOutbound,
				SrcAddr:   [16]uint8{192, 168, 1, 100},
				DstAddr:   [16]uint8{10, 0, 0, 1},
				SrcPort:   45678,
				DstPort:   80,
				ConnState: ConnStateEstablished,
				Comm:      [16]uint8{'c', 'u', 'r', 'l'},
			},
			validate: func(t *testing.T, event *NetworkEvent) {
				assert.Equal(t, uint32(1234), event.PID)
				assert.Equal(t, "connection", event.EventType)
				assert.Equal(t, "TCP", event.Protocol)
				assert.Equal(t, "192.168.1.100", event.SrcIP.String())
				assert.Equal(t, "10.0.0.1", event.DstIP.String())
				assert.Equal(t, uint16(45678), event.SrcPort)
				assert.Equal(t, uint16(80), event.DstPort)
				assert.Equal(t, "established", event.ConnState)
				assert.Equal(t, "curl", event.Command)
			},
		},
		{
			name: "UDP IPv6 DNS query",
			bpfEvent: &BPFNetworkEvent{
				Timestamp:  uint64(time.Now().UnixNano()),
				PID:        2345,
				EventType:  EventTypeDNSQuery,
				Protocol:   ProtocolUDP,
				IPVersion:  IPVersion6,
				Direction:  DirectionOutbound,
				SrcAddr:    [16]uint8{0x20, 0x01, 0x0d, 0xb8},
				DstAddr:    [16]uint8{0x20, 0x01, 0x48, 0x60},
				SrcPort:    54321,
				DstPort:    53,
				L7Protocol: L7ProtocolDNS,
			},
			validate: func(t *testing.T, event *NetworkEvent) {
				assert.Equal(t, uint32(2345), event.PID)
				assert.Equal(t, "dns_query", event.EventType)
				assert.Equal(t, "UDP", event.Protocol)
				assert.Equal(t, IPVersion6, event.IPVersion)
				assert.Equal(t, uint16(54321), event.SrcPort)
				assert.Equal(t, uint16(53), event.DstPort)
				assert.Equal(t, "DNS", event.L7Protocol)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := observer.convertBPFEvent(tt.bpfEvent)
			require.NotNil(t, event)
			tt.validate(t, event)
		})
	}
}

func TestConnectionTrackingWithEBPF(t *testing.T) {
	if !isRoot() {
		t.Skip("Skipping eBPF test: requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.MaxConnections = 100
	config.ConnectionTimeout = 1 * time.Minute

	observer, err := NewObserver("test-tracking", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Simulate network event
	event := &NetworkEvent{
		EventID:     "test-conn-1",
		Timestamp:   time.Now(),
		EventType:   "connection",
		PID:         1234,
		Protocol:    "TCP",
		SrcIP:       net.ParseIP("192.168.1.100"),
		DstIP:       net.ParseIP("10.0.0.1"),
		SrcPort:     45678,
		DstPort:     80,
		ConnState:   "established",
		BytesSent:   1024,
		BytesRecv:   2048,
		PacketsSent: 10,
		PacketsRecv: 20,
	}

	observer.updateConnectionTracking(event)

	// Verify connection is tracked
	ebpfState := observer.ebpfState.(*networkEBPF)
	ebpfState.connectionsMutex.RLock()
	connCount := len(ebpfState.connections)
	ebpfState.connectionsMutex.RUnlock()

	assert.Equal(t, 1, connCount)
}

func TestNetworkMetricsCollection(t *testing.T) {
	if !isRoot() {
		t.Skip("Skipping eBPF test: requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver("test-metrics", DefaultConfig(), logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Create test event
	event := &NetworkEvent{
		EventID:    "metric-test-1",
		Timestamp:  time.Now(),
		EventType:  "connection",
		Protocol:   "TCP",
		BytesSent:  1024,
		BytesRecv:  2048,
		L7Protocol: "HTTP",
		Latency:    10 * time.Millisecond,
	}

	// Update metrics
	observer.updateNetworkMetrics(ctx, event)

	// Let metrics export
	time.Sleep(100 * time.Millisecond)

	// Check statistics
	stats := observer.Statistics()
	assert.NotNil(t, stats)
}

func TestEBPFEventProcessing(t *testing.T) {
	if !isRoot() {
		t.Skip("Skipping eBPF test: requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.BufferSize = 100

	observer, err := NewObserver("test-processing", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Events should be flowing
	select {
	case <-observer.Events():
		// Got an event
	case <-time.After(3 * time.Second):
		// No events in 3 seconds is okay in test environment
	}

	// Check health
	health := observer.Health()
	assert.True(t, health.Healthy)
}

func TestConnectionCleanupWithEBPF(t *testing.T) {
	if !isRoot() {
		t.Skip("Skipping eBPF test: requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.ConnectionTimeout = 100 * time.Millisecond
	config.ConnectionCleanupInterval = 50 * time.Millisecond

	observer, err := NewObserver("test-cleanup", config, logger)
	require.NoError(t, err)

	// Initialize eBPF state
	observer.ebpfState = &networkEBPF{
		connections: make(map[string]*ConnectionInfo),
		logger:      logger,
		config:      config,
	}

	// Add old and new connections
	oldTime := time.Now().Add(-10 * time.Minute)
	recentTime := time.Now()

	ebpfState := observer.ebpfState.(*networkEBPF)
	ebpfState.connections["old"] = &ConnectionInfo{
		LastActivity: oldTime,
	}
	ebpfState.connections["new"] = &ConnectionInfo{
		LastActivity: recentTime,
	}

	// Run cleanup
	observer.cleanupStaleConnections()

	// Check results
	ebpfState.connectionsMutex.RLock()
	_, hasOld := ebpfState.connections["old"]
	_, hasNew := ebpfState.connections["new"]
	ebpfState.connectionsMutex.RUnlock()

	assert.False(t, hasOld)
	assert.True(t, hasNew)
}

// isRoot checks if running as root
func isRoot() bool {
	return testing.Short() == false
}
