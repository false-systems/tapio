# Pipeline Wiring - Design Session

**Status**: Ready to Implement
**Date**: 2025-12-05
**Author**: Claude + Yair
**Related**: Intelligence Service Foundation, README audit
**Timeline**: 1-2 days (~225 lines, 4 tasks)

---

## Problem

We have good components that aren't connected:

```
BUILT:                              MISSING:
─────                               ───────
✅ Observers detect patterns        ❌ Events don't flow to Intelligence Service
✅ OTLP emitter works               ❌ No NATSEmitter in runtime
✅ Context Service works            ❌ Observers don't call it for enrichment
✅ Intelligence Service works       ❌ Nothing sends it events
✅ Ahti receives from NATS          ❌ Tapio never publishes to NATS
```

**Result:** Simple tier works (OTLP). Free tier broken (no NATS flow). Enterprise tier broken (no Ahti connection).

---

## Solution

Wire the pipeline with 4 tasks:

```
CURRENT:
  Observers ──▶ OTLPEmitter ──▶ Collector
                    ✅

TARGET:
  Observers ──▶ Context Service ──▶ Emitters
                   (enrich)            │
                                       ├──▶ OTLPEmitter ──▶ Collector
                                       │
                                       └──▶ NATSEmitter ──▶ Intelligence ──▶ NATS ──▶ Ahti
                                                (new!)         (exists)
```

---

## Task 1: NATSEmitter

**File:** `internal/runtime/emitter_nats.go`

**Purpose:** Adapter that wraps Intelligence Service as an Emitter.

### Interface

```go
// internal/runtime/emitter_nats.go

package runtime

import (
    "context"

    "github.com/yairfalse/tapio/pkg/domain"
    "github.com/yairfalse/tapio/pkg/intelligence"
)

// NATSEmitter wraps IntelligenceService as an Emitter.
// This bridges Level 2 (Intelligence) with Level 4 (Runtime).
type NATSEmitter struct {
    svc intelligence.IntelligenceService
}

// NewNATSEmitter creates emitter that sends to Intelligence Service.
func NewNATSEmitter(natsURL string) (*NATSEmitter, error) {
    svc, err := intelligence.NewIntelligenceService(natsURL)
    if err != nil {
        return nil, err
    }
    return &NATSEmitter{svc: svc}, nil
}

// Emit sends event to Intelligence Service → NATS
func (e *NATSEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
    return e.svc.ProcessEvent(ctx, event)
}

// Name returns emitter identifier
func (e *NATSEmitter) Name() string {
    return "nats"
}

// IsCritical returns false - NATS is bonus, OTLP is critical
func (e *NATSEmitter) IsCritical() bool {
    return false
}

// Close shuts down Intelligence Service connection
func (e *NATSEmitter) Close() error {
    return e.svc.Shutdown(context.Background())
}
```

### Test (RED first)

```go
// internal/runtime/emitter_nats_test.go

func TestNATSEmitter_Emit(t *testing.T) {
    // Start embedded NATS
    ns := natsserver.RunDefaultServer()
    defer ns.Shutdown()

    // Subscribe to verify event arrives
    received := make(chan *domain.ObserverEvent, 1)
    nc, _ := nats.Connect(ns.ClientURL())
    nc.Subscribe("tapio.events.>", func(m *nats.Msg) {
        var event domain.ObserverEvent
        json.Unmarshal(m.Data, &event)
        received <- &event
    })

    // Create emitter
    emitter, err := NewNATSEmitter(ns.ClientURL())
    require.NoError(t, err)
    defer emitter.Close()

    // Emit event
    event := &domain.ObserverEvent{
        Type:    "deployment",
        Subtype: "rollout_stuck",
    }
    err = emitter.Emit(context.Background(), event)
    require.NoError(t, err)

    // Verify received
    select {
    case evt := <-received:
        assert.Equal(t, "deployment", evt.Type)
        assert.Equal(t, "rollout_stuck", evt.Subtype)
    case <-time.After(time.Second):
        t.Fatal("event not received on NATS")
    }
}

func TestNATSEmitter_IsCritical(t *testing.T) {
    // NATS emitter is NOT critical - if NATS is down, OTLP should still work
    emitter := &NATSEmitter{}
    assert.False(t, emitter.IsCritical())
}

func TestNATSEmitter_Name(t *testing.T) {
    emitter := &NATSEmitter{}
    assert.Equal(t, "nats", emitter.Name())
}
```

