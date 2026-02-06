# Tapio Platform Architecture - Required Services Model

> **NOTE**: NATS references in this document are outdated. TAPIO now uses **POLKU** (gRPC event gateway) for all event transport to AHTI.

## Executive Summary

Based on extensive research of Prometheus, Beyla, and Grafana patterns, Tapio adopts a **platform-first architecture** where a minimal Context Service is **required for all deployments** (FREE and ENTERPRISE). This eliminates standalone/shared dichotomy and provides superior resource efficiency even for single-observer deployments.

**Key Decision:** Following the **Prometheus Operator pattern** - a single Helm chart that installs both the operator and CRDs, allowing users to manage Tapio via **either Helm values OR Kubernetes CRs** (best of both worlds).

**Deployment Strategy:**
- **Helm + Operator** (like kube-prometheus-stack)
- Single command: `helm install tapio tapio/tapio-stack`
- Installs: Operator + CRDs + Default TapioStack CR
- Users can manage via Helm OR kubectl (their choice)

## Validated Patterns from Industry Leaders

### Pattern 1: Prometheus Operator (Helm + Operator)
```bash
# Single helm install deploys operator + CRDs + Prometheus instance
helm install prometheus prometheus-community/kube-prometheus-stack

# Users can then manage via:
# - Helm values: helm upgrade prometheus -f values.yaml
# - CRs: kubectl apply -f prometheus-cr.yaml
```

**Why this works:** Helm installs the operator, operator reconciles CRs (from Helm OR kubectl).

### Pattern 2: Beyla (Platform Mode)
Beyla offers k8s-cache service (undocumented, for advanced users):

```yaml
# k8s-cache (Deployment) - Shared K8s metadata cache
# beyla (DaemonSet) - Connects to k8s-cache via gRPC
config:
  meta_cache_address: k8s-cache:50055
```

**Memory efficiency:** 20MB per DaemonSet + 50MB cache (vs 70MB per DaemonSet standalone)

**Reference:** `/Users/yair/projects/beyla/test/integration/k8s/manifests/06-beyla-external-informer.yml`

### Tapio's Approach: Combine Both Patterns

- **Prometheus pattern:** Helm installs operator + CRDs
- **Beyla pattern:** Shared Context Service for efficiency
- **Result:** Simple deployment with maximum flexibility

## Tapio Platform Architecture

### Required Minimum Deployment

```
┌─────────────────────────────────────────────────────┐
│    Tapio Context Service (REQUIRED - Deployment)    │
├─────────────────────────────────────────────────────┤
│  Components:                                        │
│  - K8s SharedIndexInformer (Pods, Deploys, etc.)   │
│  - NATS KV Cache (indexed by IP, PID, UID)         │
│  - gRPC Server (port 50051)                        │
│                                                     │
│  Resource Usage: ~50MB                              │
│  Replicas: 1 (can be HA with leader election)      │
└─────────────────────────────────────────────────────┘
              ▲          ▲          ▲
              │          │          │
         gRPC │     gRPC │     gRPC │
              │          │          │
    ┌─────────┴──┐  ┌────┴────┐  ┌─┴─────────┐
    │  Network   │  │Scheduler│  │ Runtime   │
    │  Observer  │  │Observer │  │ Observer  │
    │ (DaemonSet)│  │(Deploy) │  │(DaemonSet)│
    │   ~20MB    │  │  ~15MB  │  │   ~20MB   │
    └────────────┘  └─────────┘  └───────────┘
```

### Resource Comparison

**Without Platform (Each Observer Standalone):**
```
Network Observer:  70MB (own K8s informer)
Scheduler Observer: 50MB (own K8s informer)
Runtime Observer:   70MB (own K8s informer)
─────────────────────────────────────────
Total:             190MB (3 duplicate informers!)
```

