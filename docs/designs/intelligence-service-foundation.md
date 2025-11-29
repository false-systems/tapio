# Intelligence Service Foundation - Design Session

**Status**: Ready to Implement
**Date**: 2025-11-30
**Author**: Claude + Yair
**Related**: Doc 010 (Intelligence Service), ADR 009 (Observer Runtime)
**Timeline**: 1-2 days (~150 lines, 6 small commits)

---

## Problem

`emitter_nats.go` currently lives in `internal/runtime/` (Level 4 - Interfaces) but violates the 5-level architecture:

**Current Reality:**
```
internal/runtime/emitter_nats.go  ← Level 4 (Interfaces)
                                   ❌ WRONG! Should be Level 2
```

**Why This Is Wrong:**
1. **Runtime should be dumb** - It's the infrastructure layer, not the intelligence layer
2. **NATS emitter is FREE tier specific** - Level 4 interfaces should be deployment-agnostic
3. **Blocks Observer Runtime refactor** - Can't refactor observers while NATS is in wrong layer
4. **Prevents tier separation** - FREE vs ENTERPRISE tiers can't be cleanly separated

**Architecture Debt Impact:**
- Every new observer imports this pattern (technical debt compounds)
- Observer Runtime refactor (ADR 009) blocked until this is fixed
- Can't implement FREE vs ENTERPRISE tiers correctly

---

## Solution

Create **Intelligence Service Foundation** (Level 2) to fix the layer violation:

**Target Architecture:**
```
Level 2: Intelligence Service (FREE tier boundary)
  ├── pkg/intelligence/            ← NEW!
  │   ├── service.go               (Interface + FREE impl)
  │   ├── nats_bridge.go           (Move emitter_nats.go here)
  │   └── service_test.go

Level 4: Interfaces (deployment modes)
  ├── internal/runtime/
  │   ├── emitter_otlp.go          ✅ Correct
  │   ├── emitter_file.go          ✅ Correct
  │   └── emitter_nats.go          ❌ DELETE (moved to Level 2)
```

---

## Flow Diagram

### Current (BROKEN)
```
┌──────────┐     ┌─────────────────┐     ┌──────────┐
│Observer  │────▶│Runtime (Level 4)│────▶│  NATS    │
│  Event   │     │ emitter_nats.go │     │ Stream   │
└──────────┘     └─────────────────┘     └──────────┘
                        ❌ WRONG LAYER!
```

### Target (CORRECT)
```
┌──────────┐     ┌─────────────────────┐     ┌──────────┐
│Observer  │────▶│Intelligence (Lvl 2) │────▶│  NATS    │
│  Event   │     │  nats_bridge.go     │     │ Stream   │
└──────────┘     └─────────────────────┘     └──────────┘
                        ✅ CORRECT LAYER!

Runtime (Level 4) no longer knows about NATS
```

---

## Interface Design

### Intelligence Service Interface (Level 2)

```go
// pkg/intelligence/service.go

package intelligence

import (
    "context"
    "github.com/yairfalse/tapio/pkg/domain"
)

// IntelligenceService processes ObserverEvents and routes them
// to appropriate outputs based on deployment tier.
//
// Deployment Tiers:
// - Simple: No intelligence service (observers use OTLPEmitter directly)
// - FREE: Intelligence service with NATS bridge only
// - ENTERPRISE: Intelligence service with enrichment + NATS
type IntelligenceService interface {
    // Process handles an incoming ObserverEvent
    Process(ctx context.Context, event domain.ObserverEvent) error

    // Shutdown gracefully stops the service
    Shutdown(ctx context.Context) error

    // Name returns the service identifier
    Name() string
}

// Config holds intelligence service configuration
type Config struct {
    // Mode determines the tier (free, enterprise)
    Mode string

    // NATS connection details (for FREE tier)
    NATSURLs     []string
    NATSKVBucket string

    // Context service endpoint (for ENTERPRISE tier)
    ContextServiceAddr string
}
```

### FREE Tier Implementation (NATS Bridge)

