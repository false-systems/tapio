# TAPIO Lean Observer Refactor Plan

**Goal**: Fast, lean observers with minimal abstractions

**Principle**: Dependency injection over inheritance. Each observer is just `func(ctx) error`.

---

## Phase 1: Delete Dead Code (30 min)

Remove unused code to reduce noise before refactoring.

### 1.1 Delete ObserverCore (unused)
```bash
rm internal/base/observer_core.go
rm internal/base/observer_core_test.go
```
**Lines removed**: ~330

### 1.2 Delete Legacy Intelligence Adapter
**File**: `pkg/intelligence/service.go`
- Delete `IntelligenceService` interface (lines 404-410)
- Delete `NewIntelligenceService()` function (lines 411-421)
- Delete `legacyAdapter` struct and method (lines 424-432)
- Update tests to use `New(Config{})` directly

**Lines removed**: ~50

### 1.3 Delete Old OTEL Metrics (after Phase 3)
```bash
rm internal/base/metrics.go
rm internal/base/metrics_builder.go
rm internal/base/metrics_test.go
rm internal/base/metrics_builder_test.go
```
**Lines removed**: ~600

---

## Phase 2: Define Lean Observer Pattern (1 hour)

### 2.1 Create Observer Interface
**File**: `internal/base/observer.go` (replace existing)

```go
package base

import "context"

// Observer is a simple run function - that's it
type Observer interface {
    Run(ctx context.Context) error
}

// ObserverFunc adapts a function to Observer interface
type ObserverFunc func(ctx context.Context) error

func (f ObserverFunc) Run(ctx context.Context) error {
    return f(ctx)
}
```

### 2.2 Shared Dependencies Struct
**File**: `internal/base/deps.go` (NEW)

```go
package base

import "github.com/yairfalse/tapio/pkg/intelligence"

// Deps holds shared dependencies for all observers
// Injected at construction, not embedded
type Deps struct {
    Metrics *PromObserverMetrics
    Emitter intelligence.Service
}

// NewDeps creates shared dependencies
func NewDeps(reg prometheus.Registerer, emitterCfg intelligence.Config) (*Deps, error) {
    metrics := NewPromObserverMetrics(reg)
    emitter, err := intelligence.New(emitterCfg)
    if err != nil {
        return nil, err
    }
    return &Deps{Metrics: metrics, Emitter: emitter}, nil
}
```

---

## Phase 3: Migrate Network Observer (Template) (2 hours)

Network observer becomes the template for all others.

### 3.1 Before (current)
```go
type NetworkObserver struct {
    *base.BaseObserver           // 200+ lines of inheritance
    config  Config
    // ... observer-specific fields
}

func NewNetworkObserver(name string, config Config) (*NetworkObserver, error) {
    baseObs, err := base.NewBaseObserver(name)  // Complex
    // ...
}
```

### 3.2 After (lean)
```go
type NetworkObserver struct {
    name    string
    config  Config
    deps    *base.Deps           // Injected, ~20 lines
    // ... observer-specific fields
}

func New(config Config, deps *base.Deps) *NetworkObserver {
    return &NetworkObserver{
        name:   "network",
        config: config,
        deps:   deps,
    }
}

func (o *NetworkObserver) Run(ctx context.Context) error {
    // Load eBPF
    objs, err := bpf.LoadNetworkObjects(nil)
    if err != nil {
        return err
    }
    defer objs.Close()

    // Attach to tracepoints
    // ...

    // Simple event loop
    reader, _ := ringbuf.NewReader(objs.Events)
    defer reader.Close()

    for {
        select {
        case <-ctx.Done():
            return nil
        default:
            record, err := reader.Read()
            if err != nil {
                continue
            }
            o.processEvent(record)
        }
    }
}

func (o *NetworkObserver) processEvent(record ringbuf.Record) {
    // Parse event
    // Enrich with K8s context
    // Record metrics: o.deps.Metrics.RecordEvent("network", "connection")
    // Emit event: o.deps.Emitter.Emit(ctx, event)
}
```

---

## Phase 4: Migrate Remaining Observers (3 hours)