**With Platform (Shared Context Service):**
```
Context Service:    50MB (1 shared informer)
Network Observer:   20MB (gRPC client only)
Scheduler Observer: 15MB (gRPC client only)
Runtime Observer:   20MB (gRPC client only)
─────────────────────────────────────────
Total:             105MB (55% reduction!)
```

**Even for 1 observer, platform is better:**
- Standalone: 70MB
- Platform: 50MB context + 20MB observer = 70MB (same), but gets K8s updates faster (no per-node polling)

## Architecture Levels

### Level 0: Context Service (FREE - Required Minimum)

**Responsibilities:**
- Watch K8s API (Pods, Deployments, Services, Nodes, Events)
- Index by multiple keys: IP, PID, UID, container ID
- Pre-compute OTEL attributes (Beyla pattern)
- Serve metadata via gRPC (low latency)
- Publish changes to NATS KV (async updates)

**Why Required:**
1. **K8s metadata is mandatory** - IPs alone are useless to users
2. **Resource efficiency** - Even 1 observer benefits from optimized cache
3. **Fast enrichment** - Pre-computed OTEL attributes (no per-event computation)
4. **Consistent data** - Single source of truth for all observers
5. **Foundation for growth** - Easy to add more observers later

**Implementation Pattern (from Beyla):**
```go
// Context Service = k8s-cache pattern
type ContextService struct {
    informers cache.SharedIndexInformer  // K8s watches
    store     *Store                      // Multi-index cache
    grpcSrv   *grpc.Server               // gRPC API
    nats      nats.KeyValue              // NATS KV for pubsub
}

// Multi-index store (Beyla pattern)
type Store struct {
    // Multiple indexes for O(1) lookup
    objectMetaByIP    map[string]*CachedObjMeta  // Network events
    objectMetaByUID   map[string]*CachedObjMeta  // Scheduler events
    containerByPID    map[uint32]*CachedObjMeta  // Runtime events

    // Pre-computed OTEL attributes (performance!)
    type CachedObjMeta struct {
        Meta             *informer.ObjectMeta
        OTELResourceMeta map[string]string  // Computed once!
    }
}
```

**gRPC API:**
```protobuf
service ContextService {
    rpc GetPodByIP(IPRequest) returns (PodMetadata);
    rpc GetPodByUID(UIDRequest) returns (PodMetadata);
    rpc GetDeploymentByUID(UIDRequest) returns (DeploymentMetadata);
    rpc WatchUpdates(stream UpdateRequest) returns (stream MetadataUpdate);
}

message PodMetadata {
    string name = 1;
    string namespace = 2;
    map<string, string> labels = 3;
    map<string, string> annotations = 4;
    map<string, string> otel_attributes = 5;  // Pre-computed!
    string owner_kind = 6;  // Deployment, StatefulSet, etc.
    string owner_name = 7;
}
```

### Level 1: Observers (FREE)

**Network Observer (DaemonSet):**
```go
type NetworkObserver struct {
    contextClient pb.ContextServiceClient  // gRPC to Context Service
    tracer        trace.Tracer
    metrics       *base.ObserverMetrics
}

func (o *NetworkObserver) enrichEvent(event *TCPConnectEvent) *domain.ObserverEvent {
    // Fast enrichment via gRPC (pre-computed attributes)
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
    defer cancel()

    pod, err := o.contextClient.GetPodByIP(ctx, &pb.IPRequest{
        IP: event.DestIP,
    })
    if err != nil {
        o.metrics.RecordEnrichmentFailed(ctx, "network", "k8s_lookup")
        return event  // Degrade gracefully, send with IP only
    }

    // OTEL attributes already computed by Context Service!
    event.Attributes = pod.OtelAttributes
    return event
}
```