```go
// pkg/intelligence/nats_bridge.go

package intelligence

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/nats-io/nats.go"
    "github.com/yairfalse/tapio/pkg/domain"
)

// NATSBridge implements IntelligenceService for FREE tier.
// It simply forwards ObserverEvents to NATS KV bucket.
//
// This is the "dumb bridge" - no enrichment, just forwarding.
// ENTERPRISE tier would add K8s context enrichment here.
type NATSBridge struct {
    nc     *nats.Conn
    kv     nats.KeyValue
    bucket string
}

// NewNATSBridge creates a FREE tier intelligence service
func NewNATSBridge(cfg Config) (*NATSBridge, error) {
    nc, err := nats.Connect(cfg.NATSURLs...)
    if err != nil {
        return nil, fmt.Errorf("failed to connect to NATS: %w", err)
    }

    js, err := nc.JetStream()
    if err != nil {
        return nil, fmt.Errorf("failed to get JetStream context: %w", err)
    }

    kv, err := js.KeyValue(cfg.NATSKVBucket)
    if err != nil {
        return nil, fmt.Errorf("failed to get KV bucket: %w", err)
    }

    return &NATSBridge{
        nc:     nc,
        kv:     kv,
        bucket: cfg.NATSKVBucket,
    }, nil
}

// Process forwards ObserverEvent to NATS KV
func (b *NATSBridge) Process(ctx context.Context, event domain.ObserverEvent) error {
    data, err := json.Marshal(event)
    if err != nil {
        return fmt.Errorf("failed to marshal event: %w", err)
    }

    key := fmt.Sprintf("%s.%s", event.Type, event.ID)
    if _, err := b.kv.Put(key, data); err != nil {
        return fmt.Errorf("failed to put event in KV: %w", err)
    }

    return nil
}

// Shutdown closes NATS connection
func (b *NATSBridge) Shutdown(ctx context.Context) error {
    b.nc.Close()
    return nil
}

// Name returns service identifier
func (b *NATSBridge) Name() string {
    return "nats-bridge-free"
}
```

---

## Migration Strategy

### Step 1: Create Intelligence Package (Commit 1)
**RED Phase:**
```go
// pkg/intelligence/service_test.go
func TestNATSBridge_Process(t *testing.T) {
    bridge := NewNATSBridge(Config{...})  // ❌ Undefined

    event := domain.ObserverEvent{Type: "tcp_connect", ID: "test-1"}
    err := bridge.Process(context.Background(), event)

    require.NoError(t, err)
}
```

**Run:** `go test ./pkg/intelligence/`
```
# pkg/intelligence
./service_test.go:10:13: undefined: NewNATSBridge
FAIL    pkg/intelligence [build failed]
```
✅ **RED confirmed** - Test doesn't compile

**GREEN Phase:**
- Create `pkg/intelligence/service.go` (interface only)
- Create `pkg/intelligence/nats_bridge.go` (minimal implementation)
- Test passes

**Commit:**
```bash
git add pkg/intelligence/
git commit -m "feat(intelligence): add service interface and NATS bridge

- Create IntelligenceService interface (Level 2)
- Implement NATSBridge for FREE tier
- Tests: TestNATSBridge_Process passing

Fixes layer violation (emitter_nats.go was in Level 4)"
```

**Size:** ~80 lines

---

### Step 2: Move emitter_nats.go Logic (Commit 2)
**RED Phase:**
```go
// pkg/intelligence/nats_bridge_test.go
func TestNATSBridge_ProcessNetworkEvent(t *testing.T) {
    // Copy test from internal/runtime/emitter_nats_test.go
    bridge := NewNATSBridge(cfg)

    event := domain.ObserverEvent{
        Type: "tcp_connect",
        Data: domain.NetworkEventData{...},
    }

    err := bridge.Process(ctx, event)
    require.NoError(t, err)

    // Verify event in NATS KV
    val, err := bridge.kv.Get("tcp_connect.test-1")
    require.NoError(t, err)
    // ... assertions
}
```

**GREEN Phase:**
- Copy logic from `emitter_nats.go` to `nats_bridge.go`
- Update to use `IntelligenceService.Process()` interface
- All tests pass

