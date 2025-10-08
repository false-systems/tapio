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