**Scheduler Observer (Deployment - singleton):**
```go
type SchedulerObserver struct {
    contextClient pb.ContextServiceClient
    // Watches kube-scheduler metrics endpoint
    // Watches K8s Events API (via Context Service informer)
}

func (o *SchedulerObserver) handleFailedScheduling(event *v1.Event) {
    // Enrich with Deployment/StatefulSet context
    pod, _ := o.contextClient.GetPodByUID(ctx, &pb.UIDRequest{
        UID: string(event.InvolvedObject.UID),
    })

    // Emit diagnostic event with owner context
    observerEvent := &domain.ObserverEvent{
        Type: "scheduling_failed",
        Attributes: map[string]string{
            "pod.name":         event.InvolvedObject.Name,
            "deployment.name":  pod.OwnerName,  // From Context Service
            "failure.reason":   parseReason(event.Message),
        },
    }
}
```

### Level 2: Optional Services (ENTERPRISE)

**Semantic Correlation Service:**
- Consumes events from all observers (via NATS)
- Correlates across observer boundaries
- Detects patterns (e.g., "DNS failure → connection timeout → pod restart")
- Generates root cause hypotheses
- **This is the ENTERPRISE moat** (not Context Service)

**Historical Context Store:**
- Time-series database for event history
- Enables "why did this fail 3 days ago?" queries
- Trend analysis and anomaly detection

**AI Recommendation Engine:**
- Analyzes correlated events
- Suggests fixes ("Increase memory limit to 2Gi")
- Learns from user feedback

## Deployment Models

### Installation: Single Helm Chart

```bash
# Add Tapio helm repository
helm repo add tapio https://charts.tapio.io

# Install Tapio (operator + platform + observers)
helm install tapio tapio/tapio-stack
```

**What gets deployed:**
1. **Tapio Operator** - Reconciles TapioStack CRs
2. **CRDs** - TapioStack, TapioObserver custom resources
3. **Default TapioStack CR** - Creates Context Service + Network Observer
4. **Context Service** - Shared K8s metadata cache
5. **Network Observer** - DaemonSet for network diagnostics

### Management: Two Options (User's Choice)

#### Option A: Helm-Managed (Simple)

**Initial install with custom observers:**
```bash
helm install tapio tapio/tapio-stack -f values.yaml
```

`values.yaml`:
```yaml
stack:
  observers:
    network:
      enabled: true
      protocols: [TCP, UDP]
    scheduler:
      enabled: true
    runtime:
      enabled: false
  tier: free
```

**Add observer later:**
```bash
helm upgrade tapio tapio/tapio-stack --set stack.observers.runtime.enabled=true
```

**Upgrade to enterprise:**
```bash
helm upgrade tapio tapio/tapio-stack --set stack.tier=enterprise
```

#### Option B: CR-Managed (GitOps)

**Create TapioStack CR:**
```yaml
apiVersion: tapio.io/v1alpha1
kind: TapioStack
metadata:
  name: tapio
  namespace: tapio
spec:
  platform:
    contextService:
      replicas: 1

  observers:
    - type: network
      enabled: true
      config:
        protocols: [TCP, UDP]

    - type: scheduler
      enabled: true

  tier: free
```

**Apply with kubectl:**
```bash
kubectl apply -f tapio-stack.yaml
# Operator reconciles and deploys observers
```

**Add observer later:**
```bash
kubectl edit tapiostack tapio -n tapio
# Add "runtime" observer to spec.observers
# Operator automatically deploys it
```

### Willie's User Journey

#### Day 1: Install Tapio
```bash
helm repo add tapio https://charts.tapio.io
helm install tapio tapio/tapio-stack
```

**Output:**
```
✓ Tapio Operator deployed
✓ CRDs installed (TapioStack, TapioObserver)
✓ Context Service deployed (50MB)
✓ Network Observer deployed (20MB per node)
✓ Tapio is ready!

View status:
  kubectl get tapiostack -n tapio
  kubectl get pods -n tapio

Customize:
  helm upgrade tapio tapio/tapio-stack -f values.yaml
  OR
  kubectl edit tapiostack tapio -n tapio
```

#### Week 3: Willie Adds Scheduler Observer
```bash
# Option 1: Helm
helm upgrade tapio tapio/tapio-stack --set stack.observers.scheduler.enabled=true

# Option 2: kubectl
kubectl edit tapiostack tapio -n tapio
# Add scheduler to observers list
```