**Commit:**
```bash
git commit -m "feat(intelligence): migrate NATS emitter logic

- Move event marshaling from runtime to intelligence
- Copy tests from emitter_nats_test.go
- All tests passing (150 tests, 0 failures)

Part 2 of layer violation fix"
```

**Size:** ~40 lines

---

### Step 3: Update Observers to Use Intelligence Service (Commit 3)
**RED Phase:**
```go
// cmd/observers/network/main.go (after change)
func main() {
    // OLD (Level 4 - Runtime)
    emitter := runtime.NewNATSEmitter(...)  // ❌ Will delete

    // NEW (Level 2 - Intelligence)
    intelligence := intelligence.NewNATSBridge(...)  // ❌ Doesn't compile yet

    rt, _ := runtime.NewObserverRuntime(
        processor,
        runtime.WithIntelligence(intelligence),  // ❌ Doesn't exist
    )
}
```

**GREEN Phase:**
- Add `runtime.WithIntelligence(IntelligenceService)` option
- Update all observers to use intelligence service
- Remove `runtime.NewNATSEmitter()` calls

**Commit:**
```bash
git commit -m "feat(observers): use intelligence service instead of NATS emitter

- Update network observer to use IntelligenceService
- Add runtime.WithIntelligence() option
- Remove direct NATS dependency from observers

Part 3 of layer violation fix"
```

**Size:** ~30 lines

---

### Step 4: Delete emitter_nats.go (Commit 4) 🔥
**RED Phase:**
```bash
# Delete the file
rm internal/runtime/emitter_nats.go
rm internal/runtime/emitter_nats_test.go

# Run tests - should still pass (now using intelligence service)
go test ./...
```

**GREEN Phase:**
- Tests still pass (observers now use intelligence service)
- Verify no import errors

**Commit:**
```bash
git commit -m "refactor(runtime): delete emitter_nats.go (moved to intelligence)

Deleted files:
- internal/runtime/emitter_nats.go (89 lines)
- internal/runtime/emitter_nats_test.go (112 lines)

Logic now in:
- pkg/intelligence/nats_bridge.go ✅

Layer violation FIXED! 🎉

Before: Runtime (Level 4) publishing to NATS ❌
After: Intelligence Service (Level 2) publishing to NATS ✅"
```

**Size:** -201 lines! 🔥

---

### Step 5: Update Documentation (Commit 5)
**Update:**
- `docs/010-intelligence-service-implementation-plan.md`
- `ARCHITECTURE.md` (5-level hierarchy diagram)
- Add migration guide for users

**Commit:**
```bash
git commit -m "docs: update architecture for intelligence service

- Mark emitter_nats.go migration as complete
- Update 5-level architecture diagram
- Add IntelligenceService to Level 2 docs"
```

**Size:** ~20 lines

---

### Step 6: Add Metrics (Commit 6)
**RED Phase:**
```go
func TestNATSBridge_Metrics(t *testing.T) {
    reg := prometheus.NewRegistry()
    bridge := NewNATSBridge(cfg, WithRegistry(reg))

    bridge.Process(ctx, event)

    // Verify metrics recorded
    metrics, _ := reg.Gather()
    assert.Contains(t, metrics, "intelligence_events_processed_total")
}
```

**GREEN/REFACTOR Phase:**
- Add Prometheus metrics to NATSBridge
- Record events processed, errors, latency

**Commit:**
```bash
git commit -m "feat(intelligence): add Prometheus metrics

Metrics:
- intelligence_events_processed_total{tier=\"free\"}
- intelligence_errors_total{tier=\"free\"}
- intelligence_processing_duration_seconds

Tests: TestNATSBridge_Metrics passing"
```

**Size:** ~30 lines

---

## Verification Checklist

Before merging, verify:

- [ ] **Tests pass:** `go test ./pkg/intelligence/`
- [ ] **No runtime NATS imports:** `grep -r "nats" internal/runtime/*.go` (should be empty except emitter interface)
- [ ] **Observers use intelligence:** `grep -r "IntelligenceService" cmd/observers/`
- [ ] **Metrics work:** Check Prometheus endpoint shows `intelligence_*` metrics
- [ ] **Documentation updated:** `docs/010-*.md` and `ARCHITECTURE.md`
- [ ] **Clean git history:** 6 commits, each <50 lines, TDD workflow

