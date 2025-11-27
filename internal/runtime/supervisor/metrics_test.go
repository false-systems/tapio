package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestMetrics_ObserverStarts tests that observer starts are recorded
func TestMetrics_ObserverStarts(t *testing.T) {
	// Setup OTEL test infrastructure
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test-supervisor")

	// Create supervisor with metrics
	sup := New(Config{ShutdownTimeout: 1 * time.Second}, WithMeter(meter))

	// Add observer
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})

	// Start supervisor
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for observer to start
	time.Sleep(100 * time.Millisecond)

	// Collect metrics
	rm := &metricdata.ResourceMetrics{}
	err := reader.Collect(context.Background(), rm)
	require.NoError(t, err)

	// Verify observer_starts_total counter exists and equals 1
	starts := findCounter(rm, "supervisor_observer_starts_total")
	require.NotNil(t, starts, "supervisor_observer_starts_total counter not found")

	// Find data point for our observer
	var found bool
	for _, dp := range starts.DataPoints {
		attrs := attributesToMap(dp.Attributes)
		if attrs["observer"] == "test-observer" && attrs["result"] == "success" {
			assert.Equal(t, int64(1), dp.Value, "start counter should be 1")
			found = true
			break
		}
	}
	assert.True(t, found, "data point for test-observer not found")

	cancel()
	<-errCh
}