**Operator automatically:**
- Deploys Scheduler Observer (Deployment, 15MB)
- Connects it to existing Context Service
- No additional platform overhead!

#### Month 6: Willie Upgrades to Enterprise
```bash
helm upgrade tapio tapio/tapio-stack --set stack.tier=enterprise
```

**Operator automatically:**
- Deploys Semantic Correlation Service
- Deploys TimescaleDB for historical storage
- Deploys AI Recommendation Engine
- Keeps existing observers running (no downtime)

## What Makes Context Service Different from Observers?

### Context Service (Platform Layer)
- **Scope:** Cluster-wide K8s metadata
- **Purpose:** Shared infrastructure for ALL observers
- **Data:** Facts only (Pod names, IPs, labels, annotations)
- **Intelligence:** ZERO - just caching and indexing
- **Deployment:** 1 replica per cluster (can be HA)
- **License:** FREE (required minimum)

### Observers (Data Collection Layer)
- **Scope:** Specific observability domain (network, scheduler, runtime)
- **Purpose:** Detect failures and gather diagnostic facts
- **Data:** Events with "what failed" and "why" (within domain)
- **Intelligence:** Domain-specific detection (e.g., "connection_refused" vs "syn_timeout")
- **Deployment:** DaemonSet or Deployment per observer
- **License:** FREE (community) or PAID (premium features)

### Semantic Service (Intelligence Layer - ENTERPRISE ONLY)
- **Scope:** Cross-observer correlation
- **Purpose:** Find root causes across multiple domains
- **Data:** Hypotheses and recommendations
- **Intelligence:** HIGH - pattern recognition, AI analysis
- **Deployment:** 1 replica per cluster
- **License:** PAID only

## Why This Architecture Wins

### 1. Resource Efficiency from Day 1
- Even 1 observer benefits from optimized Context Service
- Adding observers has minimal incremental cost
- Shared infrastructure amortizes across workloads

### 2. Consistent User Experience
- Same deployment model for FREE and ENTERPRISE
- Upgrade path is simple (`--set tier=enterprise`)
- No architectural surprises

### 3. Competitive Positioning vs Beyla

**Beyla's Focus:**
- Application performance monitoring
- "Is my app slow?" observability
- Generic OTEL instrumentation

**Tapio's Focus:**
- Kubernetes infrastructure diagnostics
- "Why won't my infrastructure work?" diagnostics
- Problem-first design with context

**Unique Moats:**
1. **Scheduler observability** - Beyla doesn't do this
2. **Diagnostic-first events** - Not just metrics, but "why it failed"
3. **Semantic correlation (ENTERPRISE)** - Cross-observer intelligence
4. **Required platform** - Makes adding observers trivial (Willie can start small, grow easily)

### 4. Clean Separation of Concerns

```
Context Service:    Dumb cache (K8s metadata only)
Observers:          Domain sensors (detect failures)
Semantic Service:   Smart intelligence (find root causes)
```

This makes FREE tier simple, ENTERPRISE tier valuable.

## Implementation Phases

### Phase 1: Operator + CRDs
- [ ] Bootstrap operator with Kubebuilder (`kubebuilder init --domain tapio.io`)
- [ ] Create TapioStack CRD (`kubebuilder create api --kind TapioStack`)
- [ ] Implement reconciliation loop (Context Service + Observers)
- [ ] Unit tests for operator logic

### Phase 2: Context Service MVP
- [ ] K8s SharedIndexInformer (Pods, Deployments, Services)
- [ ] Multi-index store (IP, UID, PID)
- [ ] gRPC server with GetPodByIP, GetPodByUID
- [ ] Pre-compute OTEL attributes (Beyla pattern)
- [ ] NATS KV integration for change notifications

