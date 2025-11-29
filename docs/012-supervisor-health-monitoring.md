# ADR 012: Supervisor Health Monitoring (Phase 2.2)

**Status**: In Progress
**Date**: 2025-11-26
**Author**: Claude + Yair

## Context

Supervisor Phase 2.1 (OTEL metrics) is complete with observability for lifecycle events. However, we lack **proactive health detection** - we only know an observer is unhealthy AFTER it crashes. We need health checks to detect degraded or unhealthy observers BEFORE they fail completely.

**Use Cases**:
- Observer is slow (degraded) but not crashed → Alert but keep running
- Observer is unresponsive (unhealthy) → Restart proactively
- Observer dependencies are down → Mark as degraded
- Observer resource usage is high → Mark as degraded

## Problem

How do we add health checking to the supervisor without:
1. Breaking existing observers (backward compatible)
2. Adding overhead to healthy observers
3. Blocking the supervision loop
4. Making health checks mandatory (optional feature)

## Design Principles (CLAUDE.md Compliance)

1. **Optional Health Checks** - Observers without health checks work normally
2. **Non-blocking** - Health checks run in background, don't block supervision
3. **Typed Status** - Use `HealthStatus` enum (not strings or bools)
4. **TDD Mandatory** - Tests FIRST (RED → GREEN → REFACTOR)
5. **Direct OTEL** - Health status exposed as metrics
6. **Small Functions** - Each function < 50 lines
7. **Zero map[string]interface{}** - Typed structs only

## Health Status Model

```go
// HealthStatus represents observer health state
type HealthStatus string

const (
    HealthStatusHealthy   HealthStatus = "healthy"   // All good ✅
    HealthStatusDegraded  HealthStatus = "degraded"  // Slow but working ⚠️
    HealthStatusUnhealthy HealthStatus = "unhealthy" // Needs restart ❌
)

// HealthCheckFunc checks if observer is healthy
type HealthCheckFunc func(ctx context.Context) HealthStatus
```

**Status Semantics**:
- **Healthy**: Observer is working normally, no action needed
- **Degraded**: Observer is slow or partially impaired, alert but continue
- **Unhealthy**: Observer is non-functional, restart immediately

## Architecture

### Phase 2.2 Scope (This PR)

**Health Check Loop** (per observer):
```
┌─────────────────────────────────────────────┐
│   Supervision Loop (existing)               │
│   - Runs observer                           │
│   - Handles crashes                         │
│   - Exponential backoff                     │
└─────────────────────────────────────────────┘
                  +
┌─────────────────────────────────────────────┐
│   Health Check Loop (NEW)                   │
│   - Runs every HealthCheckInterval (5s)     │
│   - Calls observer's HealthCheckFunc        │
│   - Non-blocking (runs in goroutine)        │
│   - If unhealthy → signal restart           │
└─────────────────────────────────────────────┘
```

**Flow**:
1. Observer registered with `WithHealthCheck(fn)`
2. Supervisor starts health check goroutine (if health check provided)
3. Every 5 seconds, call `HealthCheckFunc`
4. If status = `unhealthy` → cancel observer context → triggers restart
5. If status = `degraded` → log warning + emit metric
6. If status = `healthy` → continue normally

### Implementation Strategy

**1. Add Health Check Goroutine** (per observer):

```go
func (s *Supervisor) superviseObserver(obs *supervisedObserver) {
    defer s.wg.Done()

    // Start health check loop (if health check provided)
    healthCtx, healthCancel := context.WithCancel(s.ctx)
    defer healthCancel()

    if obs.config.healthCheckFn != nil {
        s.wg.Add(1)
        go s.healthCheckLoop(healthCtx, obs, healthCancel)
    }

    // Existing supervision logic...
    for {
        err := obs.runFn(s.ctx)
        // ... restart logic ...
    }
}
```

**2. Health Check Loop**:

```go
func (s *Supervisor) healthCheckLoop(ctx context.Context, obs *supervisedObserver, cancelObserver context.CancelFunc) {
    defer s.wg.Done()

    ticker := time.NewTicker(s.config.HealthCheckInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            status := obs.config.healthCheckFn(ctx)

            // Record health status metric
            if s.healthStatusGauge != nil {
                s.healthStatusGauge.Record(ctx, statusToInt(status), metric.WithAttributes(
                    attribute.String("observer", obs.name),
                    attribute.String("status", string(status)),
                ))
            }

            switch status {
            case HealthStatusHealthy:
                // All good, continue
                log.Debug().Str("observer", obs.name).Msg("health check passed")

            case HealthStatusDegraded:
                // Warn but continue
                log.Warn().Str("observer", obs.name).Msg("observer degraded")

            case HealthStatusUnhealthy:
                // Restart observer
                log.Error().Str("observer", obs.name).Msg("observer unhealthy - triggering restart")
                cancelObserver() // This will cause runFn to exit and restart
                return

            default:
                log.Error().Str("observer", obs.name).Str("status", string(status)).Msg("unknown health status")
            }

        case <-ctx.Done():
            return
        }
    }
}
```

**3. Updated observerConfig**:

```go
type observerConfig struct {
    // Auto-restart configuration (Phase 1)
    maxRestarts     int
    restartWindow   time.Duration
    restartCount    int
    lastRestartTime time.Time

    // Health check (Phase 2.2 - ACTIVE)
    healthCheckFn HealthCheckFunc

    // Resource limits (Phase 2.3 - reserved)
    maxCPU    float64
    maxMemory uint64

    // Dependencies (Phase 2.4 - reserved)
    dependencies []string
    optional     bool

    // Worker scaling (Phase 2.5 - reserved)
    minWorkers int
    maxWorkers int
}
```

## Testing Strategy (TDD!)

### Phase 2.2 Tests (`health_test.go`)

**RED → GREEN → REFACTOR**

```go
// TestHealth_HealthyObserver tests that healthy observers continue running
func TestHealth_HealthyObserver(t *testing.T) {
    // RED: Write test that fails (health check not implemented)
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
    assert.GreaterOrEqual(t, int(checks.Load()), 4)
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

    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    go sup.Run(ctx)

    // Wait for unhealthy restart
    require.Eventually(t, func() bool {
        return attempts.Load() >= 2 // Should restart at least once
    }, 2*time.Second, 50*time.Millisecond)

    cancel()
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
    sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
        started.Store(true)
        <-ctx.Done()
        return nil
    }, WithHealthCheck(healthCheck))

    ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
    defer cancel()

    err := sup.Run(ctx)
    require.NoError(t, err)

    // Observer should have started once (not restarted)
    assert.True(t, started.Load())
    // Should have been marked degraded multiple times
    assert.GreaterOrEqual(t, int(degradedCount.Load()), 4)
}

// TestHealth_NoHealthCheck tests observer without health check works normally
func TestHealth_NoHealthCheck(t *testing.T) {
    sup := New(Config{
        ShutdownTimeout:     1 * time.Second,
        HealthCheckInterval: 100 * time.Millisecond,
    })

    sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
        <-ctx.Done()
        return nil
    }) // No health check - should work normally

    ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
    defer cancel()

    err := sup.Run(ctx)
    assert.NoError(t, err)
}

// TestHealth_HealthCheckPanic tests health check panic handling
func TestHealth_HealthCheckPanic(t *testing.T) {
    sup := New(Config{
        ShutdownTimeout:     1 * time.Second,
        HealthCheckInterval: 100 * time.Millisecond,
    })

    healthCheck := func(ctx context.Context) HealthStatus {
        panic("health check panic!")
    }

    sup.SuperviseFunc("test-observer", func(ctx context.Context) error {
        <-ctx.Done()
        return nil
    }, WithHealthCheck(healthCheck))

    ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
    defer cancel()

    // Should not crash supervisor
    err := sup.Run(ctx)
    assert.NoError(t, err)
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

    go sup.Run(ctx)
    time.Sleep(250 * time.Millisecond)

    // Collect metrics
    rm := &metricdata.ResourceMetrics{}
    err := reader.Collect(context.Background(), rm)
    require.NoError(t, err)

    // Verify health status gauge
    healthGauge := findGauge(rm, "supervisor_observer_health_status")
    require.NotNil(t, healthGauge)

    // Should show healthy status
    var found bool
    for _, dp := range healthGauge.DataPoints {
        attrs := attributesToMap(dp.Attributes)
        if attrs["observer"] == "test-observer" && attrs["status"] == "healthy" {
            assert.Equal(t, int64(1), dp.Value) // 1 = healthy
            found = true
            break
        }
    }
    assert.True(t, found)

    cancel()
}
```

## Metrics

New metric for health status:

```go
// Gauge: Observer health status
supervisor_observer_health_status{observer="network", status="healthy|degraded|unhealthy"} = 1|2|3
```

**Status encoding**:
- 1 = healthy
- 2 = degraded
- 3 = unhealthy

## Implementation Checklist

- [ ] Design doc reviewed (this file)
- [ ] TDD RED: Write 6 failing health tests
- [ ] Add `healthCheckLoop()` goroutine
- [ ] Update `superviseObserver()` to start health loop
- [ ] Add health status metric (gauge)
- [ ] Implement panic recovery in health checks
- [ ] TDD GREEN: All tests passing
- [ ] TDD REFACTOR: Coverage > 80%
- [ ] Update existing tests to pass with health checks
- [ ] Create PR for review

## Benefits

1. **Proactive Failure Detection** - Catch unhealthy observers before crash
2. **Degraded State Awareness** - Know when observers are struggling
3. **Optional Feature** - Backward compatible, observers without health checks work normally
4. **Production Visibility** - Health status exposed as OTEL metrics
5. **Automatic Recovery** - Unhealthy observers auto-restart

## Future Work (Phase 2.3+)

- Resource-based health checks (CPU/memory thresholds)
- Dependency health checks (check if dependencies are available)
- Composite health checks (combine multiple signals)
- HTTP/gRPC health check endpoints

## References

- Kubernetes Liveness/Readiness Probes: https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/
- TAPIO CLAUDE.md: TDD mandatory, typed everything, direct OTEL
- Phase 2.1: OTEL Metrics (docs/011-supervisor-otel-metrics.md)
