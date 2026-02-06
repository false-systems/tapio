# ADR 009: Observer Runtime Refactor - Decouple Infrastructure from Logic

> **NOTE**: NATS emitter references in this document are outdated. TAPIO now uses **POLKU** (gRPC event gateway) instead of NATS.

**Status:** Proposed
**Date:** 2025-01-03
**Authors:** Tapio Team
**Decision:** Refactor all observers to use a unified ObserverRuntime infrastructure

---

## Context & Problem Statement

### Current State (6 Observers)

**Observers Using BaseObserver (4):**
- network (eBPF)
- node (eBPF)
- deployments (K8s API)
- scheduler (K8s API)

**Observers Using Custom Implementation (2):**
- container-runtime (eBPF, custom struct)
- container-api (K8s API, inline OTEL metrics)

### Problems

1. **Code Duplication**
   - Each observer implements eBPF loading/management
   - Each observer implements K8s informer management
   - Each observer implements OTLP export
   - Each observer implements metrics collection

2. **Tight Coupling**
   - Observer logic mixed with infrastructure code
   - Hard to test business logic without eBPF/K8s
   - Can't change export format without touching all observers

3. **Missing Production Features**
   - No event sampling (10K events/sec = Prometheus explosion)
   - No backpressure control (slow emitters = OOM)
   - No per-observer health checks
   - No graceful degradation (one observer fails = pod crashes)
   - No metrics cardinality control (pod_name label = cardinality explosion)

4. **Hard to Add New Observers**
   - Must copy infrastructure code
   - Must understand eBPF/K8s/OTLP details
   - No clear pattern to follow

5. **Deployment Complexity**
   - 4+ separate binaries (could be 12+ in future)
   - Each needs separate DaemonSet/Deployment
   - Hard to manage, configure, update

---

## Decision

**Build a unified ObserverRuntime infrastructure that separates infrastructure from business logic.**

### Architecture Principles

1. **Separation of Concerns**
   - Infrastructure: eBPF, K8s, OTLP, metrics, lifecycle
   - Business Logic: Observer-specific event processing

2. **Single Responsibility**
   - ObserverRuntime: Provides infrastructure to ALL observers
   - EventProcessor: Observer-specific logic ONLY

3. **Composition Over Inheritance**
   - Observers compose with ObserverRuntime, don't inherit from BaseObserver
   - Flexible options pattern for configuration

4. **Production-Ready from Day 1**
   - Event sampling, backpressure, health checks built-in
   - Not added as afterthoughts

---

## Proposed Architecture

### Two Layers

```
┌─────────────────────────────────────────────────┐
│  ObserverRuntime (Infrastructure Layer)        │
│  ════════════════════════════════════════════   │
│  - eBPF loading & ring buffers                 │
│  - K8s informers & watches                     │
│  - OTLP/NATS export (multi-emitter)            │
│  - Event sampling & filtering                  │
│  - Backpressure control                        │
│  - Health checking                             │
│  - Metrics with cardinality control            │
│  - Graceful degradation & retry                │
│  - Lifecycle (Start/Stop)                      │
│  ════════════════════════════════════════════   │
│  REUSABLE FOR ALL OBSERVERS                    │
└──────────────┬──────────────────────────────────┘
               │
               │ Provides infrastructure to
               ▼
┌─────────────────────────────────────────────────┐
│  EventProcessor (Business Logic Layer)         │
│  ════════════════════════════════════════════   │
│  - NetworkProcessor: DNS/Link/Status detection │
│  - NodeProcessor: PMC hardware monitoring      │
│  - ContainerProcessor: OOM/Exit classification │
│  - DeploymentsProcessor: Rollout health        │
│  - SchedulerProcessor: Scheduling failures     │
│  ════════════════════════════════════════════   │
│  PURE BUSINESS LOGIC - NO INFRASTRUCTURE       │
└─────────────────────────────────────────────────┘
```

---

## Core Interfaces

### EventProcessor (Business Logic)

```go
// EventProcessor is the ONLY interface observers implement
type EventProcessor interface {
    // Process converts raw event bytes to domain event
    // Returns nil if event not interesting
    Process(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error)

    // Name for logging/metrics
    Name() string
}
```

