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
	eventType := stateToEventType(TCP_SYN_SENT, TCP_ESTABLISHED)
	assert.Equal(t, "connection_established", eventType)
}

// TestStateToEventType_ListenStarted verifies LISTEN start mapping
func TestStateToEventType_ListenStarted(t *testing.T) {
	eventType := stateToEventType(TCP_CLOSE, TCP_LISTEN)
	assert.Equal(t, "listen_started", eventType)
}

// TestStateToEventType_ListenStopped verifies LISTEN stop mapping
func TestStateToEventType_ListenStopped(t *testing.T) {
	eventType := stateToEventType(TCP_LISTEN, TCP_CLOSE)
	assert.Equal(t, "listen_stopped", eventType)
}

// TestStateToEventType_ConnectionClosed verifies connection close mapping
func TestStateToEventType_ConnectionClosed(t *testing.T) {
	eventType := stateToEventType(TCP_ESTABLISHED, TCP_CLOSE)
	assert.Equal(t, "connection_closed", eventType)
}

// TestStateToEventType_GenericFallback verifies fallback for other transitions
func TestStateToEventType_GenericFallback(t *testing.T) {
	eventType := stateToEventType(TCP_SYN_SENT, TCP_SYN_RECV)
	assert.Equal(t, "tcp_state_change", eventType)
}
