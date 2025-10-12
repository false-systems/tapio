package base

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

func TestLoadTelemetryConfigFromEnv(t *testing.T) {
	// Save original env vars
	originalEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	originalCluster := os.Getenv("TAPIO_CLUSTER_ID")
	defer func() {
		os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", originalEndpoint)
		os.Setenv("TAPIO_CLUSTER_ID", originalCluster)
	}()

	t.Run("loads defaults when env vars not set", func(t *testing.T) {
		os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		os.Unsetenv("TAPIO_CLUSTER_ID")

		config := LoadTelemetryConfigFromEnv()
		require.NotNil(t, config)

		assert.Equal(t, "localhost:4317", config.OTLPEndpoint)
		assert.Equal(t, "default", config.ClusterID)
		assert.Equal(t, "tapio-system", config.Namespace)
		assert.Equal(t, "tapio-observer", config.ServiceName)
		assert.Equal(t, "dev", config.Version)
		assert.Equal(t, 0.1, config.TraceSampleRate)
		assert.Equal(t, 10*time.Second, config.MetricInterval)
		assert.False(t, config.Insecure)
	})

	t.Run("loads custom values from env", func(t *testing.T) {
		os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "collector:4317")
		os.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
		os.Setenv("TAPIO_CLUSTER_ID", "prod-cluster")
		os.Setenv("TAPIO_NAMESPACE", "monitoring")
		os.Setenv("TAPIO_NODE_NAME", "node-1")
		os.Setenv("TAPIO_SERVICE_NAME", "custom-observer")
		os.Setenv("TAPIO_VERSION", "v1.2.3")
		defer func() {
			os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
			os.Unsetenv("OTEL_EXPORTER_OTLP_INSECURE")
			os.Unsetenv("TAPIO_CLUSTER_ID")
			os.Unsetenv("TAPIO_NAMESPACE")
			os.Unsetenv("TAPIO_NODE_NAME")
			os.Unsetenv("TAPIO_SERVICE_NAME")
			os.Unsetenv("TAPIO_VERSION")
		}()

		config := LoadTelemetryConfigFromEnv()
		require.NotNil(t, config)

		assert.Equal(t, "collector:4317", config.OTLPEndpoint)
		assert.True(t, config.Insecure)
		assert.Equal(t, "prod-cluster", config.ClusterID)
		assert.Equal(t, "monitoring", config.Namespace)
		assert.Equal(t, "node-1", config.NodeName)
		assert.Equal(t, "custom-observer", config.ServiceName)
		assert.Equal(t, "v1.2.3", config.Version)
	})
}

func TestInitTelemetry(t *testing.T) {
	t.Run("returns error for nil config", func(t *testing.T) {
		ctx := context.Background()
		shutdown, err := InitTelemetry(ctx, nil)

		assert.Error(t, err)
		assert.Nil(t, shutdown)
		assert.Contains(t, err.Error(), "nil")
	})

	t.Run("initializes with valid config", func(t *testing.T) {
		// Note: This will fail to connect since there's no real OTLP collector,
		// but it should still initialize the providers
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		config := &TelemetryConfig{
			OTLPEndpoint:    "localhost:4317",
			Insecure:        true,
			ClusterID:       "test-cluster",
			Namespace:       "test-ns",
			NodeName:        "test-node",
			ServiceName:     "test-observer",
			Version:         "test",
			TraceSampleRate: 1.0,
			MetricInterval:  1 * time.Second,
		}

		shutdown, err := InitTelemetry(ctx, config)

		// Should succeed even without real collector
		require.NoError(t, err)
		require.NotNil(t, shutdown)

		// Verify global providers are set
		assert.NotNil(t, otel.GetTracerProvider())
		assert.NotNil(t, otel.GetMeterProvider())

		// Clean up
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		shutdown.Shutdown(cleanupCtx)
	})
}

func TestTelemetryShutdown(t *testing.T) {
	t.Run("handles nil shutdown", func(t *testing.T) {
		var shutdown *TelemetryShutdown
		ctx := context.Background()

		err := shutdown.Shutdown(ctx)
		assert.NoError(t, err)
	})

	t.Run("shuts down without panic", func(t *testing.T) {
		ctx := context.Background()

		config := &TelemetryConfig{
			OTLPEndpoint:    "localhost:4317",
			Insecure:        true,
			ClusterID:       "test",
			Namespace:       "test",
			NodeName:        "test",
			ServiceName:     "test",
			Version:         "test",
			TraceSampleRate: 1.0,
			MetricInterval:  1 * time.Second,
		}

		shutdown, err := InitTelemetry(ctx, config)
		require.NoError(t, err)
		require.NotNil(t, shutdown)

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		// Shutdown will return error without real collector, but shouldn't panic
		shutdown.Shutdown(shutdownCtx)
		// Test passes if no panic occurs
	})
}

func TestNewBaseObserverWithTelemetry(t *testing.T) {
	t.Run("creates observer without telemetry", func(t *testing.T) {
		observer, err := NewBaseObserverWithTelemetry("test-observer", nil)
		require.NoError(t, err)
		require.NotNil(t, observer)

		assert.Equal(t, "test-observer", observer.Name())
		assert.Nil(t, observer.telemetryShutdown)
	})

	t.Run("creates observer with telemetry", func(t *testing.T) {
		config := &TelemetryConfig{
			OTLPEndpoint:    "localhost:4317",
			Insecure:        true,
			ClusterID:       "test",
			Namespace:       "test",
			NodeName:        "test",
			ServiceName:     "test",
			Version:         "test",
			TraceSampleRate: 1.0,
			MetricInterval:  1 * time.Second,
		}

		observer, err := NewBaseObserverWithTelemetry("test-observer", config)
		require.NoError(t, err)
		require.NotNil(t, observer)

		assert.Equal(t, "test-observer", observer.Name())
		assert.NotNil(t, observer.telemetryShutdown)

		// Clean up - Start first so Stop can succeed
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go observer.Start(ctx)
		time.Sleep(50 * time.Millisecond)

		// Stop may return error without real collector, but shouldn't panic
		observer.Stop()
	})
}

func TestObserverStopWithTelemetry(t *testing.T) {
	t.Run("stops and attempts telemetry shutdown", func(t *testing.T) {
		config := &TelemetryConfig{
			OTLPEndpoint:    "localhost:4317",
			Insecure:        true,
			ClusterID:       "test",
			Namespace:       "test",
			NodeName:        "test",
			ServiceName:     "test",
			Version:         "test",
			TraceSampleRate: 1.0,
			MetricInterval:  1 * time.Second,
		}

		observer, err := NewBaseObserverWithTelemetry("test-observer", config)
		require.NoError(t, err)

		// Start observer
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go observer.Start(ctx)
		time.Sleep(50 * time.Millisecond)

		// Stop will attempt telemetry shutdown (may fail without real collector, but shouldn't panic)
		observer.Stop()
		// Test passes if no panic occurs
	})
}