**Example:**
```go
type NetworkProcessor struct {
    dnsProcessor    *DNSProcessor
    linkProcessor   *LinkProcessor
    statusProcessor *StatusProcessor
}

func (p *NetworkProcessor) Process(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
    evt := parseNetworkEventBPF(rawEvent)

    // Try each processor (existing logic!)
    if result := p.dnsProcessor.Process(ctx, evt); result != nil {
        return result, nil
    }
    if result := p.linkProcessor.Process(ctx, evt); result != nil {
        return result, nil
    }
    // etc...

    return nil, nil
}
```

### ObserverRuntime (Infrastructure)

```go
// ObserverRuntime provides all infrastructure
type ObserverRuntime struct {
    name   string
    config Config

    // Infrastructure components
    ebpf       *EBPFRuntime      // Optional: eBPF
    k8s        *K8sRuntime        // Optional: K8s
    processor  EventProcessor     // Required: Business logic
    emitters   []Emitter          // Required: Export (can be multiple)
    sampler    *Sampler           // Optional: Event sampling
    queue      *BoundedQueue      // Required: Backpressure
    health     *HealthChecker     // Required: Health checks
    metrics    *Metrics           // Required: OTEL metrics

    // Failure handling
    failurePolicy FailurePolicy
    retryConfig   RetryConfig
}

// Options pattern for configuration
func New(name string, opts ...Option) (*ObserverRuntime, error)

func WithProcessor(p EventProcessor) Option
func WithEBPF(programPath string) Option
func WithK8s(clientset kubernetes.Interface, resource string) Option
func WithEmitter(e Emitter) Option  // Can call multiple times
func WithSampling(rate float64, rules []SamplingRule) Option
func WithBackpressure(queueSize int, policy DropPolicy) Option
func WithFailurePolicy(policy FailurePolicy, retry RetryConfig) Option
```

---

## Production Features (Built-in)

### 1. Multiple Emitters (Multi-Export)

**Problem:** OSS needs OTLP, Enterprise needs OTLP + NATS, debugging needs file output

**Solution:**
```go
runtime.New("network",
    runtime.WithProcessor(processor),
    runtime.WithEmitter(NewOTLPEmitter(otlpEndpoint)),    // Primary
    runtime.WithEmitter(NewNATSEmitter(natsURL)),         // Enterprise
    runtime.WithEmitter(NewFileEmitter("/tmp/debug.log")), // Debug
)
```

Events fan-out to all emitters in parallel.

---

### 2. Event Sampling & Filtering

**Problem:** 10K events/sec will kill Prometheus/Tempo

**Solution:**
```yaml
observers:
  network:
    sampling:
      default_rate: 0.1  # 10% sampling
      rules:
        - type: network
          subtype: dns_query
          keep_all: true        # Always keep DNS queries
        - type: network
          subtype: http_connection
          rate: 0.01            # 1% of HTTP connections
```

Intelligent sampling per event type.

---

### 3. Backpressure Control

**Problem:** Slow emitters (network, disk) can cause OOM

**Solution:**
```yaml
observers:
  network:
    backpressure:
      queue_size: 10000
      drop_policy: oldest  # Drop oldest events when queue full
```

Bounded queue prevents memory explosion. Drop policies:
- `oldest`: Drop oldest events (keep recent)
- `newest`: Drop new events (preserve history)
- `random`: Random drop

---

### 4. Health Checking (Per-Observer)

**Problem:** Pod health is all-or-nothing, can't see which observer failed

**Solution:**
```bash
GET /health/network
{
  "status": "healthy",
  "last_check": "2025-01-03T10:30:00Z",
  "events_processed": 150000,
  "events_dropped": 23,
  "uptime": "2h15m30s"
}

GET /health/node
{
  "status": "degraded",
  "last_check": "2025-01-03T10:30:01Z",
  "last_error": "PMC not available on this CPU",
  "events_processed": 0,
  "uptime": "2h15m30s"
}
```

Kubernetes integration:
```yaml
livenessProbe:
  httpGet:
    path: /health/network
    port: 8080

readinessProbe:
  httpGet:
    path: /health/network
    port: 8080
```

---

### 5. Metrics Cardinality Control

**Problem:** `pod_name` label = 1000 pods = Prometheus cardinality explosion