Apply same pattern to all observers:

| Observer | Complexity | Notes |
|----------|------------|-------|
| container | Medium | Has cgroup monitor |
| container-api | Low | K8s API only |
| container-runtime | Medium | eBPF |
| node | Medium | eBPF + PMC |
| scheduler | Low | K8s events |
| deployments | Low | K8s API |

Each migration:
1. Remove `*base.BaseObserver` embedding
2. Add `deps *base.Deps` field
3. Inject deps in constructor
4. Replace `o.RecordEvent()` with `o.deps.Metrics.RecordEvent()`
5. Replace `o.SendObserverEvent()` with `o.deps.Emitter.Emit()`
6. Simplify `Start()` to `Run()` (no pipeline abstraction)

---

## Phase 5: Delete BaseObserver (30 min)

Once all observers migrated:
```bash
rm internal/base/observer.go          # Old 288-line version
rm internal/base/observer_test.go
rm internal/base/pipeline.go
rm internal/base/pipeline_test.go
```

Keep new lean files:
- `internal/base/observer.go` (new, ~20 lines)
- `internal/base/deps.go` (new, ~30 lines)
- `internal/base/prom_metrics.go` (exists)
- `internal/base/registry.go` (exists)

---

## Phase 6: Update Supervisor Integration (1 hour)

### 6.1 Main.go Pattern
```go
func main() {
    // Create shared deps once
    deps, err := base.NewDeps(base.GlobalRegistry, intelligence.Config{
        Tier: intelligence.TierFree,
    })

    // Create observers
    networkObs := network.New(networkCfg, deps)
    containerObs := container.New(containerCfg, deps)

    // Register with supervisor
    sup := supervisor.New(supervisor.DefaultConfig())
    sup.SuperviseFunc("network", networkObs.Run)
    sup.SuperviseFunc("container", containerObs.Run)

    // Run
    sup.Run(ctx)
}
```

---

## Summary

| Phase | What | Lines Removed | Lines Added | Time |
|-------|------|---------------|-------------|------|
| 1 | Delete dead code | ~980 | 0 | 30m |
| 2 | Lean observer pattern | 0 | ~50 | 1h |
| 3 | Migrate network (template) | ~100 | ~80 | 2h |
| 4 | Migrate 6 observers | ~600 | ~480 | 3h |
| 5 | Delete BaseObserver | ~500 | 0 | 30m |
| 6 | Update main/supervisor | ~50 | ~30 | 1h |
| **Total** | | **~2230** | **~640** | **8h** |

**Net reduction**: ~1600 lines of code

---

## What We Keep

- `internal/base/prom_metrics.go` - Native Prometheus metrics
- `internal/base/registry.go` - Global registry
- `internal/base/ebpf_manager.go` - eBPF lifecycle helper
- `internal/base/telemetry.go` - Traces/logs (simplified)
- `internal/runtime/supervisor/` - Observer lifecycle
- `pkg/intelligence/` - Event emission
- All eBPF code unchanged

---

## What We Delete

- `internal/base/observer.go` (old, 288 lines)
- `internal/base/observer_core.go` (unused, 80 lines)
- `internal/base/pipeline.go` (abstraction, ~150 lines)
- `internal/base/metrics.go` (OTEL SDK, ~200 lines)
- `internal/base/metrics_builder.go` (OTEL SDK, ~150 lines)
- Legacy intelligence adapter (~50 lines)
- Associated tests (~700 lines)

---

## Order of Execution

1. **Phase 1.1-1.2**: Delete dead code (quick wins)
2. **Phase 2**: Create new lean pattern
3. **Phase 3**: Migrate network observer (prove pattern works)
4. **Run tests** - verify nothing broke
5. **Phase 4**: Migrate remaining observers
6. **Phase 5**: Delete old BaseObserver
7. **Phase 6**: Wire up main.go
8. **Phase 1.3**: Delete old metrics (now safe)

---

**Result**: Lean observers that are just `func(ctx) error` with injected dependencies. No inheritance, no pipeline abstraction, direct metrics, simple loops.
