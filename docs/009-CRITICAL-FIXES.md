# ADR 009 - Critical Fixes Before Phase 1

> **NOTE**: NATS references in this document are outdated. TAPIO now uses **POLKU** (gRPC event gateway) instead of NATS.

**Status**: FIXED ✅
**Date**: 2025-01-05
**Related**: docs/009-observer-runtime-refactor.md

## Overview

This document addresses the 4 CRITICAL issues identified in design review before starting Phase 1 implementation.

---

## ✅ Critical Fix #1: EventProcessor Lifecycle

### Problem
EventProcessor interface was missing Config parameter in Setup(), needed for:
- K8s informers (need to access runtime config)
- eBPF programs (need sampling/backpressure settings)
- Context service connections (need config for endpoints)

### Solution (IMPLEMENTED)

```go
type EventProcessor interface {
    Name() string

    // Setup receives Config so processor can access runtime configuration
    Setup(ctx context.Context, cfg Config) error

    Process(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error)

    Teardown(ctx context.Context) error
}
```

**Implementation**: `internal/runtime/processor.go:47`
**Tests**: `TestObserverRuntime_Setup`

---

## ✅ Critical Fix #3: Multi-Emitter Error Handling

### Problem
Undefined behavior when OTLP succeeds but NATS fails. What should happen?

### Solution (IMPLEMENTED)

Added `IsCritical()` to Emitter interface:

```go
type Emitter interface {
    Emit(ctx context.Context, event *domain.ObserverEvent) error
    Name() string

    // IsCritical defines failure policy
    // true: Failure fails entire event emission (OTLP)
    // false: Failure logged but doesn't block (NATS, File)
    IsCritical() bool

    Close() error
}
```

**Behavior**:
- **Critical emitters** (OTLP): If they fail, ProcessEvent returns error
- **Non-critical emitters** (NATS, File): Failure logged, processing continues

**Example Usage**:
```go
// OSS: OTLP is critical (required for observability)
otlpEmitter := NewOTLPEmitter()  // IsCritical() returns true

// Enterprise: NATS is optional add-on (can degrade)
natsEmitter := NewNATSEmitter()  // IsCritical() returns false

runtime, _ := NewObserverRuntime(processor,
    WithEmitters(otlpEmitter, natsEmitter),
)

// If OTLP fails → ProcessEvent returns error
// If NATS fails → Logged, OTLP still gets event
```

**Implementation**: `internal/runtime/emitter.go:36`, `observer_runtime.go:86-100`
**Tests**: `TestObserverRuntime_CriticalEmitterFailure`, `TestObserverRuntime_NonCriticalEmitterFailure`

---

## 🔲 Critical Fix #2: Rollback Plan (DESIGN ONLY - Not Implemented Yet)

### Problem
Migration without rollback is dangerous. Need ability to revert instantly if issues arise.

### Solution (DESIGN - Implement in Phase 1.5)

**Dual Deployment Strategy**:

```yaml
# Phase 1.5: Run OLD + NEW side-by-side
apiVersion: v1
kind: ConfigMap
metadata:
  name: tapio-feature-flags
data:
  # Feature flag: Start at 0%, gradually increase
  network_use_new_runtime: "false"

---
# OLD DaemonSet (existing)
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tapio-network-observer-legacy
spec:
  selector:
    matchLabels:
      app: tapio-network-observer
      version: legacy

---
# NEW DaemonSet (new runtime)
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tapio-network-observer-v2
spec:
  selector:
    matchLabels:
      app: tapio-network-observer
      version: v2
```

**Rollout Process** (Phase 1.5):

1. **Week 4**: Deploy both old + new DaemonSets (0% traffic to new)
2. **Week 5**: Canary deployment (10% of nodes use new runtime)
   - Compare metrics: Event counts, latency, memory, CPU
   - Verify event structure matches (100 sample events)
3. **Week 6**: Production soak test (2 weeks)
   - Monitor error rates, memory leaks, performance
   - **CHECKPOINT**: Only proceed if no regressions
4. **Week 7+**: Gradual rollout (25% → 50% → 100%)