**Solution:**
```yaml
observers:
  network:
    metrics:
      labels:
        - namespace      # OK: ~10 namespaces
        - observer_type  # OK: ~6 observer types
        # Exclude: pod_name (1000+ cardinality)
        # Exclude: container_id (5000+ cardinality)
```

Configurable label filtering prevents cardinality issues.

---

### 6. Graceful Degradation & Retry

**Problem:** Network observer fails → entire pod crashes → node observer also down

**Solution:**
```yaml
observers:
  network:
    failure_policy: isolate  # Don't crash other observers
    retry:
      max_attempts: 3
      initial_delay: 5s
      max_delay: 60s
      multiplier: 2.0
```

Failure policies:
- `isolate`: Continue other observers, mark this one unhealthy
- `restart`: Retry with exponential backoff
- `fail_fast`: Crash entire binary (for critical observers)

---

## Binary Architecture

### Two Binaries (Not One!)

**tapio-observer (DaemonSet - eBPF observers):**
- network
- node
- container-runtime

**tapio-controller (Deployment - K8s API observers):**
- deployments
- scheduler
- container-api

**Why two binaries?**
1. **Security:** eBPF needs privileged, K8s API doesn't
2. **Performance:** eBPF runs on every node, K8s API only needs one instance
3. **Scaling:** DaemonSet can't scale, Deployment can (HA)
4. **Smaller binaries:** eBPF observers ~95MB, K8s API observers ~50MB

---

## Example Usage

### tapio-observer Binary

```go
// cmd/tapio-observer/main.go
func main() {
    cfg := loadConfig()
    ctx := context.Background()

    var wg sync.WaitGroup

    // Network observer
    if cfg.Observers.Network.Enabled {
        wg.Add(1)
        go func() {
            defer wg.Done()
            runNetworkObserver(ctx, cfg)
        }()
    }

    // Node observer
    if cfg.Observers.Node.Enabled {
        wg.Add(1)
        go func() {
            defer wg.Done()
            runNodeObserver(ctx, cfg)
        }()
    }

    wg.Wait()
}

func runNetworkObserver(ctx context.Context, cfg *Config) error {
    processor := network.NewNetworkProcessor()

    obs, err := runtime.New("network",
        runtime.WithProcessor(processor),
        runtime.WithEBPF("/var/lib/tapio/network_monitor.o"),
        runtime.WithEmitter(NewOTLPEmitter(cfg.OTLP.Endpoint)),
        runtime.WithSampling(0.1, cfg.Observers.Network.SamplingRules),
        runtime.WithBackpressure(10000, runtime.DropOldest),
        runtime.WithFailurePolicy(runtime.FailPolicyIsolate, cfg.RetryConfig),
    )
    if err != nil {
        return err
    }

    return obs.Run(ctx)
}
```

### Configuration

```yaml
# ConfigMap for tapio-observer (DaemonSet)
observers:
  network:
    enabled: true
    sampling:
      default_rate: 0.1
      rules:
        - type: network
          subtype: dns_query
          keep_all: true
    backpressure:
      queue_size: 10000
      drop_policy: oldest
    failure_policy: isolate

  node:
    enabled: true
    sampling:
      default_rate: 1.0  # Keep all PMC events
    failure_policy: isolate

otlp:
  endpoint: http://tempo:4318

retry:
  max_attempts: 3
  initial_delay: 5s
  max_delay: 60s
  multiplier: 2.0
```

---

## Implementation Plan (REVISED - 13-14 weeks)

### Phase 1: Build Infrastructure (Week 1-3) - APPROVED ✅

**Extended to 3 weeks for proper validation**

**Deliverables:**
```
internal/runtime/
├── observer_runtime.go         # Main runtime orchestrator
├── ebpf_runtime.go             # eBPF infrastructure
├── k8s_runtime.go              # K8s informer infrastructure
├── emitter.go                  # Multi-emitter support
├── emitter_otlp.go             # OTLP emitter
├── emitter_nats.go             # NATS emitter (enterprise)
├── emitter_file.go             # File emitter (debug)
├── sampler.go                  # Event sampling & filtering
├── queue.go                    # Bounded queue with backpressure
├── health.go                   # Health checking
├── metrics.go                  # Metrics with cardinality control
├── processor.go                # EventProcessor interface (UPDATED)
└── config.go                   # Runtime configuration

internal/observers/test/         # NEW: Test observer
├── processor.go                # Generates mock events
├── processor_test.go
└── README.md                   # How to use for testing
```