// TestMetrics_ObserverRestarts tests that observer restarts are recorded
func TestMetrics_ObserverRestarts(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test-supervisor")

	sup := New(Config{ShutdownTimeout: 1 * time.Second}, WithMeter(meter))

	var attempt int
	sup.SuperviseFunc("failing-observer", func(ctx context.Context) error {
		attempt++
		if attempt == 1 {
			return assert.AnError // Fail first attempt
		}
		<-ctx.Done()
		return nil
	}, WithRestartPolicy(5, 1*time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for restart to happen
	time.Sleep(1500 * time.Millisecond)

	// Collect metrics
	rm := &metricdata.ResourceMetrics{}
	err := reader.Collect(context.Background(), rm)
	require.NoError(t, err)

	// Verify observer_restarts_total counter
	restarts := findCounter(rm, "supervisor_observer_restarts_total")
	require.NotNil(t, restarts, "supervisor_observer_restarts_total counter not found")

	// Find restart data point
	var found bool
	for _, dp := range restarts.DataPoints {
		attrs := attributesToMap(dp.Attributes)
		if attrs["observer"] == "failing-observer" && attrs["reason"] == "crash" {
			assert.GreaterOrEqual(t, dp.Value, int64(1), "restart counter should be >= 1")
			found = true
			break
		}
	}
	assert.True(t, found, "restart data point not found")

	cancel()
	<-errCh
}

// TestMetrics_RestartLatency tests restart latency histogram
func TestMetrics_RestartLatency(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test-supervisor")

	sup := New(Config{ShutdownTimeout: 1 * time.Second}, WithMeter(meter))

	var attempt int
	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		attempt++
		if attempt == 1 {
			return assert.AnError
		}
		<-ctx.Done()
		return nil
	}, WithRestartPolicy(5, 1*time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for restart
	time.Sleep(1500 * time.Millisecond)

	// Collect metrics
	rm := &metricdata.ResourceMetrics{}
	err := reader.Collect(context.Background(), rm)
	require.NoError(t, err)

	// Verify restart_latency_ms histogram
	latency := findHistogram(rm, "supervisor_restart_latency_ms")
	require.NotNil(t, latency, "supervisor_restart_latency_ms histogram not found")

	// Verify histogram has data
	var found bool
	for _, dp := range latency.DataPoints {
		attrs := attributesToMap(dp.Attributes)
		if attrs["observer"] == "test-observer" {
			assert.Greater(t, dp.Count, uint64(0), "latency histogram should have observations")
			found = true
			break
		}
	}
	assert.True(t, found, "latency data point not found")

	cancel()
	<-errCh
}

// TestMetrics_CircuitBreaker tests circuit breaker trigger counter
func TestMetrics_CircuitBreaker(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test-supervisor")

	sup := New(Config{ShutdownTimeout: 1 * time.Second}, WithMeter(meter))

	// Observer that always fails (will trigger circuit breaker)
	sup.SuperviseFunc("always-failing", func(ctx context.Context) error {
		return assert.AnError
	}, WithRestartPolicy(2, 1*time.Minute)) // Max 2 restarts

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for circuit breaker to trigger (initial + 2 restarts = 3 total, then disabled)
	time.Sleep(4 * time.Second)

	// Collect metrics
	rm := &metricdata.ResourceMetrics{}
	err := reader.Collect(context.Background(), rm)
	require.NoError(t, err)

	// Verify circuit_breaker_triggers_total counter
	breakers := findCounter(rm, "supervisor_circuit_breaker_triggers_total")
	require.NotNil(t, breakers, "supervisor_circuit_breaker_triggers_total counter not found")

	// Find circuit breaker trigger
	var found bool
	for _, dp := range breakers.DataPoints {
		attrs := attributesToMap(dp.Attributes)
		if attrs["observer"] == "always-failing" {
			assert.Equal(t, int64(1), dp.Value, "circuit breaker should trigger once")
			found = true
			break
		}
	}
	assert.True(t, found, "circuit breaker data point not found")

	cancel()
	<-errCh
}

// TestMetrics_ActiveObservers tests active observers gauge
func TestMetrics_ActiveObservers(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("test-supervisor")

	sup := New(Config{ShutdownTimeout: 1 * time.Second}, WithMeter(meter))

	// Add multiple observers
	sup.SuperviseFunc("observer-1", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})
	sup.SuperviseFunc("observer-2", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})
	sup.SuperviseFunc("observer-3", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Run(ctx)
	}()

	// Wait for observers to start
	time.Sleep(100 * time.Millisecond)

	// Collect metrics
	rm := &metricdata.ResourceMetrics{}
	err := reader.Collect(context.Background(), rm)
	require.NoError(t, err)

	// Verify active_observers gauge
	active := findGauge(rm, "supervisor_active_observers")
	require.NotNil(t, active, "supervisor_active_observers gauge not found")

	// Gauge should show 3 active observers
	require.Len(t, active.DataPoints, 1, "should have one data point")
	assert.Equal(t, int64(3), active.DataPoints[0].Value, "should have 3 active observers")

	cancel()
	<-errCh
}

// TestMetrics_NoMeter tests that supervisor works without metrics (nil meter)
func TestMetrics_NoMeter(t *testing.T) {
	sup := New(Config{ShutdownTimeout: 1 * time.Second}) // No meter

	sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should not panic, should work normally
	err := sup.Run(ctx)
	assert.NoError(t, err)
}

// Helper functions for metric extraction

func findCounter(rm *metricdata.ResourceMetrics, name string) *metricdata.Sum[int64] {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
					return &sum
				}
			}
		}
	}
	return nil
}

func findHistogram(rm *metricdata.ResourceMetrics, name string) *metricdata.Histogram[float64] {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if hist, ok := m.Data.(metricdata.Histogram[float64]); ok {
					return &hist
				}
			}
		}
	}
	return nil
}

func findGauge(rm *metricdata.ResourceMetrics, name string) *metricdata.Gauge[int64] {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if gauge, ok := m.Data.(metricdata.Gauge[int64]); ok {
					return &gauge
				}
			}
		}
	}
	return nil
}

func attributesToMap(attrs attribute.Set) map[string]string {
	m := make(map[string]string)
	iter := attrs.Iter()
	for iter.Next() {
		kv := iter.Attribute()
		m[string(kv.Key)] = kv.Value.AsString()
	}
	return m
}
