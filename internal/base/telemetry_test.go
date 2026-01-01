package base

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitForEndpoint polls an HTTP endpoint until it responds or timeout is reached
func waitForEndpoint(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				return fmt.Errorf("failed to close response body: %w", closeErr)
			}
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("endpoint %s not ready within %s", url, timeout)
}

// getFreePort allocates a free ephemeral port
func getFreePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Logf("failed to close listener: %v", err)
	}
	return port
}

// TestHealthEndpoint_AlwaysReturns200 tests that /health always returns 200 OK
func TestHealthEndpoint_AlwaysReturns200(t *testing.T) {
	port := getFreePort(t)
	config := &TelemetryConfig{
		OTLPEndpoint:      "localhost:4317",
		Insecure:          true,
		PrometheusEnabled: true,
		PrometheusPort:    port,
		ServiceName:       "test-service",
		Version:           "test",
		TraceSampleRate:   0.1,
		MetricInterval:    10 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := InitTelemetry(ctx, config)
	require.NoError(t, err)
	defer func() {
		if err := shutdown.Shutdown(ctx); err != nil {
			t.Logf("telemetry shutdown error: %v", err)
		}
	}()

	healthURL := fmt.Sprintf("http://localhost:%d/health", port)
	require.NoError(t, waitForEndpoint(healthURL, 2*time.Second))

	resp, err := http.Get(healthURL)
	require.NoError(t, err)
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("failed to close response body: %v", err)
		}
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "/health should always return 200")
}

// TestReadyEndpoint_AlwaysReturns200 tests that /ready always returns 200 OK
func TestReadyEndpoint_AlwaysReturns200(t *testing.T) {
	port := getFreePort(t)
	config := &TelemetryConfig{
		OTLPEndpoint:      "localhost:4317",
		Insecure:          true,
		PrometheusEnabled: true,
		PrometheusPort:    port,
		ServiceName:       "test-service",
		Version:           "test",
		TraceSampleRate:   0.1,
		MetricInterval:    10 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := InitTelemetry(ctx, config)
	require.NoError(t, err)
	defer func() {
		if err := shutdown.Shutdown(ctx); err != nil {
			t.Logf("telemetry shutdown error: %v", err)
		}
	}()

	readyURL := fmt.Sprintf("http://localhost:%d/ready", port)
	require.NoError(t, waitForEndpoint(readyURL, 2*time.Second))

	resp, err := http.Get(readyURL)
	require.NoError(t, err)
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("failed to close response body: %v", err)
		}
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "/ready should always return 200")
}
