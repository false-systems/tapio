package otel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewGRPCExporter(t *testing.T) {
	logger := zap.NewNop()

	t.Run("Creates exporter with valid config", func(t *testing.T) {
		config := OTLPConfig{
			Enabled:  true,
			Endpoint: "localhost:4317",
			Timeout:  10 * time.Second,
			Insecure: true,
			Headers:  make(map[string]string),
		}

		exporter, err := NewGRPCExporter(config, logger)
		require.NoError(t, err)
		require.NotNil(t, exporter)
		assert.Equal(t, config.Endpoint, exporter.config.Endpoint)
	})

	t.Run("Fails when OTLP is disabled", func(t *testing.T) {
		config := OTLPConfig{
			Enabled: false,
		}

		exporter, err := NewGRPCExporter(config, logger)
		assert.Error(t, err)
		assert.Nil(t, exporter)
		assert.Contains(t, err.Error(), "disabled")
	})

	t.Run("Uses headers if provided", func(t *testing.T) {
		config := OTLPConfig{
			Enabled:  true,
			Endpoint: "localhost:4317",
			Timeout:  10 * time.Second,
			Insecure: true,
			Headers: map[string]string{
				"authorization": "Bearer token123",
				"x-custom-key":  "value",
			},
		}

		exporter, err := NewGRPCExporter(config, logger)
		require.NoError(t, err)
		require.NotNil(t, exporter)
		assert.Len(t, exporter.config.Headers, 2)
	})
}

func TestNewExporter(t *testing.T) {
	logger := zap.NewNop()

	t.Run("Returns NoopExporter when disabled", func(t *testing.T) {
		config := OTLPConfig{
			Enabled: false,
		}

		exporter, err := NewExporter(config, logger)
		require.NoError(t, err)
		require.NotNil(t, exporter)

		// Should be NoopExporter
		_, ok := exporter.(*NoopExporter)
		assert.True(t, ok)
	})

	t.Run("Returns GRPCExporter when enabled", func(t *testing.T) {
		config := OTLPConfig{
			Enabled:  true,
			Endpoint: "localhost:4317",
			Timeout:  10 * time.Second,
			Insecure: true,
			Headers:  make(map[string]string),
		}

		exporter, err := NewExporter(config, logger)
		require.NoError(t, err)
		require.NotNil(t, exporter)

		// Should be GRPCExporter
		_, ok := exporter.(*GRPCExporter)
		assert.True(t, ok)
	})
}

func TestGRPCExporter_ExportSpans(t *testing.T) {
	logger := zap.NewNop()

	config := OTLPConfig{
		Enabled:  true,
		Endpoint: "localhost:4317",
		Timeout:  2 * time.Second,
		Insecure: true,
		Headers:  make(map[string]string),
	}

	exporter, err := NewGRPCExporter(config, logger)
	require.NoError(t, err)

	t.Run("Handles empty span batch", func(t *testing.T) {
		ctx := context.Background()
		err := exporter.ExportSpans(ctx, nil)
		assert.NoError(t, err)
	})

	t.Run("Handles cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := exporter.ExportSpans(ctx, nil)
		assert.NoError(t, err) // Empty batch returns early
	})

	// Note: We can't test actual span export without TransformSpansToOTLP
	// That will be tested in Chunk 4
}

func TestGRPCExporter_Shutdown(t *testing.T) {
	logger := zap.NewNop()

	config := OTLPConfig{
		Enabled:  true,
		Endpoint: "localhost:4317",
		Timeout:  10 * time.Second,
		Insecure: true,
		Headers:  make(map[string]string),
	}

	exporter, err := NewGRPCExporter(config, logger)
	require.NoError(t, err)

	t.Run("Shutdown succeeds", func(t *testing.T) {
		ctx := context.Background()
		err := exporter.Shutdown(ctx)
		assert.NoError(t, err)
	})
}

func TestDialOptions(t *testing.T) {
	t.Run("Insecure connection", func(t *testing.T) {
		config := OTLPConfig{
			Insecure: true,
		}

		opts := dialOptions(config)
		assert.NotEmpty(t, opts)
	})

	t.Run("Secure connection", func(t *testing.T) {
		config := OTLPConfig{
			Insecure: false,
		}

		opts := dialOptions(config)
		assert.Empty(t, opts) // No special options for secure
	})
}

func TestGRPCExporter_Metrics(t *testing.T) {
	logger := zap.NewNop()

	config := OTLPConfig{
		Enabled:  true,
		Endpoint: "localhost:4317",
		Timeout:  2 * time.Second,
		Insecure: true,
		Headers:  make(map[string]string),
	}

	exporter, err := NewGRPCExporter(config, logger)
	require.NoError(t, err)

	t.Run("Initial metrics are zero", func(t *testing.T) {
		metrics := exporter.Metrics()
		assert.Equal(t, int64(0), metrics.ExportsTotal)
		assert.Equal(t, int64(0), metrics.ExportsFailed)
		assert.Equal(t, int64(0), metrics.SpansExported)
		assert.True(t, metrics.LastExportTime.IsZero())
	})

	// Note: We can't test actual export without a running OTLP server
	// That will be tested in integration tests
}
