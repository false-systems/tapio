package supervisor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestHealth_HealthyObserver tests that healthy observers continue running
func TestHealth_HealthyObserver(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout:     1 * time.Second,
		HealthCheckInterval: 100 * time.Millisecond,
	})

	var checks atomic.Int32
	healthCheck := func(ctx context.Context) HealthStatus {
		checks.Add(1)
		return HealthStatusHealthy
	}

	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}, WithHealthCheck(healthCheck))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := sup.Run(ctx)
	require.NoError(t, err)

	// Health check should have been called ~5 times (500ms / 100ms interval)
	assert.GreaterOrEqual(t, int(checks.Load()), 4, "health check should be called multiple times")
}

// TestHealth_UnhealthyObserverRestart tests unhealthy observer restart
func TestHealth_UnhealthyObserverRestart(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout:     1 * time.Second,
		HealthCheckInterval: 100 * time.Millisecond,
	})

	var attempts atomic.Int32
	var healthCheckCount atomic.Int32

	healthCheck := func(ctx context.Context) HealthStatus {
		count := healthCheckCount.Add(1)
		if count >= 3 {
			return HealthStatusUnhealthy // Become unhealthy after 3 checks
		}
		return HealthStatusHealthy
	}

	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		attempts.Add(1)
		<-ctx.Done()
		return nil
	}, WithHealthCheck(healthCheck), WithRestartPolicy(5, 1*time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for unhealthy restart
	require.Eventually(t, func() bool {
		return attempts.Load() >= 2 // Should restart at least once
	}, 3*time.Second, 50*time.Millisecond, "observer should restart when unhealthy")

	cancel()
	<-errCh
}

// TestHealth_DegradedObserverContinues tests degraded observer keeps running
func TestHealth_DegradedObserverContinues(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout:     1 * time.Second,
		HealthCheckInterval: 100 * time.Millisecond,
	})

	var degradedCount atomic.Int32
	healthCheck := func(ctx context.Context) HealthStatus {
		degradedCount.Add(1)
		return HealthStatusDegraded
	}

	var started atomic.Bool
	var restartCount atomic.Int32
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		if started.Load() {
			restartCount.Add(1)
		}
		started.Store(true)
		<-ctx.Done()
		return nil
	}, WithHealthCheck(healthCheck))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := sup.Run(ctx)
	require.NoError(t, err)

	// Observer should have started once (not restarted for degraded status)
	assert.True(t, started.Load(), "observer should have started")
	assert.Equal(t, int32(0), restartCount.Load(), "degraded observer should not restart")
	// Should have been marked degraded multiple times
	assert.GreaterOrEqual(t, int(degradedCount.Load()), 4, "health check should report degraded multiple times")
}

// TestHealth_NoHealthCheck tests observer without health check works normally
func TestHealth_NoHealthCheck(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout:     1 * time.Second,
		HealthCheckInterval: 100 * time.Millisecond,
	})

	var started atomic.Bool
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		started.Store(true)
		<-ctx.Done()
		return nil
	}) // No health check - should work normally

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := sup.Run(ctx)
	assert.NoError(t, err)
	assert.True(t, started.Load(), "observer without health check should run normally")
}

// TestHealth_HealthCheckPanic tests health check panic handling
func TestHealth_HealthCheckPanic(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout:     1 * time.Second,
		HealthCheckInterval: 100 * time.Millisecond,
	})

	var panicCount atomic.Int32
	healthCheck := func(ctx context.Context) HealthStatus {
		panicCount.Add(1)
		panic("health check panic!")
	}

	var started atomic.Bool
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		started.Store(true)
		<-ctx.Done()
		return nil
	}, WithHealthCheck(healthCheck))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Should not crash supervisor
	err := sup.Run(ctx)
	assert.NoError(t, err)
	assert.True(t, started.Load(), "observer should start despite health check panics")
	assert.GreaterOrEqual(t, int(panicCount.Load()), 1, "health check should have been called at least once")
}

// TestHealth_WithMetrics tests health status metrics
func TestHealth_WithMetrics(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test-supervisor")

	sup := New(Config{
		ShutdownTimeout:     1 * time.Second,
		HealthCheckInterval: 100 * time.Millisecond,
	}, WithMeter(meter))

	healthCheck := func(ctx context.Context) HealthStatus {
		return HealthStatusHealthy
	}

	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}, WithHealthCheck(healthCheck))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	time.Sleep(250 * time.Millisecond)

	// Collect metrics
	rm := &metricdata.ResourceMetrics{}
	err := reader.Collect(context.Background(), rm)
	require.NoError(t, err)

	// Verify health status gauge
	healthGauge := findGauge(rm, "supervisor_observer_health_status")
	require.NotNil(t, healthGauge, "health status gauge should exist")

	// Should show healthy status
	var found bool
	for _, dp := range healthGauge.DataPoints {
		attrs := attributesToMap(dp.Attributes)
		if attrs["observer"] == "test-observer" && attrs["status"] == "healthy" {
			assert.Equal(t, int64(1), dp.Value, "healthy status should be 1")
			found = true
			break
		}
	}
	assert.True(t, found, "health status metric should be recorded")

	cancel()
	<-errCh
}

// TestHealth_MultipleHealthChecks tests multiple observers with different health statuses
func TestHealth_MultipleHealthChecks(t *testing.T) {
	sup := New(Config{
		ShutdownTimeout:     1 * time.Second,
		HealthCheckInterval: 100 * time.Millisecond,
	})

	// Observer 1: Always healthy
	sup.SuperviseFunc("observer-1", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}, WithHealthCheck(func(ctx context.Context) HealthStatus {
		return HealthStatusHealthy
	}))

	// Observer 2: Always degraded
	sup.SuperviseFunc("observer-2", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}, WithHealthCheck(func(ctx context.Context) HealthStatus {
		return HealthStatusDegraded
	}))

	// Observer 3: No health check
	sup.SuperviseFunc("observer-3", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := sup.Run(ctx)
	assert.NoError(t, err, "all observers should run successfully")
}