**Commit 1:** `feat(runtime): add NATSEmitter wrapping Intelligence Service`
**Size:** ~50 lines code + ~80 lines test

---

## Task 2: Context Service Wiring

**File:** `internal/runtime/enrichment.go`

**Purpose:** Enrich events with K8s context before emitting.

### Interface

```go
// internal/runtime/enrichment.go

package runtime

import (
    "github.com/yairfalse/tapio/internal/services/k8scontext"
    "github.com/yairfalse/tapio/pkg/domain"
)

// Enricher adds K8s context to events before emission.
type Enricher struct {
    ctx *k8scontext.Service
}

// NewEnricher creates enricher with K8s context service.
func NewEnricher(ctx *k8scontext.Service) *Enricher {
    return &Enricher{ctx: ctx}
}

// Enrich adds pod/namespace info to event based on IP.
func (e *Enricher) Enrich(event *domain.ObserverEvent) {
    if e.ctx == nil {
        return
    }

    // Network events: lookup by source IP
    if event.NetworkData != nil && event.NetworkData.SrcIP != "" {
        podInfo := e.ctx.LookupByIP(event.NetworkData.SrcIP)
        if podInfo != nil {
            event.NetworkData.PodName = podInfo.Name
            event.NetworkData.Namespace = podInfo.Namespace
            event.NetworkData.NodeName = podInfo.NodeName
        }
    }

    // Scheduler events: lookup by UID
    if event.SchedulerData != nil && event.SchedulerData.PodUID != "" {
        podInfo := e.ctx.LookupByUID(event.SchedulerData.PodUID)
        if podInfo != nil {
            event.SchedulerData.PodName = podInfo.Name
            event.SchedulerData.Namespace = podInfo.Namespace
        }
    }
}
```

### Test (RED first)

```go
// internal/runtime/enrichment_test.go

func TestEnricher_EnrichNetworkEvent(t *testing.T) {
    // Mock context service
    ctx := &k8scontext.MockService{
        Pods: map[string]*k8scontext.PodInfo{
            "10.0.1.42": {Name: "nginx-abc123", Namespace: "production", NodeName: "node-1"},
        },
    }

    enricher := NewEnricher(ctx)

    event := &domain.ObserverEvent{
        Type: "network",
        NetworkData: &domain.NetworkEventData{
            SrcIP: "10.0.1.42",
        },
    }

    enricher.Enrich(event)

    assert.Equal(t, "nginx-abc123", event.NetworkData.PodName)
    assert.Equal(t, "production", event.NetworkData.Namespace)
    assert.Equal(t, "node-1", event.NetworkData.NodeName)
}

func TestEnricher_NilContextService(t *testing.T) {
    enricher := NewEnricher(nil)

    event := &domain.ObserverEvent{
        Type: "network",
        NetworkData: &domain.NetworkEventData{SrcIP: "10.0.1.42"},
    }

    // Should not panic
    enricher.Enrich(event)

    // Event unchanged
    assert.Empty(t, event.NetworkData.PodName)
}
```

**Commit 2:** `feat(runtime): add Enricher for K8s context`
**Size:** ~40 lines code + ~60 lines test

---

## Task 3: Tier Configuration

**File:** `internal/runtime/config.go` (modify existing)

**Purpose:** Configure emitters based on deployment tier.

### Interface

```go
// internal/runtime/config.go (additions)

// Tier determines which emitters are enabled
type Tier string

const (
    TierFree       Tier = "free"       // OTLP only
    TierEnterprise Tier = "enterprise" // OTLP + NATS → Ahti
)

// Config holds observer runtime configuration
type Config struct {
    // Existing fields...

    // Tier determines emitter configuration
    Tier Tier

    // NATSURL for enterprise tier (ignored for free tier)
    NATSURL string

    // OTLPURL for all tiers
    OTLPURL string

    // ContextServiceURL for enrichment (optional)
    ContextServiceURL string
}

// BuildEmitters creates emitters based on tier configuration.
func (c *Config) BuildEmitters() ([]Emitter, error) {
    var emitters []Emitter

    // OTLP always enabled (critical)
    if c.OTLPURL != "" {
        otlp, err := NewOTLPEmitter(c.OTLPURL)
        if err != nil {
            return nil, fmt.Errorf("failed to create OTLP emitter: %w", err)
        }
        emitters = append(emitters, otlp)
    }

    // NATS only for enterprise tier (non-critical)
    if c.Tier == TierEnterprise && c.NATSURL != "" {
        nats, err := NewNATSEmitter(c.NATSURL)
        if err != nil {
            // Log warning but don't fail - NATS is non-critical
            log.Printf("WARN: failed to create NATS emitter: %v", err)
        } else {
            emitters = append(emitters, nats)
        }
    }

    return emitters, nil
}
```

