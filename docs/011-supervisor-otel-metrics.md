# ADR 011: Supervisor OTEL Metrics Integration

**Status**: Proposed
**Date**: 2025-11-26
**Author**: Claude + Yair

## Context

Supervisor Phase 1 is complete with auto-restart, exponential backoff, and circuit breaker protection. We need observability to monitor supervisor behavior in production:

- How many observers are running?
- How often do observers restart?
- What's the restart latency distribution?
- Are circuit breakers triggering?
- How long does shutdown take?

**TAPIO Mandate**: Direct OpenTelemetry only - NO wrappers!

## Problem

We need to instrument the supervisor with OTEL metrics to answer:

1. **Lifecycle Metrics**: Observer starts, stops, restarts
2. **Health Metrics**: Circuit breaker triggers, restart failures
3. **Performance Metrics**: Restart latency, shutdown duration
4. **State Metrics**: Active observers, restart counts

## Design Principles

1. **Direct OTEL**: Use `go.opentelemetry.io/otel/metric` directly (no wrappers)
2. **Prometheus Standards**: Follow naming conventions (_total, _duration_ms, etc.)
3. **Low Overhead**: Metrics must not impact supervisor performance
4. **Testable**: All metrics must be verifiable in tests
5. **Optional**: Metrics are optional (nil meter = no-op)

## Metric Design

### 1. Observer Lifecycle Metrics

```go
// Counter: Observer starts
supervisor_observer_starts_total{observer="network", result="success|failure"}

// Counter: Observer stops
supervisor_observer_stops_total{observer="network", result="clean|timeout|error"}

// Counter: Observer restarts
supervisor_observer_restarts_total{observer="network", reason="crash|circuit_breaker"}
```

### 2. Circuit Breaker Metrics

```go
// Counter: Circuit breaker triggers
supervisor_circuit_breaker_triggers_total{observer="network"}

// Gauge: Current restart count per observer
supervisor_restart_count{observer="network"}
```

### 3. Performance Metrics

```go
// Histogram: Restart latency (time from crash to restart)
supervisor_restart_latency_ms{observer="network"}

// Histogram: Shutdown duration
supervisor_shutdown_duration_ms{result="success|timeout"}
```

### 4. State Metrics

```go
// Gauge: Active observers
supervisor_active_observers

// Gauge: Total registered observers
supervisor_registered_observers
```

## Implementation Strategy

### Phase 2.1: Core Metrics (This PR)

**Scope**: Essential lifecycle and state metrics

```go
type Supervisor struct {
    config    Config
    observers map[string]*supervisedObserver
    mu        sync.RWMutex
    wg        sync.WaitGroup
    ctx       context.Context
    cancel    context.CancelFunc
    hasRun    bool

    // OTEL metrics (Phase 2.1)
    meter               metric.Meter
    observerStarts      metric.Int64Counter
    observerStops       metric.Int64Counter
    observerRestarts    metric.Int64Counter
    circuitBreakers     metric.Int64Counter
    activeObservers     metric.Int64ObservableGauge
    registeredObservers metric.Int64ObservableGauge
    restartLatency      metric.Float64Histogram
}
```

**New Options**:
```go
// WithMeter configures OTEL metrics collection
func WithMeter(meter metric.Meter) Option

// WithMeterProvider creates meter from provider
func WithMeterProvider(provider metric.MeterProvider) Option
```

**Initialization**:
```go
func New(cfg Config, opts ...Option) *Supervisor {
    s := &Supervisor{
        config:    cfg,
        observers: make(map[string]*supervisedObserver),
    }

    // Apply options (including meter)
    for _, opt := range opts {
        opt(s)
    }

    // Initialize metrics if meter provided
    if s.meter != nil {
        s.initMetrics()
    }

    return s
}

func (s *Supervisor) initMetrics() error {
    var err error

    // Counters
    s.observerStarts, err = s.meter.Int64Counter(
        "supervisor_observer_starts_total",
        metric.WithDescription("Total number of observer starts"),
    )
    if err != nil {
        return fmt.Errorf("failed to create observerStarts counter: %w", err)
    }

    s.observerRestarts, err = s.meter.Int64Counter(
        "supervisor_observer_restarts_total",
        metric.WithDescription("Total number of observer restarts"),
    )
    if err != nil {
        return fmt.Errorf("failed to create observerRestarts counter: %w", err)
    }

    // Histogram
    s.restartLatency, err = s.meter.Float64Histogram(
        "supervisor_restart_latency_ms",
        metric.WithDescription("Observer restart latency in milliseconds"),
        metric.WithUnit("ms"),
    )
    if err != nil {
        return fmt.Errorf("failed to create restartLatency histogram: %w", err)
    }

    // Gauges (observable)
    _, err = s.meter.Int64ObservableGauge(
        "supervisor_active_observers",
        metric.WithDescription("Number of currently active observers"),
        metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
            s.mu.RLock()
            defer s.mu.RUnlock()
            o.Observe(int64(len(s.observers)))
            return nil
        }),
    )
    if err != nil {
        return fmt.Errorf("failed to create activeObservers gauge: %w", err)
    }

    return nil
}
```