### Phase 3: Observer Updates
- [ ] Update Network Observer to use Context Service gRPC client
- [ ] Update Scheduler Observer to use Context Service
- [ ] Remove embedded informers from observers
- [ ] Graceful degradation when Context Service unavailable
- [ ] Observer-specific CRDs (TapioObserver)

### Phase 4: Helm Chart
- [ ] Helm chart structure (`charts/tapio-stack/`)
- [ ] Operator deployment templates
- [ ] CRD templates
- [ ] Default TapioStack CR template
- [ ] values.yaml with all configuration options
- [ ] Chart documentation

### Phase 5: Testing & Documentation
- [ ] E2E tests for operator reconciliation
- [ ] Integration tests for Helm chart
- [ ] Willie scenario walkthrough docs
- [ ] GitOps examples (ArgoCD, Flux)
- [ ] Troubleshooting guide

### Phase 6: Enterprise Features
- [ ] Semantic Correlation Service (reconciled by operator)
- [ ] Historical Context Store (TimescaleDB)
- [ ] AI Recommendation Engine
- [ ] Enterprise tier in TapioStack CRD spec

## Implementation Details

### Helm Chart Structure

```
charts/tapio-stack/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── operator/
│   │   ├── deployment.yaml           # Tapio Operator
│   │   ├── service-account.yaml
│   │   ├── role.yaml                 # RBAC for operator
│   │   └── rolebinding.yaml
│   ├── crds/
│   │   ├── tapiostack-crd.yaml       # TapioStack CRD
│   │   └── tapioobserver-crd.yaml    # TapioObserver CRD
│   └── instances/
│       └── default-tapiostack.yaml   # Default TapioStack CR
└── README.md
```

### values.yaml Structure

```yaml
# Operator configuration
operator:
  image:
    repository: ghcr.io/yairfalse/tapio-operator
    tag: v1.0.0
    pullPolicy: IfNotPresent
  replicas: 1
  resources:
    limits:
      memory: 128Mi
    requests:
      memory: 64Mi

# Default TapioStack configuration
stack:
  name: tapio
  namespace: tapio

  # Platform components
  platform:
    contextService:
      replicas: 1
      image:
        repository: ghcr.io/yairfalse/tapio-context-service
        tag: v1.0.0
      resources:
        limits:
          memory: 128Mi
        requests:
          memory: 64Mi

    nats:
      enabled: true
      replicas: 1

  # Observers
  observers:
    network:
      enabled: true
      image:
        repository: ghcr.io/yairfalse/tapio-network-observer
        tag: v1.0.0
      config:
        protocols: [TCP]

    scheduler:
      enabled: false
      image:
        repository: ghcr.io/yairfalse/tapio-scheduler-observer
        tag: v1.0.0

    runtime:
      enabled: false
      image:
        repository: ghcr.io/yairfalse/tapio-runtime-observer
        tag: v1.0.0

  # Tier (free or enterprise)
  tier: free

  # Enterprise features (only when tier=enterprise)
  enterprise:
    semantic:
      enabled: false
      replicas: 1
    historical:
      enabled: false
      storage: 100Gi
    ai:
      enabled: false
```

### TapioStack CRD Spec

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: tapiostacks.tapio.io
spec:
  group: tapio.io
  names:
    kind: TapioStack
    plural: tapiostacks
    singular: tapiostack
    shortNames: [ts]
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                platform:
                  type: object
                  properties:
                    contextService:
                      type: object
                      properties:
                        replicas:
                          type: integer
                          minimum: 1
                observers:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        enum: [network, scheduler, runtime]
                      enabled:
                        type: boolean
                      config:
                        type: object
                        x-kubernetes-preserve-unknown-fields: true
                tier:
                  type: string
                  enum: [free, enterprise]
            status:
              type: object
              properties:
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      lastTransitionTime:
                        type: string
                        format: date-time
                      reason:
                        type: string
                      message:
                        type: string