**Instant Rollback**:
```bash
# If issues arise, delete new DaemonSet
kubectl delete daemonset tapio-network-observer-v2

# Old DaemonSet still running - zero downtime
```

**Implementation Plan**:
- Phase 1: Build ObserverRuntime infrastructure
- Phase 1.5: Implement dual deployment for Network observer
- Use this pattern for all remaining observer migrations

---

## 🔲 Critical Fix #4: Failure Isolation Implementation (DESIGN ONLY - Not Implemented Yet)

### Problem
ADR says `failure_policy: isolate` but doesn't show how to prevent one observer crash from taking down entire binary.

### Solution (DESIGN - Implement in Phase 1.2)

**Panic Recovery Pattern**:

```go
// ObserverRuntime.Run() with panic recovery
func (r *ObserverRuntime) Run(ctx context.Context) error {
    // Setup panic recovery
    defer func() {
        if p := recover(); p != nil {
            // Log panic with stack trace
            fmt.Printf("PANIC in observer %s: %v\n", r.processor.Name(), p)

            // Mark observer unhealthy
            if r.health != nil {
                r.health.MarkUnhealthy(fmt.Sprintf("panic: %v", p))
            }

            // Increment panic metric
            // metrics.observer_panics_total.Inc()

            // DO NOT crash binary - isolate failure
        }
    }()

    // Normal runtime logic...
    return r.runUnsafe(ctx)
}
```

**Failure Policies** (from Config):

```go
type FailurePolicy string

const (
    // FailPolicyIsolate: Recover from panic, mark unhealthy, continue
    // Other observers keep running (RECOMMENDED for production)
    FailPolicyIsolate FailurePolicy = "isolate"

    // FailPolicyRestart: Retry with exponential backoff
    FailPolicyRestart FailurePolicy = "restart"

    // FailPolicyFailFast: Crash entire binary on first failure
    // Use for critical observers that MUST work
    FailPolicyFailFast FailurePolicy = "fail_fast"
)
```

**Implementation Example**:

```go
// Network observer panics → Isolated
networkRuntime, _ := NewObserverRuntime(networkProcessor,
    WithFailurePolicy(FailPolicyIsolate),  // Don't crash binary
)

// Deployments observer panics → Also isolated
deploymentsRuntime, _ := NewObserverRuntime(deploymentsProcessor,
    WithFailurePolicy(FailPolicyIsolate),
)

// Both run in same binary, failures don't propagate
go networkRuntime.Run(ctx)
go deploymentsRuntime.Run(ctx)

// If network observer panics:
// - networkRuntime marked unhealthy
// - deploymentsRuntime keeps running
// - Binary stays alive
```

**Health Endpoint**:
```bash
$ curl localhost:8080/health
{
  "observers": {
    "network": {"healthy": false, "reason": "panic: nil pointer dereference"},
    "deployments": {"healthy": true}
  },
  "overall": "degraded"
}
```

**Implementation Plan**:
- Phase 1.2: Add panic recovery to ObserverRuntime.Run()
- Phase 1.2: Implement FailPolicyIsolate, FailPolicyFailFast
- Phase 1.8: Wire up health endpoint
- Phase 2: Test with real observer migrations

---

## Test Plan (Per Observer Migration)

Addresses design review concern #6.

### 1. Unit Tests (MANDATORY)
```bash
# All existing tests must pass
go test ./internal/observers/network/... -v

# NEW runtime tests
go test ./internal/runtime/... -v -cover
# Target: 80%+ coverage
```

### 2. Integration Test (MANDATORY)
```bash
# Test: Observer → NATS → Verify event structure
go test ./internal/observers/network/... -run TestNetworkObserver_E2E

# Verify:
# - Events reach NATS
# - Event structure matches domain.ObserverEvent
# - No data loss
```

### 3. Load Test (MANDATORY)
```bash
# Generate 10K events/sec for 5 minutes
go test ./internal/observers/network/... -run TestNetworkObserver_Load

# Verify:
# - No OOM (memory stays < 500MB)
# - No CPU spikes (< 0.5 cores steady state)
# - No event drops (backpressure queue working)
# - p99 latency < 10ms
```