**Instrumentation Points**:

1. **Observer Start**:
```go
func (s *Supervisor) superviseObserver(obs *supervisedObserver) {
    defer s.wg.Done()

    log.Info().Str("observer", obs.name).Msg("starting observer")

    // Record start
    if s.observerStarts != nil {
        s.observerStarts.Add(s.ctx, 1, metric.WithAttributes(
            attribute.String("observer", obs.name),
            attribute.String("result", "success"),
        ))
    }

    // ... rest of supervision logic
}
```

2. **Observer Restart**:
```go
// Record restart with latency
restartStart := time.Now()

// Record restart
if s.observerRestarts != nil {
    s.observerRestarts.Add(s.ctx, 1, metric.WithAttributes(
        attribute.String("observer", obs.name),
        attribute.String("reason", "crash"),
    ))
}

// Wait for backoff
select {
case <-time.After(backoff):
    attempt++

    // Record restart latency
    if s.restartLatency != nil {
        latency := time.Since(restartStart).Milliseconds()
        s.restartLatency.Record(s.ctx, float64(latency), metric.WithAttributes(
            attribute.String("observer", obs.name),
        ))
    }
}
```

3. **Circuit Breaker**:
```go
if obs.config.restartCount >= obs.config.maxRestarts {
    // Record circuit breaker trigger
    if s.circuitBreakers != nil {
        s.circuitBreakers.Add(s.ctx, 1, metric.WithAttributes(
            attribute.String("observer", obs.name),
        ))
    }

    log.Error().
        Str("observer", obs.name).
        Msg("circuit breaker triggered - observer disabled")
    return
}
```

## Testing Strategy (TDD!)

**Phase 2.1 Tests** (`metrics_test.go`):

```go
// TestMetrics_ObserverStarts tests start counter
func TestMetrics_ObserverStarts(t *testing.T) {
    // RED: Write test first
    reader := metric.NewManualReader()
    provider := metric.NewMeterProvider(metric.WithReader(reader))
    meter := provider.Meter("test")

    sup := New(DefaultConfig(), WithMeter(meter))

    sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
        <-ctx.Done()
        return nil
    })

    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    go sup.Run(ctx)
    time.Sleep(50 * time.Millisecond) // Wait for start

    // Collect metrics
    rm := &metricdata.ResourceMetrics{}
    err := reader.Collect(context.Background(), rm)
    require.NoError(t, err)

    // Verify start counter
    starts := findMetric(rm, "supervisor_observer_starts_total")
    require.NotNil(t, starts)
    assert.Equal(t, int64(1), starts.Sum.Value)
}

// TestMetrics_RestartLatency tests restart histogram
func TestMetrics_RestartLatency(t *testing.T) {
    // Test restart latency recording
}

// TestMetrics_CircuitBreaker tests circuit breaker counter
func TestMetrics_CircuitBreaker(t *testing.T) {
    // Test circuit breaker triggers
}

// TestMetrics_NoMeter tests nil meter (no-op)
func TestMetrics_NoMeter(t *testing.T) {
    sup := New(DefaultConfig()) // No meter
    // Should not panic, should work normally
}
```

## Metric Verification

**Local Testing**:
```bash
# Run tests with metrics
go test ./internal/runtime/supervisor/... -v -run TestMetrics

# Check metric output format
go test ./internal/runtime/supervisor/... -v -run TestMetrics_ObserverStarts
```

**Production Query Examples** (Prometheus):
```promql
# Observer restart rate
rate(supervisor_observer_restarts_total[5m])

# Circuit breaker triggers
increase(supervisor_circuit_breaker_triggers_total[1h])

# P95 restart latency
histogram_quantile(0.95, supervisor_restart_latency_ms)

# Active observers
supervisor_active_observers
```

## Benefits

1. **Visibility**: See exactly what the supervisor is doing
2. **Alerting**: Alert on high restart rates or circuit breaker triggers
3. **Debugging**: Correlate observer failures with system issues
4. **Capacity Planning**: Track active observer counts over time
5. **Performance**: Monitor restart latency distribution

## Implementation Checklist

- [ ] Design doc reviewed
- [ ] TDD: Write metric tests FIRST
- [ ] Add `meter` field to Supervisor
- [ ] Add `WithMeter()` option
- [ ] Implement `initMetrics()`
- [ ] Instrument observer starts
- [ ] Instrument observer restarts
- [ ] Instrument circuit breaker
- [ ] Add restart latency histogram
- [ ] Add active observers gauge
- [ ] All tests passing (>80% coverage)
- [ ] No TODOs or stubs
- [ ] Update README with metrics examples

## Future Work (Phase 2.2+)

- Shutdown duration histogram
- Observer stop counter (clean vs timeout)
- Per-observer restart counts (gauge)
- Resource usage metrics (CPU/memory)
- Dependency graph metrics

## References

- OTEL Go Metrics: https://opentelemetry.io/docs/instrumentation/go/manual/#metrics
- Prometheus Naming: https://prometheus.io/docs/practices/naming/
- TAPIO CLAUDE.md: Direct OTEL mandate (no wrappers)
