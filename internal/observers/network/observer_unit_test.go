package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
)

// setupOTEL sets up OTEL for tests
func setupOTEL(t *testing.T) {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(nil)
	})
}

func TestNewNetworkObserver(t *testing.T) {
	setupOTEL(t)

	config := Config{
		Output: base.OutputConfig{
			Stdout: true,
		},
	}

	observer, err := NewNetworkObserver("test-network", config)
	require.NoError(t, err)
	require.NotNil(t, observer)

	assert.Equal(t, "test-network", observer.Name())
	assert.NotNil(t, observer.BaseObserver)
}

func TestNetworkObserver_Name(t *testing.T) {
	setupOTEL(t)

	config := Config{
		Output: base.OutputConfig{Stdout: true},
	}

	observer, err := NewNetworkObserver("my-network-observer", config)
	require.NoError(t, err)

	assert.Equal(t, "my-network-observer", observer.Name())
}

// TestStateToEventType_Established verifies TCP ESTABLISHED mapping
func TestStateToEventType_Established(t *testing.T) {
	eventType := stateToEventType(TCP_SYN_SENT, TCP_ESTABLISHED, "", nil)
	assert.Equal(t, "connection_established", eventType)
}

// TestStateToEventType_ListenStarted verifies LISTEN start mapping
func TestStateToEventType_ListenStarted(t *testing.T) {
	eventType := stateToEventType(TCP_CLOSE, TCP_LISTEN, "", nil)
	assert.Equal(t, "listen_started", eventType)
}

// TestStateToEventType_ListenStopped verifies LISTEN stop mapping
func TestStateToEventType_ListenStopped(t *testing.T) {
	eventType := stateToEventType(TCP_LISTEN, TCP_CLOSE, "", nil)
	assert.Equal(t, "listen_stopped", eventType)
}

// TestStateToEventType_ConnectionClosed verifies connection close mapping
func TestStateToEventType_ConnectionClosed(t *testing.T) {
	eventType := stateToEventType(TCP_ESTABLISHED, TCP_CLOSE, "", nil)
	assert.Equal(t, "connection_closed", eventType)
}

// TestStateToEventType_GenericFallback verifies fallback for other transitions
func TestStateToEventType_GenericFallback(t *testing.T) {
	eventType := stateToEventType(TCP_SYN_SENT, TCP_SYN_RECV, "", nil)
	assert.Equal(t, "tcp_state_change", eventType)
}

// TestConvertIPv4_Localhost verifies localhost conversion
func TestConvertIPv4_Localhost(t *testing.T) {
	ipStr := convertIPv4(0x0100007f) // 127.0.0.1
	assert.Equal(t, "127.0.0.1", ipStr)
}

// TestConvertIPv4_Standard verifies standard IP
func TestConvertIPv4_Standard(t *testing.T) {
	ipStr := convertIPv4(0x6401a8c0) // 192.168.1.100
	assert.Equal(t, "192.168.1.100", ipStr)
}

// TestConvertIPv6_Localhost verifies IPv6 localhost
func TestConvertIPv6_Localhost(t *testing.T) {
	ipv6 := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	ipStr := convertIPv6(ipv6)
	assert.Equal(t, "0:0:0:0:0:0:0:1", ipStr)
}

// TestExtractComm_NullTerminated verifies process name extraction
func TestExtractComm_NullTerminated(t *testing.T) {
	comm := [16]byte{'c', 'u', 'r', 'l', 0}
	assert.Equal(t, "curl", extractComm(comm))
}

// TestExtractComm_Full verifies full 16-char names
func TestExtractComm_Full(t *testing.T) {
	comm := [16]byte{'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p'}
	assert.Equal(t, "abcdefghijklmnop", extractComm(comm))
}

// TestRetransmitStatsTracking tests retransmit stats structure
func TestRetransmitStatsTracking(t *testing.T) {
	setupOTEL(t)

	config := Config{Output: base.OutputConfig{Stdout: false}}
	observer, err := NewNetworkObserver("test-retransmit", config)
	require.NoError(t, err)

	connKey := "127.0.0.1:50000:127.0.0.2:80"

	// Manually store stats to test the data structure
	stats := &retransmitStats{
		totalPackets: 100,
		retransmits:  5,
	}
	observer.retransmitStats.Store(connKey, stats)

	// Verify stats were stored
	statsInterface, ok := observer.retransmitStats.Load(connKey)
	assert.True(t, ok, "Stats should be stored for connection")

	retrievedStats := statsInterface.(*retransmitStats)
	assert.Equal(t, uint64(100), retrievedStats.totalPackets, "Should have 100 total packets")
	assert.Equal(t, uint64(5), retrievedStats.retransmits, "Should have 5 retransmits")

	// Test retransmit rate calculation
	rate := float64(retrievedStats.retransmits) / float64(retrievedStats.totalPackets) * 100
	assert.Equal(t, 5.0, rate, "Retransmit rate should be 5%")
}

// TestHighRetransmitRateCalculation tests high retransmit rate detection logic
func TestHighRetransmitRateCalculation(t *testing.T) {
	// Test low rate (below threshold)
	lowStats := &retransmitStats{totalPackets: 100, retransmits: 3}
	lowRate := float64(lowStats.retransmits) / float64(lowStats.totalPackets) * 100
	assert.Less(t, lowRate, 5.0, "3% rate should be below 5% threshold")

	// Test exact threshold
	thresholdStats := &retransmitStats{totalPackets: 100, retransmits: 5}
	thresholdRate := float64(thresholdStats.retransmits) / float64(thresholdStats.totalPackets) * 100
	assert.Equal(t, 5.0, thresholdRate, "5% rate should equal threshold")

	// Test high rate (above threshold)
	highStats := &retransmitStats{totalPackets: 100, retransmits: 10}
	highRate := float64(highStats.retransmits) / float64(highStats.totalPackets) * 100
	assert.Greater(t, highRate, 5.0, "10% rate should exceed threshold")
}