---

## Expected Outcome

**Before (Current State):**
```
internal/runtime/
├── emitter.go                    Interface (89 lines)
├── emitter_nats.go               ❌ WRONG LAYER (89 lines)
├── emitter_nats_test.go          ❌ WRONG LAYER (112 lines)
├── emitter_otlp.go               ✅ Correct (126 lines)
└── emitter_file.go               ✅ Correct (57 lines)

Total: 473 lines
Layer violation: NATS in Level 4 ❌
```

**After (Target State):**
```
pkg/intelligence/
├── service.go                    Interface (40 lines)
├── nats_bridge.go                FREE tier impl (80 lines)
├── nats_bridge_test.go           Tests (120 lines)
└── metrics.go                    Prometheus (30 lines)

internal/runtime/
├── emitter.go                    Interface (89 lines)
├── emitter_otlp.go               ✅ Correct (126 lines)
└── emitter_file.go               ✅ Correct (57 lines)

Total: 542 lines (+69 lines for proper abstraction)
Layer violation: FIXED ✅
Architecture: Compliant with 5-level hierarchy ✅
```

---

## Benefits

### Immediate Wins
1. **Architecture Compliance** - Fixes layer violation
2. **Enables Observer Runtime Refactor** - Clean layers unlock ADR 009
3. **Tier Separation** - Clear boundary for FREE vs ENTERPRISE
4. **Better Testability** - Intelligence service can be mocked

### Future Enablement
1. **ENTERPRISE Tier** - Add enrichment in `intelligence/enricher.go`
2. **Multiple Outputs** - Intelligence can route to NATS + OTLP + File
3. **Event Transformation** - ObserverEvent → TapioEvent in intelligence layer
4. **Context Integration** - K8s metadata enrichment in ENTERPRISE tier

---

## Risk Mitigation

### Risk 1: Breaking existing deployments
**Mitigation:**
- Keep `emitter_nats.go` until all observers migrated
- Add deprecation warning
- Update deployment YAMLs in same PR

### Risk 2: Performance regression
**Mitigation:**
- Benchmark before/after
- Intelligence service is just a thin wrapper
- Same NATS operations, different layer

### Risk 3: Test coverage drops
**Mitigation:**
- Copy all tests from `emitter_nats_test.go`
- Add new integration tests
- Verify 80%+ coverage maintained

---

## Success Criteria

1. ✅ All tests pass (`go test ./...`)
2. ✅ `emitter_nats.go` deleted
3. ✅ `pkg/intelligence/` package exists with clean interface
4. ✅ All observers use `IntelligenceService`
5. ✅ Architecture documentation updated
6. ✅ Metrics working (Prometheus endpoint shows `intelligence_*`)
7. ✅ Clean TDD commit history (6 commits, each <50 lines)

---

## Next Steps After Foundation

Once Intelligence Service Foundation is complete:

1. **Observer Runtime Refactor (ADR 009)** - Now unblocked!
2. **ENTERPRISE Tier** - Add `intelligence/enricher.go` for K8s context
3. **Multiple Outputs** - Route events to NATS + OTLP simultaneously
4. **Cleanup map[string]interface{}** - Use typed structs throughout

---

## Timeline

**Day 1 (Morning):**
- Commit 1: Create intelligence package (~1 hour)
- Commit 2: Move NATS logic (~1 hour)
- Commit 3: Update observers (~1 hour)

**Day 1 (Afternoon):**
- Commit 4: Delete emitter_nats.go (~30 min)
- Commit 5: Update docs (~30 min)
- Commit 6: Add metrics (~1 hour)

**Day 2 (Buffer):**
- Integration testing
- PR review fixes
- Deployment YAML updates

**Total:** 1-2 days for ~150 lines of code

---

**Let's fix this layer violation and unblock the Observer Runtime refactor! 🚀**

False Systems - Building observability tools that actually make sense 🇫🇮