### Environment Variables

```bash
# Free tier (default) - OTLP only
TAPIO_TIER=free
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317

# Enterprise tier - OTLP + NATS → Ahti
TAPIO_TIER=enterprise
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
NATS_URL=nats://localhost:4222
```

### Test (RED first)

```go
// internal/runtime/config_test.go (additions)

func TestConfig_BuildEmitters_FreeTier(t *testing.T) {
    cfg := Config{
        Tier:    TierFree,
        OTLPURL: "localhost:4317",
        NATSURL: "nats://localhost:4222",  // Should be ignored for Free tier
    }

    emitters, err := cfg.BuildEmitters()
    require.NoError(t, err)

    // Only OTLP emitter (Free tier = OTLP only)
    assert.Len(t, emitters, 1)
    assert.Equal(t, "otlp", emitters[0].Name())
}

func TestConfig_BuildEmitters_EnterpriseTier(t *testing.T) {
    ns := natsserver.RunDefaultServer()
    defer ns.Shutdown()

    cfg := Config{
        Tier:    TierEnterprise,
        OTLPURL: "localhost:4317",
        NATSURL: ns.ClientURL(),
    }

    emitters, err := cfg.BuildEmitters()
    require.NoError(t, err)

    // OTLP + NATS emitters
    assert.Len(t, emitters, 2)
    names := []string{emitters[0].Name(), emitters[1].Name()}
    assert.Contains(t, names, "otlp")
    assert.Contains(t, names, "nats")
}

func TestConfig_BuildEmitters_NATSDown(t *testing.T) {
    cfg := Config{
        Tier:    TierEnterprise,
        OTLPURL: "localhost:4317",
        NATSURL: "nats://nonexistent:4222",  // Bad URL
    }

    emitters, err := cfg.BuildEmitters()

    // Should NOT fail - NATS is non-critical
    require.NoError(t, err)

    // Only OTLP emitter (NATS failed gracefully)
    assert.Len(t, emitters, 1)
    assert.Equal(t, "otlp", emitters[0].Name())
}
```

**Commit 3:** `feat(runtime): add tier-based emitter configuration`
**Size:** ~30 lines code + ~50 lines test

---

## Task 4: Integration Test (Full Pipeline)

**File:** `internal/runtime/integration_pipeline_test.go`

**Purpose:** Verify complete flow: Observer → Enrichment → NATS → (Ahti can subscribe)

### Test