**NEW: EventProcessor Interface (Extended)**
```go
type EventProcessor interface {
    // Process converts raw event bytes to domain event
    Process(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error)

    // Name for logging/metrics
    Name() string

    // NEW: Setup called once before Start
    Setup(ctx context.Context) error

    // NEW: Teardown called once after Stop
    Teardown(ctx context.Context) error
}
```

**NEW: Test Observer**
- Generates configurable mock events
- Tests all runtime features (sampling, backpressure, health)
- Framework validation before any real observer migration

**Testing:**
- Unit tests for each component
- Integration test with test observer (not mock!)
- Load test (10K events/sec sustained for 1 hour)
- Failure scenarios (network failures, slow emitters, backpressure)
- Memory leak detection (run 24h)
- CPU profiling under load

**Exit Criteria:**
- ✅ All tests passing
- ✅ Test observer runs successfully
- ✅ Handles 10K events/sec without OOM (24h test)
- ✅ Health checks working
- ✅ Sampling working (validate drop rates)
- ✅ Multiple emitters working (fan-out verified)
- ✅ No memory leaks (pprof validation)
- ✅ CPU usage acceptable (<5% idle, <80% under load)

---

### Phase 1.5: Network Observer Migration + Production Validation (Week 4-6) - CHECKPOINT

**CRITICAL VALIDATION PHASE - DO NOT SKIP!**

**Week 4: Migration**
- Extract NetworkProcessor from existing network observer
- Wire to ObserverRuntime
- Integration tests (all existing tests must pass)
- Performance benchmarks (compare old vs new)

**Week 5: Dev/Staging Deployment**
- Deploy new network observer to dev cluster
- Run side-by-side with old observer (dual deployment)
- Compare metrics:
  - Event counts (should match ±5%)
  - Event latency (should be comparable)
  - CPU/memory usage (should be lower or same)
  - Error rates (should be same or lower)
- Test failure scenarios:
  - Slow OTLP endpoint (backpressure kicks in?)
  - High event rate (sampling working?)
  - Pod restarts (graceful shutdown?)

**Week 6: Production Soak Test**
- Deploy to production (canary: 10% of nodes)
- Run for 2 weeks alongside old observer
- Monitor continuously:
  - Missing events? (compare with old observer)
  - Performance degradation?
  - Memory leaks?
  - Unexpected errors?
  - Health check accuracy?

**CHECKPOINT DECISION (End of Week 6):**

**✅ Proceed to Phase 2 if:**
- No missing events vs old observer
- Performance equal or better
- No memory leaks
- No critical bugs
- Health checks accurate
- Team confident in architecture

