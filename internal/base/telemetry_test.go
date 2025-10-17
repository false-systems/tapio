package base

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHealthEndpoint_AlwaysReturns200 tests that /health always returns 200 OK
func TestHealthEndpoint_AlwaysReturns200(t *testing.T) {
	// Setup: Start telemetry with Prometheus enabled
	config := &TelemetryConfig{
		OTLPEndpoint:      "localhost:4317",
		Insecure:          true,
		PrometheusEnabled: true,
		PrometheusPort:    19090, // Use different port to avoid conflicts
		ServiceName:       "test-service",
		Version:           "test",
		TraceSampleRate:   0.1,
		MetricInterval:    10 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := InitTelemetry(ctx, config, nil) // No observers
	require.NoError(t, err)
	defer shutdown.Shutdown(ctx)

	// Wait for HTTP server to start
	time.Sleep(100 * time.Millisecond)

	// Test: GET /health
	resp, err := http.Get("http://localhost:19090/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Always returns 200 OK
	assert.Equal(t, http.StatusOK, resp.StatusCode, "/health should always return 200")
}

// TestReadyEndpoint_AllObserversHealthy tests /ready when all observers are healthy
func TestReadyEndpoint_AllObserversHealthy(t *testing.T) {
	// Setup: Create mock healthy observers
	obs1, err := NewBaseObserver("test-observer-1")
	require.NoError(t, err)
	obs1.running.Store(true) // Mark as running

	obs2, err := NewBaseObserver("test-observer-2")
	require.NoError(t, err)
	obs2.running.Store(true)

	observers := []Observer{obs1, obs2}

	// Setup: Start telemetry with observers
	config := &TelemetryConfig{
		OTLPEndpoint:      "localhost:4317",
		Insecure:          true,
		PrometheusEnabled: true,
		PrometheusPort:    19091,
		ServiceName:       "test-service",
		Version:           "test",
		TraceSampleRate:   0.1,
		MetricInterval:    10 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := InitTelemetry(ctx, config, observers)
	require.NoError(t, err)
	defer shutdown.Shutdown(ctx)

	// Wait for HTTP server to start
	time.Sleep(100 * time.Millisecond)

	// Test: GET /ready
	resp, err := http.Get("http://localhost:19091/ready")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Returns 200 when all observers healthy
	assert.Equal(t, http.StatusOK, resp.StatusCode, "/ready should return 200 when all observers healthy")
}

// TestReadyEndpoint_OneObserverUnhealthy tests /ready when one observer is unhealthy
func TestReadyEndpoint_OneObserverUnhealthy(t *testing.T) {
	// Setup: Create observers (one healthy, one unhealthy)
	obs1, err := NewBaseObserver("test-observer-1")
	require.NoError(t, err)
	obs1.running.Store(true) // Healthy

	obs2, err := NewBaseObserver("test-observer-2")
	require.NoError(t, err)
	obs2.running.Store(false) // Unhealthy

	observers := []Observer{obs1, obs2}

	// Setup: Start telemetry with observers
	config := &TelemetryConfig{
		OTLPEndpoint:      "localhost:4317",
		Insecure:          true,
		PrometheusEnabled: true,
		PrometheusPort:    19092,
		ServiceName:       "test-service",
		Version:           "test",
		TraceSampleRate:   0.1,
		MetricInterval:    10 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := InitTelemetry(ctx, config, observers)
	require.NoError(t, err)
	defer shutdown.Shutdown(ctx)

	// Wait for HTTP server to start
	time.Sleep(100 * time.Millisecond)

	// Test: GET /ready
	resp, err := http.Get("http://localhost:19092/ready")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Returns 503 when any observer unhealthy
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, "/ready should return 503 when any observer unhealthy")
}

// TestReadyEndpoint_NoObservers tests /ready when no observers provided
func TestReadyEndpoint_NoObservers(t *testing.T) {
	// Setup: Start telemetry without observers
	config := &TelemetryConfig{
		OTLPEndpoint:      "localhost:4317",
		Insecure:          true,
		PrometheusEnabled: true,
		PrometheusPort:    19093,
		ServiceName:       "test-service",
		Version:           "test",
		TraceSampleRate:   0.1,
		MetricInterval:    10 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := InitTelemetry(ctx, config, nil)
	require.NoError(t, err)
	defer shutdown.Shutdown(ctx)

	// Wait for HTTP server to start
	time.Sleep(100 * time.Millisecond)

	// Test: GET /ready
	resp, err := http.Get("http://localhost:19093/ready")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Returns 200 when no observers (nothing to check)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "/ready should return 200 when no observers")
}