### 4. Failure Test (MANDATORY)
```bash
# Kill observer, verify restart + recovery
kubectl delete pod tapio-network-observer-xyz

# Verify:
# - K8s restarts pod
# - Observer reconnects to NATS
# - No event loss (queue preserved)
# - Health endpoint returns to healthy
```

### 5. Comparison Test (MANDATORY)
```bash
# Run old + new side-by-side, compare output
go test ./internal/observers/network/... -run TestNetworkObserver_Comparison

# Sample 100 events from each:
# - Event types match
# - Event subtypes match
# - Payload structure identical
# - Timestamp within 100ms
```

---

## Performance SLOs (Per Observer)

Addresses design review concern #7.

### Latency
- **Event processing**: p99 < 10ms (from eBPF → domain.ObserverEvent)
- **End-to-end**: p99 < 50ms (from eBPF → NATS publish)

### Resource Usage
- **Memory**: < 500MB steady state (per observer)
- **CPU**: < 0.5 cores at 1K events/sec
- **Disk**: < 100MB (logs + state)

### Throughput
- **Backpressure threshold**: No drops below 10K events/sec
- **Queue depth**: < 5000 events at 1K events/sec steady state

### Reliability
- **Crash rate**: < 0.1% (1 crash per 1000 hours)
- **Data loss**: 0% for critical events (link_failure, oom_killed)
- **Recovery time**: < 30s after pod restart

### Monitoring
```promql
# Latency
histogram_quantile(0.99,
  rate(observer_processing_duration_ms_bucket[5m])
) < 10

# Memory
container_memory_usage_bytes{pod=~"tapio-.*-observer.*"} < 500 * 1024 * 1024

# CPU
rate(container_cpu_usage_seconds_total{pod=~"tapio-.*-observer.*"}[5m]) < 0.5

# Drop rate
rate(observer_events_dropped_total[5m]) /
rate(observer_events_received_total[5m]) < 0.01
```

---

## Timeline Adjustment (Addresses Concern #5)

Original: 10 weeks
**Revised: 14 weeks** (add Week 0 + extend Phase 1.5)

### Week 0: Design Fixes (THIS WEEK) ✅
- [x] Fix EventProcessor.Setup signature
- [x] Add Emitter.IsCritical()
- [x] Design rollback strategy
- [x] Design failure isolation pattern
- [x] Document test plan
- [x] Define SLOs

### Phase 1 (Week 1-3): Infrastructure (APPROVED)
- Build ObserverRuntime with all production features
- Implement panic recovery (FailPolicyIsolate)
- Add OTEL metrics
- Create test observer

### Phase 1.5 (Week 4-6): Validation (CHECKPOINT)
- Week 4: Migrate Network observer
- Week 5: Dev/Staging dual deployment
- Week 6: Production soak test (2 weeks)
- **DECISION POINT**: Only proceed if:
  - All tests pass
  - No regressions vs old observer
  - SLOs met for 2 weeks

### Phase 2 (Week 7-13): Remaining Observers (CONDITIONAL)
- Only start if Phase 1.5 successful
- Migrate 5 remaining observers, one per week
- Use same dual deployment pattern

---

## Summary

### Fixed (Implemented Code) ✅
1. **EventProcessor.Setup(ctx, cfg)** - Config parameter added
2. **Emitter.IsCritical()** - Failure policy defined

### Fixed (Design Complete) 🎯
3. **Rollback Plan** - Dual deployment strategy documented
4. **Failure Isolation** - Panic recovery pattern designed

### Ready to Start Phase 1? YES ✅

**All 4 critical issues are either implemented or have detailed designs.**

**Next Steps**:
1. Continue Phase 1 implementation (EBPFRuntime, K8sRuntime, etc.)
2. Implement panic recovery in Phase 1.2
3. Execute dual deployment in Phase 1.5
4. Measure, validate, decide at checkpoint

---

**Don't let perfect be the enemy of good. Fix critical issues, start Phase 1, learn and adjust.** 🎯