**🛑 STOP and fix if:**
- Missing events (sampling too aggressive?)
- Performance worse (where's the bottleneck?)
- Memory leaks (which component?)
- Critical bugs (what did we miss?)
- Health checks inaccurate (false positives/negatives?)

**If we stop:** Fix issues, extend Phase 1.5, re-validate. DO NOT proceed until rock-solid.

---

### Phase 2: Remaining Observers (Week 7-13) - ONLY IF PHASE 1.5 SUCCESSFUL

**Migration order (validated pattern from Phase 1.5):**

1. **Node Observer** (Week 7)
   - Similar to network (eBPF + BaseObserver)
   - PMC logic extraction
   - 1 week migration + testing

2. **Deployments Observer** (Week 8)
   - K8s API observer
   - Create tapio-controller binary
   - 1 week migration + testing

3. **Scheduler Observer** (Week 9)
   - K8s API observer
   - Add to tapio-controller
   - 1 week migration + testing

4. **Container Runtime Observer** (Week 10-12)
   - **COMPLEX**: Custom eBPF, OOM detection critical
   - 2-3 weeks for careful migration
   - Extensive testing (OOM scenarios)

5. **Container API Observer** (Week 13)
   - K8s API observer
   - Remove inline metrics
   - 1 week migration + testing

**Each observer follows Phase 1.5 pattern:**
- Extract processor
- Integration tests
- Dev/staging validation
- Performance comparison
- NO production deployment until all tested

---

### Original Phase 2: Migrate Observers (Week 3-8) - REPLACED BY REVISED PLAN ABOVE

**Migration order (easiest → hardest):**

1. **Network Observer** (Week 3)
   - Already uses BaseObserver
   - Extract NetworkProcessor
   - Keep DNS/Link/Status processors unchanged
   - Validate no regressions

2. **Node Observer** (Week 4)
   - Already uses BaseObserver
   - Extract NodeProcessor (PMC logic)
   - Validate IPC calculations correct

3. **Deployments Observer** (Week 5)
   - Already uses BaseObserver
   - Extract DeploymentsProcessor
   - Create tapio-controller binary

4. **Scheduler Observer** (Week 6)
   - Already uses BaseObserver
   - Extract SchedulerProcessor
   - Add to tapio-controller

5. **Container Runtime Observer** (Week 7)
   - Custom eBPF implementation
   - Extract ContainerProcessor (keep OOMProcessor, ExitProcessor)
   - Thorough testing (OOM detection is critical!)

6. **Container API Observer** (Week 8)
   - Custom K8s implementation
   - Extract ContainerAPIProcessor
   - Remove inline OTEL metrics

**After each migration:**
- ✅ All tests pass
- ✅ Manual testing in dev cluster
- ✅ Update documentation
- ✅ Git commit

---

### Phase 3: Framework for Future Observers (Week 9)

**Deliverables:**

1. **Observer Generator**
   ```bash
   ./scripts/generate-observer.sh storage
   # Creates skeleton: processor.go, tests, README
   ```

2. **Development Guide**
   ```
   docs/OBSERVER_DEVELOPMENT.md
   - How to implement EventProcessor
   - How to wire to ObserverRuntime
   - Testing checklist
   - Deployment guide
   ```

3. **Example Observer**
   ```go
   // internal/observers/example/processor.go
   // Well-commented reference implementation
   ```

4. **Binary Build Scripts**
   ```bash
   ./scripts/add-observer-to-binary.sh storage tapio-observer
   ```

---

### Phase 4: Final Integration (Week 10)

1. **Update Operator**
   - Create DaemonSet for tapio-observer
   - Create Deployment for tapio-controller
   - Generate ConfigMaps

2. **End-to-End Testing**
   - Deploy full stack
   - Test all 6 observers
   - Load testing
   - Failure scenarios

3. **Documentation**
   - Architecture docs
   - Migration guide
   - Operator deployment
   - Troubleshooting

---

## Technology Choices

### Go Libraries

- **eBPF:** `github.com/cilium/ebpf` (existing, proven)
- **K8s Client:** `k8s.io/client-go` (existing, standard)
- **OTLP Export:** `go.opentelemetry.io/otel` (existing, standard)
- **NATS:** `github.com/nats-io/nats.go` (existing, lightweight)
- **Logging:** `github.com/rs/zerolog` (existing, structured)

### Patterns

- **Options Pattern:** Clean API for runtime configuration
- **Fan-Out:** Parallel event emission to multiple destinations
- **Bounded Queues:** Prevent OOM under load
- **Health Checks:** Per-component health reporting
- **Graceful Degradation:** Isolate failures

### Testing

- **Unit Tests:** Each component tested independently
- **Integration Tests:** Mock processor with real infrastructure
- **Load Tests:** 10K events/sec sustained load
- **Failure Tests:** Network failures, slow emitters, resource limits

---

## Benefits

### For Development

1. **Faster Observer Development**
   - Implement EventProcessor interface only
   - No infrastructure code needed
   - Clear pattern to follow

2. **Better Testing**
   - Test business logic without eBPF/K8s
   - Mock infrastructure easily
   - Faster test execution

3. **Easier Debugging**
   - Separate logs for infrastructure vs logic
   - Per-observer health checks
   - File emitter for debugging

### For Operations

1. **Better Observability**
   - Per-observer health endpoints
   - Detailed metrics with cardinality control
   - Sampling to reduce volume

2. **Resource Efficiency**
   - Event sampling reduces load
   - Backpressure prevents OOM
   - 2 binaries instead of 6+

3. **Graceful Failures**
   - One observer fails, others continue
   - Automatic retries with backoff
   - Clear failure reporting

### For Product

1. **OSS + Enterprise Support**
   - Multi-emitter: OTLP (OSS) + NATS (Enterprise)
   - Same observers, different export
   - Clean separation

2. **Scalability**
   - Add observers without infrastructure code
   - Generator scripts
   - Clear patterns

3. **Production-Ready**
   - Sampling, backpressure, health checks from day 1
   - Not afterthoughts
   - Battle-tested patterns

---

## Risks & Mitigation

### Risk 1: Infrastructure Takes Longer Than Expected

**Mitigation:**
- Timebox Phase 1 to 3 weeks max
- Cut nice-to-have features if needed
- Keep MUST-HAVE: multi-emitter, sampling, health checks

### Risk 2: Observer Migration Breaks Functionality

**Mitigation:**
- Keep old observer code during migration
- Comprehensive testing after each migration
- Can rollback if issues found
- One observer at a time (not all at once)

### Risk 3: Performance Regression

**Mitigation:**
- Load testing in Phase 1 (before any migration)
- Benchmark each observer after migration
- Compare with baseline metrics
- Profile hot paths

### Risk 4: Timeline Slips

**Mitigation:**
- Phase 1-2 are CRITICAL (infrastructure + migrations)
- Phase 3 can be deferred (nice-to-have)
- Phase 4 documentation can be incremental
- Checkpoints after each phase (go/no-go decision)

---

## Alternatives Considered

### Alternative 1: Keep Current BaseObserver Pattern

**Pros:**
- No refactor needed
- Works today

**Cons:**
- Still have code duplication
- Missing production features (sampling, backpressure)
- Hard to add observers
- 2 observers don't use BaseObserver (inconsistent)

**Decision:** Rejected. Need production features, cleaner architecture.

---

### Alternative 2: Single Binary for All Observers

**Pros:**
- Only one binary to build/deploy

**Cons:**
- Security: K8s API observers don't need privileged
- Size: ~150MB binary vs 95MB + 50MB = 145MB
- Scaling: Can't scale K8s API observers independently
- Blast radius: One bad observer crashes all

**Decision:** Rejected. Two binaries (eBPF vs K8s) is better.

---

### Alternative 3: Plugin System (Load observers at runtime)

**Pros:**
- Add observers without recompiling

**Cons:**
- Complex plugin architecture
- Security concerns (loading arbitrary code)
- Versioning nightmare
- Go doesn't have great plugin support

**Decision:** Rejected. Compile-time is simpler and safer.

---

## Success Criteria

### Phase 1 Success

- ✅ ObserverRuntime works with mock processor
- ✅ All production features working
- ✅ Load tested (10K events/sec)
- ✅ Failure scenarios tested

### Phase 2 Success

- ✅ All 6 observers migrated
- ✅ No regressions in functionality
- ✅ 2 working binaries (tapio-observer, tapio-controller)
- ✅ Integration tests passing

### Phase 3 Success

- ✅ Clear documentation for adding observers
- ✅ Generator scripts working
- ✅ Example observer as reference

### Overall Success

- ✅ Clean architecture (infrastructure vs logic)
- ✅ Production features (sampling, health, backpressure)
- ✅ Easy to add observers
- ✅ Better than before (not just different)
- ✅ No regressions
- ✅ Deployed to production successfully

---

## Critical Design Questions & Answers

### Question 1: Multi-Emitter Policy - Best Effort or All-or-Nothing?

**Question:** If we have 3 emitters (OTLP + NATS + File), and one fails, do we:
- **Best Effort:** Continue with working emitters, log error
- **All-or-Nothing:** Fail entire event if any emitter fails

**Answer: BEST EFFORT** ✅

**Reasoning:**
1. **Availability > Consistency:** If NATS is down, OTLP should still work
2. **Graceful Degradation:** Enterprise features shouldn't break OSS export
3. **Observable Failures:** Log + metric each emitter failure separately

**Implementation:**
```go
func (r *ObserverRuntime) emitEvent(ctx context.Context, event *domain.ObserverEvent) {
    var wg sync.WaitGroup
    for _, emitter := range r.emitters {
        wg.Add(1)
        go func(e Emitter) {
            defer wg.Done()
            if err := e.Emit(ctx, event); err != nil {
                // BEST EFFORT: Log error, don't fail event
                r.logger.Error().
                    Str("emitter", e.Name()).
                    Err(err).
                    Msg("emitter failed, continuing with others")
                r.metrics.emitterErrors.Add(ctx, 1,
                    metric.WithAttributes(
                        attribute.String("emitter", e.Name()),
                    ))
            }
        }(emitter)
    }
    wg.Wait()
}
```

**Metrics to track:**
- `emitter_errors_total{emitter="otlp"}` - Track per-emitter failures
- `emitter_success_total{emitter="nats"}` - Track per-emitter successes
- Alert if error rate > 5% for any emitter

---

### Question 2: Rollback Strategy - Can We Run Old + New Side-by-Side?

**Question:** During migration, can we run old observer + new observer simultaneously for comparison?

**Answer: YES - Dual Deployment Pattern** ✅

**Implementation:**

**Option A: Separate DaemonSets (Recommended)**
```yaml
# Old network observer (existing)
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: network-observer-old
spec:
  selector:
    matchLabels:
      app: network-observer-old
  template:
    spec:
      containers:
      - name: observer
        image: tapio/network-observer:old
        # ... existing config

---
# New network observer (runtime-based)
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: network-observer-new
spec:
  selector:
    matchLabels:
      app: network-observer-new
  template:
    spec:
      containers:
      - name: observer
        image: tapio/tapio-observer:new
        env:
        - name: ENABLED_OBSERVERS
          value: "network"
        # ... new runtime config
```

**Benefits:**
- Both observers run on every node
- Compare event counts, latency, resource usage
- Can route to different OTLP endpoints for comparison
- Easy rollback: delete new DaemonSet

**Option B: Canary Deployment (Production)**
```yaml
# New observer on 10% of nodes
nodeSelector:
  tapio.io/canary: "true"  # Label 10% of nodes

# Old observer on 90% of nodes
nodeSelector:
  tapio.io/canary: "false"
```

**Rollback Plan:**
1. **Dev/Staging:** Run old + new simultaneously (1 week)
2. **Production Canary:** 10% of nodes with new (2 weeks)
3. **If issues:** Delete new DaemonSet, investigate
4. **If success:** Gradually increase new, decrease old
5. **Final cutover:** Remove old observer

**Comparison Metrics:**
```
# Event count comparison
old_observer_events_total{type="network"} ≈ new_observer_events_total{type="network"} (±5%)

# Latency comparison
old_observer_processing_time_ms_p99 ≈ new_observer_processing_time_ms_p99

# Resource usage
new_observer_memory_bytes < old_observer_memory_bytes (goal: lower or same)
```

---

### Question 3: Hot Reload - Dynamic Sampling Config Without Restart?

**Question:** Can we change sampling rates without restarting pods?

**Answer: YES - Config Watcher Pattern** ✅

**Implementation:**

**Phase 1: NOT INCLUDED** (keep it simple first)
**Phase 2/3: ADD if needed** (after production validation)

**Design (for future):**
```go
type ConfigWatcher struct {
    configMap   string
    namespace   string
    clientset   kubernetes.Interface
    onChange    func(*Config)
    stopCh      chan struct{}
}

func (w *ConfigWatcher) Watch(ctx context.Context) {
    informer := // ... K8s informer for ConfigMap

    informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        UpdateFunc: func(oldObj, newObj interface{}) {
            newConfig := parseConfig(newObj)

            // Validate config
            if err := newConfig.Validate(); err != nil {
                log.Error().Err(err).Msg("invalid config, ignoring update")
                return
            }

            // Apply new config
            w.onChange(newConfig)
            log.Info().Msg("config hot-reloaded successfully")
        },
    })

    informer.Run(w.stopCh)
}

// In ObserverRuntime
func (r *ObserverRuntime) UpdateSamplingConfig(newRules []SamplingRule) {
    r.sampler.Update(newRules)  // Thread-safe update
    r.logger.Info().Msg("sampling config updated without restart")
}
```

**What can be hot-reloaded:**
- ✅ Sampling rates/rules
- ✅ Backpressure queue size
- ✅ Emitter endpoints (add/remove)
- ❌ Observer enable/disable (requires restart)
- ❌ eBPF program (requires restart)

**Benefits:**
- Adjust sampling in production without downtime
- React to traffic spikes (increase sampling)
- Debug issues (temporary 100% sampling)

**Risks:**
- Config validation critical (bad config = crash)
- Race conditions if not thread-safe
- Complexity in Phase 1

**Decision:** Skip for Phase 1, add in Phase 2/3 if validated as needed.

---

## Timeline Summary (REVISED)

```
Week 1-3:   Phase 1 - Build ObserverRuntime infrastructure (APPROVED ✅)
Week 4:     Phase 1.5 - Migrate Network Observer
Week 5:     Phase 1.5 - Dev/Staging validation
Week 6:     Phase 1.5 - Production soak test (CHECKPOINT 🛑)
Week 7:     Phase 2 - Migrate Node Observer (if checkpoint passed)
Week 8:     Phase 2 - Migrate Deployments Observer
Week 9:     Phase 2 - Migrate Scheduler Observer
Week 10-12: Phase 2 - Migrate Container Runtime Observer (complex!)
Week 13:    Phase 2 - Migrate Container API Observer
Week 14:    Phase 3 - Final integration & docs

Total: 14 weeks (was 10 weeks)
```

**Original Timeline:**

```
Week 1-2:  Build ObserverRuntime infrastructure
Week 3:    Migrate Network Observer
Week 4:    Migrate Node Observer
Week 5:    Migrate Deployments Observer
Week 6:    Migrate Scheduler Observer
Week 7:    Migrate Container Runtime Observer
Week 8:    Migrate Container API Observer
Week 9:    Prepare observer framework (generators, docs)
Week 10:   Final integration & documentation

Total: 10 weeks
```

**Checkpoints:**
- After Week 2: Infrastructure done, proceed?
- After Week 4: 2 observers migrated, pattern validated?
- After Week 6: tapio-controller ready?
- After Week 8: All observers migrated, production-ready?

---

## References

### Internal Documents

- [ADR 002: Observer Consolidation](./002-tapio-observer-consolidation.md)
- [ADR 008: Node Observer PMC](./008-node-observer-pmc-ebpf.md)
- [CLAUDE.md: Development Standards](../CLAUDE.md)

### External References

- Grafana Beyla: Component architecture pattern
- Honeycomb Agent: Watcher-based configuration
- Brendan Gregg: eBPF Performance Tools
- OpenTelemetry: Semantic conventions

---

## Approval Status

### Phase 1: Infrastructure (Week 1-3) - ✅ APPROVED

- [x] Architecture approved
- [x] Timeline approved (3 weeks, not 2)
- [x] Test observer requirement added
- [x] Setup/Teardown methods added to EventProcessor
- [x] Multi-emitter policy: Best Effort ✅
- [x] Rollback strategy: Dual deployment ✅
- [x] Hot reload: Phase 2/3 (not Phase 1) ✅

**Next Step:** Start implementing ObserverRuntime core interfaces.

---

### Phase 1.5: Network Observer Migration (Week 4-6) - 🛑 CHECKPOINT

- [ ] Architecture validated in Phase 1
- [ ] Network observer migrated
- [ ] Dev/staging tested
- [ ] Production soak test (2 weeks)
- [ ] Performance validated
- [ ] No regressions confirmed

**Decision Point:** Proceed to Phase 2 ONLY if all criteria met.

---

### Phase 2-3: Remaining Observers (Week 7-14) - ⏳ PENDING

**Conditional on Phase 1.5 success.**

- [ ] Phase 1.5 checkpoint passed
- [ ] All observers migrated
- [ ] Production deployed
- [ ] Documentation complete

**Final approval required after Phase 1.5 checkpoint.**

---

## Document Version

- **Version:** 1.0 (Revised)
- **Date:** 2025-01-03
- **Status:** Phase 1 Approved, Phase 1.5+ Conditional
- **Next Review:** End of Week 3 (Phase 1 completion)
- **Checkpoint Review:** End of Week 6 (Phase 1.5 completion)