```go
// internal/runtime/integration_pipeline_test.go

func TestFullPipeline_ObserverToNATS(t *testing.T) {
    // 1. Start embedded NATS
    ns := natsserver.RunDefaultServer()
    defer ns.Shutdown()

    // 2. Subscribe (simulating Ahti)
    received := make(chan *domain.ObserverEvent, 10)
    nc, _ := nats.Connect(ns.ClientURL())
    defer nc.Close()
    nc.Subscribe("tapio.events.>", func(m *nats.Msg) {
        var event domain.ObserverEvent
        json.Unmarshal(m.Data, &event)
        received <- &event
    })

    // 3. Create mock context service
    ctxSvc := &k8scontext.MockService{
        Pods: map[string]*k8scontext.PodInfo{
            "10.0.1.42": {Name: "web-abc", Namespace: "prod"},
        },
    }

    // 4. Create runtime with enterprise tier config
    cfg := Config{
        Tier:    TierEnterprise,
        NATSURL: ns.ClientURL(),
    }
    emitters, _ := cfg.BuildEmitters()
    enricher := NewEnricher(ctxSvc)

    runtime := NewObserverRuntime(
        WithEmitters(emitters),
        WithEnricher(enricher),
    )

    // 5. Simulate observer emitting event
    event := &domain.ObserverEvent{
        Type:    "network",
        Subtype: "syn_timeout",
        NetworkData: &domain.NetworkEventData{
            SrcIP: "10.0.1.42",
            DstIP: "10.0.2.100",
        },
    }

    err := runtime.Emit(context.Background(), event)
    require.NoError(t, err)

    // 6. Verify Ahti would receive enriched event
    select {
    case evt := <-received:
        assert.Equal(t, "network", evt.Type)
        assert.Equal(t, "syn_timeout", evt.Subtype)
        // Verify enrichment worked
        assert.Equal(t, "web-abc", evt.NetworkData.PodName)
        assert.Equal(t, "prod", evt.NetworkData.Namespace)
    case <-time.After(2 * time.Second):
        t.Fatal("event not received - pipeline broken")
    }
}

func TestFullPipeline_FreeTierNoNATS(t *testing.T) {
    // Free tier should NOT publish to NATS
    ns := natsserver.RunDefaultServer()
    defer ns.Shutdown()

    received := make(chan bool, 1)
    nc, _ := nats.Connect(ns.ClientURL())
    defer nc.Close()
    nc.Subscribe("tapio.events.>", func(m *nats.Msg) {
        received <- true
    })

    cfg := Config{
        Tier:    TierFree,  // Free tier - no NATS
        NATSURL: ns.ClientURL(),
    }
    emitters, _ := cfg.BuildEmitters()
    runtime := NewObserverRuntime(WithEmitters(emitters))

    event := &domain.ObserverEvent{Type: "test"}
    runtime.Emit(context.Background(), event)

    // Should NOT receive anything on NATS
    select {
    case <-received:
        t.Fatal("free tier should not publish to NATS")
    case <-time.After(500 * time.Millisecond):
        // Good - no message received
    }
}
```

**Commit 4:** `test(runtime): add full pipeline integration test`
**Size:** ~100 lines test

---

## Verification Checklist

Before merging:

- [ ] **Unit tests pass:** `go test ./internal/runtime/...`
- [ ] **Integration tests pass:** `go test ./internal/runtime/... -run Integration`
- [ ] **No lint errors:** `golangci-lint run`
- [ ] **Coverage maintained:** >80% for runtime package
- [ ] **NATS emitter is non-critical:** Verify IsCritical() returns false
- [ ] **Enrichment works:** Network events get pod context
- [ ] **Tier config works:** Free = OTLP only, Enterprise = OTLP + NATS

---

## Commit Plan

| # | Message | Files | Lines |
|---|---------|-------|-------|
| 1 | `feat(runtime): add NATSEmitter` | emitter_nats.go, emitter_nats_test.go | ~130 |
| 2 | `feat(runtime): add Enricher` | enrichment.go, enrichment_test.go | ~100 |
| 3 | `feat(runtime): add tier config` | config.go, config_test.go | ~80 |
| 4 | `test(runtime): pipeline integration` | integration_pipeline_test.go | ~100 |

**Total:** ~410 lines (including tests)

---

## After Wiring

```
Deployments Observer
        │
        ▼
┌───────────────────┐
│  ObserverRuntime  │
│                   │
│  1. Enricher      │◀── K8s Context Service
│     (add context) │    (lookup by IP/UID)
│                   │
│  2. Emitters      │
│     ├─ OTLP ──────┼──▶ Prometheus/Grafana
│     └─ NATS ──────┼──▶ tapio.events.deployment.rollout_stuck
└───────────────────┘              │
                                   ▼
                            ┌─────────────┐
                            │    AHTI     │
                            │  subscribes │
                            │  correlates │
                            └─────────────┘
```

**Result:** Tapio → Ahti pipeline complete. Priority 1 and 3 connected.

---

## Dependencies

No new dependencies. Uses existing:
- `github.com/nats-io/nats.go` (already in go.mod)
- `pkg/intelligence` (already exists)
- `internal/services/k8scontext` (already exists)

---

## TDD Workflow

For each task:

```bash
# RED: Write failing test
go test ./internal/runtime -v -run TestNATSEmitter  # FAIL

# GREEN: Minimal implementation
go test ./internal/runtime -v -run TestNATSEmitter  # PASS

# REFACTOR: Clean up
go fmt ./... && go vet ./... && golangci-lint run

# COMMIT: Small commit
git add -A && git commit -m "feat(runtime): add NATSEmitter"
```

---

**Let's wire this pipeline! 🔌**