```

### Operator Reconciliation Logic

```go
func (r *TapioStackReconciler) Reconcile(ctx context.Context, req Request) (Result, error) {
    stack := &tapiov1alpha1.TapioStack{}
    if err := r.Get(ctx, req.NamespacedName, stack); err != nil {
        return Result{}, client.IgnoreNotFound(err)
    }

    // 1. Ensure Context Service
    if err := r.ensureContextService(ctx, stack); err != nil {
        r.setCondition(stack, "ContextServiceReady", "False", err.Error())
        return Result{RequeueAfter: 10 * time.Second}, err
    }
    r.setCondition(stack, "ContextServiceReady", "True", "Context Service deployed")

    // 2. Wait for Context Service to be ready
    if !r.isContextServiceReady(ctx, stack) {
        return Result{RequeueAfter: 5 * time.Second}, nil
    }

    // 3. Ensure Observers (based on spec.observers)
    for _, observer := range stack.Spec.Observers {
        if !observer.Enabled {
            if err := r.deleteObserver(ctx, stack, observer.Type); err != nil {
                return Result{}, err
            }
            continue
        }

        if err := r.ensureObserver(ctx, stack, observer); err != nil {
            r.setCondition(stack, "ObserversReady", "False", err.Error())
            return Result{RequeueAfter: 10 * time.Second}, err
        }
    }
    r.setCondition(stack, "ObserversReady", "True", "All observers deployed")

    // 4. Enterprise features (if tier=enterprise)
    if stack.Spec.Tier == "enterprise" {
        if err := r.ensureEnterpriseServices(ctx, stack); err != nil {
            r.setCondition(stack, "EnterpriseReady", "False", err.Error())
            return Result{RequeueAfter: 10 * time.Second}, err
        }
        r.setCondition(stack, "EnterpriseReady", "True", "Enterprise services deployed")
    }

    // Update status
    if err := r.Status().Update(ctx, stack); err != nil {
        return Result{}, err
    }

    return Result{}, nil
}
```

## Design Decisions Finalized

### ✅ Deployment Method: Helm + Operator
- Single `helm install` deploys operator + CRDs + default stack
- Users manage via Helm values OR kubectl CRs (their choice)
- Follows proven Prometheus Operator pattern

### ✅ Platform-First: Context Service Required
- Shared K8s metadata cache for all observers
- Resource efficiency from day 1 (even 1 observer)
- Follows Beyla k8s-cache pattern

### ✅ Observer Deployment Types
- **Network Observer:** DaemonSet (per-node eBPF)
- **Scheduler Observer:** Deployment (singleton, watches centralized APIs)
- **Runtime Observer:** DaemonSet (per-node container events)

### ✅ Management Flexibility
- **Helm users:** `helm upgrade` with values.yaml
- **GitOps users:** `kubectl apply` with CRs
- **Operator reconciles both** - no conflict

### ✅ Tier Model
- **FREE:** Context Service + Observers (diagnostics)
- **ENTERPRISE:** + Semantic Correlation + AI (intelligence)

## Success Metrics

**Resource Efficiency:**
- 55% memory reduction (3 observers: 190MB → 105MB)
- Single K8s watch per cluster (not per node × observers)
- Pre-computed OTEL attributes (no per-event computation)

**User Experience:**
- 2-step installation (platform + observer)
- Add observers with zero platform overhead
- Simple upgrade path to ENTERPRISE

**Competitive Position:**
- Same resource efficiency as Beyla's k8s-cache mode
- But with Kubernetes-first diagnostic focus
- Clear differentiation: diagnostics vs observability

## References

- Beyla k8s-cache implementation: `/Users/yair/projects/beyla/cmd/k8s-cache/`
- Beyla platform mode manifest: `/Users/yair/projects/beyla/test/integration/k8s/manifests/06-beyla-external-informer.yml`
- Prometheus SharedInformer patterns: `/Users/yair/projects/prometheus/discovery/kubernetes/`
- Architecture research findings: `/Users/yair/projects/tapio/docs/ARCHITECTURE_RESEARCH_FINDINGS.md`
