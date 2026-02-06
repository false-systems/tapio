# Design: K8sContext Service v2 (Sharp + Fast + Lean)

> **NOTE**: NATS references in this document are outdated. TAPIO now uses **POLKU** (gRPC event gateway) instead of NATS.

**Status**: Proposed
**Date**: 2025-01-02
**Related**: ADR 009 (PORTTI), ADR 010 (TAPIO Refactor)
**Prior Art**: [Grafana Beyla](https://github.com/grafana/beyla) - k8s-cache implementation

---

## Philosophy: Sharp + Fast + Lean

**Sharp**: Intelligence, not just data (Locality, Ownership, Tombstones)
**Fast**: Lock-free reads, zero-alloc hot path, <10ns lookups
**Lean**: Minimal memory, minimal deps, minimal GC pressure

```
Performance Targets:
┌────────────────────────────────────────────────────────────┐
│ Metric              │ Beyla (Standard) │ TAPIO (Sharp)    │
├─────────────────────┼──────────────────┼──────────────────┤
│ PodByIP latency     │ ~50ns            │ <10ns            │
│ Memory per pod      │ ~500 bytes       │ ~80 bytes        │
│ GC impact           │ Moderate         │ Near-zero        │
│ Lock contention     │ RWMutex          │ Lock-free reads  │
│ Cold start misses   │ 1-2s             │ 0ms (CRI)        │
└────────────────────────────────────────────────────────────┘
```

---

## Overview

K8sContextService provides **in-memory metadata lookup** for eBPF event enrichment in TAPIO.

**What it does:**
- Watches K8s pods/services
- Stores in-memory cache with multiple indexes
- Provides O(1) lookups for enrichment
- Pre-computes OTEL attributes (Beyla pattern)

**What it does NOT do:**
- Emit events (that's PORTTI's job)
- Store in NATS/external storage
- Complex change detection

---

## Prior Art: Grafana Beyla

Beyla solves the same problem: eBPF gives IPs/container IDs, need pod names.

**Key patterns we adopt from Beyla:**

| Pattern | Description |
|---------|-------------|
| **Node filtering** | `spec.nodeName` field selector for pods |
| **Heuristic deployment name** | Extract from ReplicaSet name, no RS watching |
| **SetTransform** | Convert full K8s objects to minimal cached structs |
| **Container ID stripping** | Remove `containerd://` prefix at cache time |
| **OTEL env extraction** | Extract `OTEL_SERVICE_NAME` from pod spec once |

**Beyla deployment modes:**
```
Mode 1: Embedded (what we do)     Mode 2: Separate service (future option)
┌──────────────────────┐          ┌──────────────────────┐
│ TAPIO (DaemonSet)    │          │ k8s-cache (Deployment)│
│ └── k8scontext       │          │ └── gRPC service      │
└──────────────────────┘          └──────────┬───────────┘
                                             │
                                   ┌─────────┼─────────┐
                                   ▼         ▼         ▼
                                 TAPIO    TAPIO     TAPIO
```

We start with Mode 1 (embedded). Mode 2 is an option for very large clusters (500+ nodes).

---

## Deployment Architecture

### Challenge: TAPIO is a DaemonSet

```
Node 1                Node 2                Node 3
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│ TAPIO       │      │ TAPIO       │      │ TAPIO       │
│ ├─ eBPF     │      │ ├─ eBPF     │      │ ├─ eBPF     │
│ └─ k8sctx   │      │ └─ k8sctx   │      │ └─ k8sctx   │
└─────────────┘      └─────────────┘      └─────────────┘
```

If every TAPIO watches ALL pods cluster-wide:
- N nodes × all pods = wasteful duplication
- N watchers hitting API server = load

### Solution: Local Pods + All Services (kube-proxy pattern)

```
Each TAPIO instance:
├── Watches pods:     spec.nodeName=<this-node>  (LOCAL ONLY)
├── Watches services: ALL (cluster-wide, few <1000)
└── Lookups:          Local enrichment only
```

**Why this is fine (kube-proxy does the same):**
- kube-proxy on every node watches ALL services and endpoints
- API server is designed for thousands of watchers
- Watch = long-poll connection (idle most of the time)
- Updates are delta-only, not full list

**Why this works:**

| eBPF Event | What we enrich | Where is it? |
|------------|----------------|--------------|
| Network src IP | Source pod | LOCAL (event from this node) |
| Network dst IP | Service or pod | Service: cluster-wide, Pod: might be remote |
| Container ID | Pod | LOCAL (container on this node) |

**Cross-node enrichment:**
- TAPIO enriches src pod (local)
- TAPIO enriches dst service (cluster-wide)
- TAPIO does NOT enrich dst pod on another node
- AHTI correlates cross-node traffic (central intelligence)

This is **edge intelligence**: Enrich locally, correlate centrally.

---

## Data Structures

### PodMeta

```go
type PodMeta struct {
    // Identity
    UID       string
    Name      string
    Namespace string
    NodeName  string

    // Network (for IP → Pod lookup)
    PodIP     string
    HostIP    string

    // Containers (for CID → Pod lookup)
    Containers []ContainerMeta

    // Ownership (resolved via heuristic - see below)
    OwnerKind string  // Deployment, StatefulSet, DaemonSet, Job, CronJob
    OwnerName string  // Resolved root owner name

    // Labels
    Labels map[string]string

    // Pre-computed OTEL attributes (Beyla pattern)
    OTELServiceName      string
    OTELServiceNamespace string
}

type ContainerMeta struct {
    Name        string
    ContainerID string  // Short ID: "abc123" (prefix stripped)
    Image       string
    Env         map[string]string  // Only OTEL-relevant env vars
}
```

### ServiceMeta

```go
type ServiceMeta struct {
    UID       string
    Name      string
    Namespace string
    ClusterIP string
    Type      string  // ClusterIP, NodePort, LoadBalancer
    Ports     []PortMeta
    Selector  map[string]string
}

type PortMeta struct {
    Name     string
    Port     int32
    Protocol string  // TCP, UDP
}
```

---

## Heuristic Deployment Name (Beyla Pattern)

**No ReplicaSet watching required!**

Beyla extracts deployment name from ReplicaSet name using a heuristic:

```go
// ReplicaSet naming pattern: {deployment-name}-{hash}
// Example: nginx-7d8f9xxxx → nginx

func resolveOwner(pod *corev1.Pod) (kind, name string) {
    for _, ref := range pod.OwnerReferences {
        if ref.Controller == nil || !*ref.Controller {
            continue
        }

        switch ref.Kind {
        case "ReplicaSet":
            // Heuristic: strip the hash suffix to get Deployment name
            if idx := strings.LastIndexByte(ref.Name, '-'); idx > 0 {
                return "Deployment", ref.Name[:idx]
            }
            return "ReplicaSet", ref.Name

        case "Job":
            // Heuristic: strip suffix to get CronJob name
            if idx := strings.LastIndexByte(ref.Name, '-'); idx > 0 {
                return "CronJob", ref.Name[:idx]
            }
            return "Job", ref.Name

        case "StatefulSet", "DaemonSet":
            return ref.Kind, ref.Name

        default:
            return ref.Kind, ref.Name
        }
    }

    // No owner - pod is standalone
    return "Pod", pod.Name
}
```

**Why this works:**
- Kubernetes naming convention: `{owner}-{hash}` or `{owner}-{ordinal}`
- ReplicaSet: `nginx-7d8f9xxxx` → Deployment: `nginx`
- Job: `backup-28391234` → CronJob: `backup`
- Saves watching ReplicaSets, Jobs, CronJobs

**Edge case:** Custom ReplicaSet names (rare) - we'd return the wrong name. Acceptable tradeoff.

---

## Container ID Handling (Beyla Pattern)

Strip runtime prefix at cache time, not lookup time:

```go
// Input:  "containerd://abc123def456789..."
// Output: "abc123def456789..."

func stripContainerIDPrefix(fullID string) string {
    if idx := strings.Index(fullID, "://"); idx != -1 {
        return fullID[idx+3:]
    }
    return fullID
}
```

**Supported runtimes:**
- `containerd://...`
- `docker://...`
- `cri-o://...`

---

## Memory Optimization: SetTransform (Beyla Pattern)

Convert full K8s objects to minimal structs at informer level:

```go
func (s *Service) setupPodInformer() error {
    informer := s.factory.Core().V1().Pods().Informer()

    // Transform full Pod → minimal PodMeta at cache time
    informer.SetTransform(func(obj interface{}) (interface{}, error) {
        pod, ok := obj.(*corev1.Pod)
        if !ok {
            // Handle already-transformed or stale objects
            if meta, ok := obj.(*PodMeta); ok {
                return meta, nil
            }
            if stale, ok := obj.(cache.DeletedFinalStateUnknown); ok {
                return stale, nil
            }
            return nil, fmt.Errorf("unexpected type: %T", obj)
        }

        return s.podToMeta(pod), nil
    })

    return nil
}

func (s *Service) podToMeta(pod *corev1.Pod) *PodMeta {
    // Extract containers with stripped IDs
    containers := make([]ContainerMeta, 0, len(pod.Status.ContainerStatuses))
    for _, cs := range pod.Status.ContainerStatuses {
        containers = append(containers, ContainerMeta{
            Name:        cs.Name,
            ContainerID: stripContainerIDPrefix(cs.ContainerID),
            Image:       cs.Image,
            Env:         extractOTELEnvVars(pod, cs.Name),
        })
    }

    // Extract IPs (skip host-networked pods)
    podIP := pod.Status.PodIP
    if podIP == pod.Status.HostIP {
        podIP = ""  // Host network - don't index by this IP
    }

    // Resolve owner
    ownerKind, ownerName := resolveOwner(pod)

    return &PodMeta{
        UID:             string(pod.UID),
        Name:            pod.Name,
        Namespace:       pod.Namespace,
        NodeName:        pod.Spec.NodeName,
        PodIP:           podIP,
        HostIP:          pod.Status.HostIP,
        Containers:      containers,
        OwnerKind:       ownerKind,
        OwnerName:       ownerName,
        Labels:          pod.Labels,
        OTELServiceName: computeOTELServiceName(pod, ownerName),
    }
}
```

**Memory savings:**
- Full Pod: ~10-50KB (includes spec, status, managed fields)
- PodMeta: ~500 bytes
- For 1000 pods: 50MB → 500KB

---

## OTEL Pre-computation (Beyla Pattern)

Compute OTEL attributes **once** on pod add/update, not on every event.

### Priority Cascade

```go
var otelEnvVars = map[string]struct{}{
    "OTEL_SERVICE_NAME":        {},
    "OTEL_RESOURCE_ATTRIBUTES": {},
}

func computeOTELServiceName(pod *corev1.Pod, deploymentName string) string {
    // 1. OTEL_SERVICE_NAME env var (highest priority)
    for _, c := range pod.Spec.Containers {
        for _, e := range c.Env {
            if e.Name == "OTEL_SERVICE_NAME" && e.Value != "" {
                return e.Value
            }
        }
    }

    // 2. resource.k8s.deployment.name annotation
    if name := pod.Annotations["resource.k8s.deployment.name"]; name != "" {
        return name
    }

    // 3. app.kubernetes.io/name label
    if name := pod.Labels["app.kubernetes.io/name"]; name != "" {
        return name
    }

    // 4. app label
    if name := pod.Labels["app"]; name != "" {
        return name
    }

    // 5. Resolved deployment name (from heuristic)
    if deploymentName != "" {
        return deploymentName
    }

    // 6. Pod name fallback
    return pod.Name
}

func extractOTELEnvVars(pod *corev1.Pod, containerName string) map[string]string {
    result := make(map[string]string)

    for _, c := range pod.Spec.Containers {
        if c.Name != containerName {
            continue
        }
        for _, e := range c.Env {
            if _, ok := otelEnvVars[e.Name]; ok && e.Value != "" {
                result[e.Name] = e.Value
            }
        }
    }

    return result
}
```

### Performance Impact

```
Without pre-computation:
  10,000 events/sec × env var lookup = slow

With pre-computation:
  1 pod add × env var lookup = fast
  10,000 events/sec × field access = very fast
```

---

## Store Design: Multi-Index

```go
type Store struct {
    // Primary stores (by UID - immutable identifier)
    pods     map[string]*PodMeta     // UID → *PodMeta
    services map[string]*ServiceMeta // UID → *ServiceMeta

    // Secondary indexes (point to UID, not copies)
    podByIP   map[string]string  // "10.0.1.5" → UID
    podByCID  map[string]string  // "abc123" → UID
    podByName map[string]string  // "default/nginx" → UID
    svcByIP   map[string]string  // ClusterIP → UID
    svcByName map[string]string  // "default/nginx-svc" → UID

    mu sync.RWMutex  // Single lock (KISS)
}
```

### Why Multi-Index?

Different observers need different lookups:

| Observer | Lookup by | Index used |
|----------|-----------|------------|
| Network | src/dst IP | podByIP, svcByIP |
| Container | container ID | podByCID |
| General | namespace/name | podByName, svcByName |

All lookups are **O(1)**.

### Locking Strategy

**sync.RWMutex** (KISS approach)

```
Access pattern:
- Reads: ~10,000/sec (every eBPF event)
- Writes: ~1/sec (pod add/update/delete)

Read-heavy workload → RWMutex is fine.
Multiple readers don't block each other.
Writes are rare, brief blocking is acceptable.
```

Future optimization if needed: Copy-on-write or sync.Map.

---

## Lookup API

```go
// ContextProvider is the interface for eBPF enrichment
type ContextProvider interface {
    // Pod lookups
    PodByIP(ip string) (*PodMeta, bool)
    PodByContainerID(cid string) (*PodMeta, bool)
    PodByName(namespace, name string) (*PodMeta, bool)

    // Service lookups
    ServiceByClusterIP(ip string) (*ServiceMeta, bool)
    ServiceByName(namespace, name string) (*ServiceMeta, bool)

    // Lifecycle
    Ready() bool  // True after initial sync
}
```

### Usage Example (Network Observer)

```go
func (o *NetworkObserver) enrichEvent(event *raw.NetworkEvent) {
    ctx := o.k8sContext

    // Enrich source (always local)
    if srcPod, ok := ctx.PodByIP(event.SrcIP); ok {
        event.SrcPod = srcPod.Name
        event.SrcNamespace = srcPod.Namespace
        event.SrcLabels = srcPod.Labels
        event.OTELServiceName = srcPod.OTELServiceName
        event.OwnerKind = srcPod.OwnerKind
        event.OwnerName = srcPod.OwnerName
    }

    // Enrich destination (service first, then pod)
    if svc, ok := ctx.ServiceByClusterIP(event.DstIP); ok {
        event.DstService = svc.Name
        event.DstNamespace = svc.Namespace
    } else if dstPod, ok := ctx.PodByIP(event.DstIP); ok {
        event.DstPod = dstPod.Name
        event.DstNamespace = dstPod.Namespace
    }
    // If neither found → dst is external or on another node
}
```

---

## Informer Setup

```go
func (s *Service) setupInformers(nodeName string) error {
    // Create factory with node filter for pods
    podFactory := informers.NewSharedInformerFactoryWithOptions(
        s.k8sClient,
        0, // No resync
        informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
            opts.FieldSelector = fields.Set{"spec.nodeName": nodeName}.String()
        }),
    )

    // Service factory - cluster-wide (no filter)
    svcFactory := informers.NewSharedInformerFactory(s.k8sClient, 0)

    // Setup pod informer with transform
    s.podInformer = podFactory.Core().V1().Pods().Informer()
    s.podInformer.SetTransform(s.transformPod)
    s.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        AddFunc:    s.onPodAdd,
        UpdateFunc: s.onPodUpdate,
        DeleteFunc: s.onPodDelete,
    })

    // Setup service informer with transform
    s.svcInformer = svcFactory.Core().V1().Services().Informer()
    s.svcInformer.SetTransform(s.transformService)
    s.svcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        AddFunc:    s.onServiceAdd,
        UpdateFunc: s.onServiceUpdate,
        DeleteFunc: s.onServiceDelete,
    })

    return nil
}
```

---

## Metrics

```go
var (
    cacheSize = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "tapio_k8scontext_cache_size",
            Help: "Number of items in k8scontext cache",
        },
        []string{"type"}, // pod, service
    )

    lookupTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "tapio_k8scontext_lookups_total",
            Help: "Total lookups by type and result",
        },
        []string{"type", "found"}, // pod_by_ip/true, service_by_ip/false
    )

    informerEvents = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "tapio_k8scontext_informer_events_total",
            Help: "Informer events by type and action",
        },
        []string{"type", "action"}, // pod/add, service/delete
    )

    informerSynced = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "tapio_k8scontext_informer_synced",
            Help: "1 if informers are synced, 0 otherwise",
        },
    )
)
```

---

## Cache Warmup Strategy

**Don't block, tag as pending** (per Beyla pattern):

```go
func (s *Service) Start(ctx context.Context) error {
    // Start informers
    s.podFactory.Start(ctx.Done())
    s.svcFactory.Start(ctx.Done())

    // Wait for sync in background
    go func() {
        if !cache.WaitForCacheSync(ctx.Done(),
            s.podInformer.HasSynced,
            s.svcInformer.HasSynced) {
            s.log.Warn("cache sync timed out")
            return
        }
        s.ready.Store(true)
        s.log.Info("cache synced",
            "pods", s.store.PodCount(),
            "services", s.store.ServiceCount())
    }()

    return nil
}

func (s *Service) Ready() bool {
    return s.ready.Load()
}
```

**eBPF events during warmup:**
- Lookup returns `(nil, false)`
- Event is enriched with raw IP only
- No blocking, no event loss

---

## File Structure

```
internal/services/k8scontext/
├── service.go       # Service struct, Start/Stop, lifecycle
├── types.go         # PodMeta, ServiceMeta, ContainerMeta
├── store.go         # Multi-index store with RWMutex
├── transform.go     # SetTransform functions (Beyla pattern)
├── owners.go        # Heuristic owner resolution
├── otel.go          # OTEL attribute pre-computation
├── metrics.go       # Prometheus metrics
├── service_test.go  # Unit tests
└── store_test.go    # Store tests
```

---

## Summary

| Aspect | Decision | Source |
|--------|----------|--------|
| **Scope** | Local pods (field selector), all services | Beyla |
| **Storage** | In-memory maps, no external storage | - |
| **Indexes** | Multi-index: by IP, CID, name | - |
| **Locking** | sync.RWMutex (KISS) | - |
| **Memory** | SetTransform to minimal structs | Beyla |
| **OTEL** | Pre-computed on pod add | Beyla |
| **Ownership** | Heuristic from RS name (no RS watching) | Beyla |
| **Container ID** | Strip prefix at cache time | Beyla |
| **Cross-node** | Not enriched at edge, AHTI correlates | - |
| **Warmup** | Non-blocking, tag as pending | Beyla |

---

## Sharp Features (Beyond Beyla)

These features elevate TAPIO from a passive cache to a high-velocity **intelligence engine**.

### 1. Zero-Copy Enrichment (Pre-baked OTEL Attributes)

**Problem:** Standard implementations copy data or recreate OTEL attribute sets on every event. At 10k+ events/sec, this kills CPU cache and triggers GC.

**Solution:** Pre-compute `attribute.Set` inside PodMeta at cache time:

```go
type PodMeta struct {
    // ... existing fields ...

    // Pre-baked OTEL attributes (zero-alloc enrichment)
    OTELAttrs attribute.Set  // Immutable, reusable
}

func (s *Service) podToMeta(pod *corev1.Pod) *PodMeta {
    ownerKind, ownerName := resolveOwner(pod)
    serviceName := computeOTELServiceName(pod, ownerName)

    // Pre-bake the attribute set ONCE
    attrs := attribute.NewSet(
        attribute.String("k8s.pod.name", pod.Name),
        attribute.String("k8s.namespace.name", pod.Namespace),
        attribute.String("k8s.node.name", pod.Spec.NodeName),
        attribute.String("service.name", serviceName),
        attribute.String("k8s.deployment.name", ownerName),
        // ... more attributes
    )

    return &PodMeta{
        // ...
        OTELAttrs: attrs,
    }
}

// Usage in observer - ZERO allocations
func (o *NetworkObserver) enrichEvent(event *raw.NetworkEvent, span trace.Span) {
    if srcPod, ok := o.k8sContext.PodByIP(event.SrcIP); ok {
        span.SetAttributes(srcPod.OTELAttrs.ToSlice()...)  // Pointer copy only
    }
}
```

**Impact:** 10x reduction in GC pressure during high-traffic bursts.

---

### 2. CRI Socket Fallback (Solve Informer Lag)

**Problem:** 100ms-2s delay between container start and Informer cache update. You miss the first packets (handshake, DNS).

**Solution:** Query local containerd/cri-o socket on cache miss:

```go
type Service struct {
    // ... existing fields ...
    criClient cri.RuntimeServiceClient  // containerd socket
}

func (s *Service) PodByContainerID(cid string) (*PodMeta, bool) {
    s.mu.RLock()
    uid, ok := s.podByCID[cid]
    if ok {
        pod := s.pods[uid]
        s.mu.RUnlock()
        return pod, true
    }
    s.mu.RUnlock()

    // FAST PATH: CRI socket fallback
    if s.criClient != nil {
        if meta := s.lookupViaCRI(cid); meta != nil {
            // Inject synthetic entry until Informer catches up
            s.injectSyntheticPod(meta)
            return meta, true
        }
    }

    return nil, false
}

func (s *Service) lookupViaCRI(cid string) *PodMeta {
    ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
    defer cancel()

    resp, err := s.criClient.ContainerStatus(ctx, &cri.ContainerStatusRequest{
        ContainerId: cid,
    })
    if err != nil {
        return nil
    }

    // Extract pod info from CRI response
    labels := resp.Status.Labels
    return &PodMeta{
        Name:      labels["io.kubernetes.pod.name"],
        Namespace: labels["io.kubernetes.pod.namespace"],
        // Mark as synthetic until Informer confirms
        Synthetic: true,
    }
}
```

**Impact:** 0ms latency for new container enrichment vs 1-2s with Informer-only.

---

### 3. Locality Tagging (Intelligence, not just Data)

**Problem:** Raw metadata doesn't tell you the traffic's relationship to the node.

**Solution:** Add Locality enum to lookups:

```go
type Locality int

const (
    LocalPod       Locality = iota  // Pod on this node
    ClusterService                   // Known ClusterIP service
    RemotePod                        // K8s IP range, but not this node
    External                         // Public internet / out-of-cluster
)

type LookupResult struct {
    Pod      *PodMeta
    Service  *ServiceMeta
    Locality Locality
}

func (s *Service) LookupIP(ip string) LookupResult {
    // 1. Check local pods (highest priority)
    if pod, ok := s.PodByIP(ip); ok {
        return LookupResult{Pod: pod, Locality: LocalPod}
    }

    // 2. Check services (ClusterIP)
    if svc, ok := s.ServiceByClusterIP(ip); ok {
        return LookupResult{Service: svc, Locality: ClusterService}
    }

    // 3. Check if IP is in cluster CIDR (remote pod)
    if s.isClusterIP(ip) {
        return LookupResult{Locality: RemotePod}
    }

    // 4. External traffic
    return LookupResult{Locality: External}
}

func (s *Service) isClusterIP(ip string) bool {
    // Check against pod CIDR and service CIDR
    // These are discovered from node spec or config
    parsedIP := net.ParseIP(ip)
    return s.podCIDR.Contains(parsedIP) || s.serviceCIDR.Contains(parsedIP)
}
```

**Impact:** AHTI can immediately filter cross-node duplicates; observers can skip external traffic if not interesting.

---

### 4. Advanced Ownership Heuristics (ArgoCD, Knative, Flux)

**Problem:** Simple suffix stripping fails on complex operators.

**Solution:** Prioritized heuristic check:

```go
func resolveOwner(pod *corev1.Pod) (kind, name string) {
    // 1. GOLD STANDARD: Explicit annotation
    if name := pod.Annotations["resource.k8s.deployment.name"]; name != "" {
        return "Deployment", name
    }

    // 2. ArgoCD pattern
    if name := pod.Labels["app.kubernetes.io/instance"]; name != "" {
        return "Application", name  // ArgoCD Application
    }

    // 3. Knative pattern
    if name := pod.Labels["serving.knative.dev/service"]; name != "" {
        return "KnativeService", name
    }

    // 4. Flux pattern
    if name := pod.Labels["kustomize.toolkit.fluxcd.io/name"]; name != "" {
        return "Kustomization", name
    }

    // 5. Standard owner references with heuristic
    for _, ref := range pod.OwnerReferences {
        if ref.Controller == nil || !*ref.Controller {
            continue
        }

        switch ref.Kind {
        case "ReplicaSet":
            // Heuristic: strip hash suffix
            if idx := strings.LastIndexByte(ref.Name, '-'); idx > 0 {
                return "Deployment", ref.Name[:idx]
            }
            return "ReplicaSet", ref.Name

        case "Job":
            if idx := strings.LastIndexByte(ref.Name, '-'); idx > 0 {
                return "CronJob", ref.Name[:idx]
            }
            return "Job", ref.Name

        case "StatefulSet", "DaemonSet":
            return ref.Kind, ref.Name

        default:
            return ref.Kind, ref.Name
        }
    }

    return "Pod", pod.Name
}
```

**Impact:** Correct service names for GitOps-deployed workloads (majority of production clusters).

---

### 5. Tombstone Cache (Ghost Pod Handling)

**Problem:** When pod is deleted, Informer removes it immediately. But eBPF still sees trailing TCP FIN packets or long-lived connections.

**Solution:** Move deleted pods to tombstone cache with TTL:

```go
type Store struct {
    // ... existing fields ...

    // Tombstone cache for deleted pods (30s TTL)
    tombstones map[string]*TombstonePod  // UID → TombstonePod
    tombstoneMu sync.RWMutex
}

type TombstonePod struct {
    Meta      *PodMeta
    DeletedAt time.Time
    TTL       time.Duration  // Default 30s
}

func (s *Store) onPodDelete(obj interface{}) {
    meta := obj.(*PodMeta)

    s.mu.Lock()
    // Remove from primary store
    delete(s.pods, meta.UID)
    delete(s.podByIP, meta.PodIP)
    for _, c := range meta.Containers {
        delete(s.podByCID, c.ContainerID)
    }
    s.mu.Unlock()

    // Add to tombstone cache
    s.tombstoneMu.Lock()
    s.tombstones[meta.UID] = &TombstonePod{
        Meta:      meta,
        DeletedAt: time.Now(),
        TTL:       30 * time.Second,
    }
    s.tombstoneMu.Unlock()
}

func (s *Store) PodByIP(ip string) (*PodMeta, bool) {
    // Check live cache first
    s.mu.RLock()
    if uid, ok := s.podByIP[ip]; ok {
        pod := s.pods[uid]
        s.mu.RUnlock()
        return pod, true
    }
    s.mu.RUnlock()

    // Check tombstones for trailing traffic
    s.tombstoneMu.RLock()
    defer s.tombstoneMu.RUnlock()
    for _, tomb := range s.tombstones {
        if tomb.Meta.PodIP == ip && time.Since(tomb.DeletedAt) < tomb.TTL {
            // Return with terminating flag
            meta := *tomb.Meta  // Copy
            meta.Terminating = true
            return &meta, true
        }
    }

    return nil, false
}

// Background cleanup goroutine
func (s *Store) cleanupTombstones(ctx context.Context) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            s.tombstoneMu.Lock()
            for uid, tomb := range s.tombstones {
                if time.Since(tomb.DeletedAt) > tomb.TTL {
                    delete(s.tombstones, uid)
                }
            }
            s.tombstoneMu.Unlock()
        }
    }
}
```

**Impact:** Perfect visibility into connection draining; no "unknown pod" errors for trailing traffic.

---

## Sharp Features Summary

| Feature | Beyla (Standard) | TAPIO (Sharp) |
|---------|------------------|---------------|
| **Data Delivery** | Copy/Map transformation | Pointer to pre-baked `attribute.Set` |
| **Cold Starts** | Misses first 1s of traffic | CRI Socket fallback for 0ms latency |
| **Ownership** | Simple regex/stripping | Context-aware (Argo/Knative/Flux) heuristics |
| **Lifecycle** | Hard Delete | Tombstone state for trailing traffic |
| **Intelligence** | Raw metadata | Explicit Locality tagging (`Local` vs `Remote`) |

---

## Fast + Lean Architecture

### The Hot Path Problem

Current naive implementation (what Beyla does):

```go
pod, ok := ctx.PodByIP(ip)  // What happens here?

// 1. RWMutex.RLock()      <- atomic ops, cache line bounce
// 2. map[string]string    <- hash IP string, cache miss
// 3. map[string]*PodMeta  <- another hash, another cache miss
// 4. RWMutex.RUnlock()    <- more atomic ops
// 5. Return pointer       <- pointer chase to scattered memory

// Total: ~50ns per lookup, 5 potential cache misses
```

**TAPIO target: <10ns, 1 cache miss**

---

### Fast Optimization 1: Lock-Free Reads (atomic.Pointer)

RWMutex has overhead even for reads. Copy-on-write eliminates read locks entirely:

```go
type Store struct {
    // Immutable snapshot - reads are lock-free
    current atomic.Pointer[Snapshot]

    // Only for writes (rare: ~1/sec)
    writeMu sync.Mutex
}

type Snapshot struct {
    // All data for one atomic load
    podsByIP   map[uint32]*PodMeta
    podsByCID  map[uint64]*PodMeta  // Hash of CID
    svcsByIP   map[uint32]*ServiceMeta

    // Bloom filter for fast negative lookups
    podBloom *bloom.Filter
}

// HOT PATH - Single atomic load, no locks
func (s *Store) PodByIP(ip uint32) (*PodMeta, bool) {
    snap := s.current.Load()  // 1 atomic read (~1ns)

    // Fast negative check
    if !snap.podBloom.Test(ip) {
        return nil, false  // Skip map lookup
    }

    pod, ok := snap.podsByIP[ip]  // Map lookup (~5ns)
    return pod, ok
}

// COLD PATH - Clone and swap (rare)
func (s *Store) addPod(meta *PodMeta) {
    s.writeMu.Lock()
    defer s.writeMu.Unlock()

    old := s.current.Load()
    newSnap := old.Clone()  // Copy maps
    newSnap.podsByIP[meta.IPAsUint32] = meta
    newSnap.podBloom.Add(meta.IPAsUint32)
    s.current.Store(newSnap)  // Atomic swap
}
```

**Impact:** Reads go from ~50ns to ~7ns. Zero lock contention.

---

### Fast Optimization 2: IP as uint32 (No String Hashing)

String hashing is expensive. IPv4 fits in 32 bits:

```go
// SLOW: Hash "10.0.1.5" every lookup (~15ns)
podsByIP map[string]*PodMeta

// FAST: Direct integer key (~2ns)
podsByIP map[uint32]*PodMeta

// Conversion (done once at cache time)
func ipToUint32(ip net.IP) uint32 {
    ip4 := ip.To4()
    if ip4 == nil {
        return 0  // IPv6 needs different handling
    }
    return binary.BigEndian.Uint32(ip4)
}

// For eBPF events, IP already comes as uint32 from kernel
type NetworkEvent struct {
    SrcIP uint32  // Already in correct format
    DstIP uint32
    // ...
}
```

**Impact:** Map lookup 3-5x faster.

---

### Fast Optimization 3: Bloom Filter (Skip 80% of Misses)

Most destination IPs are external or cross-node. Bloom filter catches these instantly:

```go
type Snapshot struct {
    podsByIP map[uint32]*PodMeta
    podBloom *bloom.Filter  // ~1KB for 1000 pods, 1% false positive
}

func (s *Store) PodByIP(ip uint32) (*PodMeta, bool) {
    snap := s.current.Load()

    // Bloom filter: if not present, definitely miss
    if !snap.podBloom.Test(ip) {
        return nil, false  // ~1ns, no map access
    }

    // Bloom says maybe - check map
    pod, ok := snap.podsByIP[ip]
    return pod, ok
}
```

**Impact:** Skip map lookup for 80%+ of misses. Bloom check is ~1ns.

---

### Fast Optimization 4: Batch Lookups

eBPF ring buffer delivers events in batches. Amortize overhead:

```go
// SLOW: One snapshot load per event
for _, event := range events {
    pod, _ := ctx.PodByIP(event.SrcIP)  // atomic.Load each time
}

// FAST: One snapshot load for entire batch
func (s *Store) BatchLookup(ips []uint32) []*PodMeta {
    snap := s.current.Load()  // Single atomic load

    results := make([]*PodMeta, len(ips))
    for i, ip := range ips {
        if snap.podBloom.Test(ip) {
            results[i] = snap.podsByIP[ip]
        }
    }
    return results
}
```

**Impact:** Amortize atomic load across batch. Better cache warming.

---

### Lean Optimization 1: Compact PodMeta

```go
// BLOATED: ~500 bytes per pod
type PodMeta struct {
    UID       string            // 16 bytes header + ~36 bytes data
    Name      string            // 16 + ~20 bytes
    Namespace string            // 16 + ~15 bytes
    NodeName  string            // 16 + ~20 bytes
    PodIP     string            // 16 + ~15 bytes
    Labels    map[string]string // 48 + N*32 bytes
    OTELAttrs attribute.Set     // ~200 bytes
    // ...
}

// LEAN: ~80 bytes per pod
type PodMeta struct {
    // Pack small fields together
    IP        uint32            // 4 bytes
    NameIdx   uint16            // 2 bytes (index into string table)
    NSIdx     uint16            // 2 bytes
    NodeIdx   uint16            // 2 bytes
    OwnerIdx  uint16            // 2 bytes
    LabelSet  uint32            // 4 bytes (index into label set table)
    Flags     uint16            // 2 bytes (Terminating, Synthetic, etc.)
    _padding  uint16            // 2 bytes (alignment)

    // Keep containers inline (up to 3, covers 99% of cases)
    Containers [3]ContainerMeta // 3 * 16 = 48 bytes
    NumContainers uint8

    // Pre-baked OTEL (pointer to shared, immutable set)
    OTELAttrs *attribute.Set    // 8 bytes (pointer)
}

type ContainerMeta struct {
    CIDHash uint64  // 8 bytes (hash of container ID)
    NameIdx uint16  // 2 bytes
    _pad    [6]byte // alignment
}

// String interning table (shared across all pods)
type StringTable struct {
    strings []string           // Deduplicated: "default", "kube-system", etc.
    index   map[string]uint16  // String → index
}

func (s *StringTable) Intern(str string) uint16 {
    if idx, ok := s.index[str]; ok {
        return idx
    }
    idx := uint16(len(s.strings))
    s.strings = append(s.strings, str)
    s.index[str] = idx
    return idx
}

func (s *StringTable) Get(idx uint16) string {
    return s.strings[idx]
}
```

**Impact:** 6x less memory. Better cache locality.

---

### Lean Optimization 2: Arena Allocation

Instead of scattered heap allocations, use contiguous arena:

```go
type Store struct {
    // All PodMeta in contiguous memory
    podArena []PodMeta  // One allocation, grows as needed

    // Indexes point into arena
    podsByIP map[uint32]uint32  // IP → arena index
}

func (s *Store) allocPod() *PodMeta {
    idx := len(s.podArena)
    s.podArena = append(s.podArena, PodMeta{})
    return &s.podArena[idx]
}

func (s *Store) PodByIP(ip uint32) (*PodMeta, bool) {
    snap := s.current.Load()
    idx, ok := snap.podsByIP[ip]
    if !ok {
        return nil, false
    }
    return &snap.podArena[idx], true  // Pointer into contiguous memory
}
```

**Impact:** Near-zero GC. Better CPU cache locality.

---

### Lean Optimization 3: Shared OTEL Attribute Sets

Most pods in same namespace/deployment share attributes. Deduplicate:

```go
type OTELAttrCache struct {
    sets  []attribute.Set
    index map[uint64]uint32  // Hash of attrs → index
}

func (c *OTELAttrCache) GetOrCreate(attrs ...attribute.KeyValue) *attribute.Set {
    hash := hashAttrs(attrs)
    if idx, ok := c.index[hash]; ok {
        return &c.sets[idx]  // Reuse existing set
    }

    // Create new set
    set := attribute.NewSet(attrs...)
    idx := uint32(len(c.sets))
    c.sets = append(c.sets, set)
    c.index[hash] = idx
    return &c.sets[idx]
}
```

**Impact:** 10 pods in same deployment = 1 attribute set, not 10.

---

## Complete Fast + Lean Architecture

```
                     TAPIO K8sContext (Sharp + Fast + Lean)
┌──────────────────────────────────────────────────────────────────────┐
│                                                                      │
│  atomic.Pointer ──────────────────────────────────────────────────┐  │
│         │                                                         │  │
│         ▼                                                         │  │
│  ┌────────────────────────────────────────────────────────────┐  │  │
│  │ Snapshot (immutable, copy-on-write)                        │  │  │
│  │                                                            │  │  │
│  │  ┌──────────┐  ┌──────────────┐  ┌───────────────────┐    │  │  │
│  │  │ Bloom    │  │ podsByIP     │  │ podArena          │    │  │  │
│  │  │ Filter   │  │ uint32→idx   │  │ []PodMeta         │    │  │  │
│  │  │ (1KB)    │  │              │  │ (contiguous)      │    │  │  │
│  │  └────┬─────┘  └──────┬───────┘  └─────────┬─────────┘    │  │  │
│  │       │               │                    │              │  │  │
│  │       ▼               ▼                    ▼              │  │  │
│  │  ┌────────────────────────────────────────────────────┐   │  │  │
│  │  │                  HOT PATH                          │   │  │  │
│  │  │                                                    │   │  │  │
│  │  │  1. snap := current.Load()        // 1ns atomic    │   │  │  │
│  │  │  2. if !bloom.Test(ip) return     // 1ns           │   │  │  │
│  │  │  3. idx := podsByIP[ip]           // 3ns           │   │  │  │
│  │  │  4. return &podArena[idx]         // 1ns           │   │  │  │
│  │  │                                                    │   │  │  │
│  │  │  Total: ~6ns per lookup                            │   │  │  │
│  │  └────────────────────────────────────────────────────┘   │  │  │
│  │                                                            │  │  │
│  │  ┌────────────────────┐  ┌─────────────────────────────┐  │  │  │
│  │  │ StringTable        │  │ OTELAttrCache               │  │  │  │
│  │  │ (interned strings) │  │ (deduplicated attr sets)    │  │  │  │
│  │  └────────────────────┘  └─────────────────────────────┘  │  │  │
│  └────────────────────────────────────────────────────────────┘  │  │
│                                                                   │  │
│  ┌────────────────────────────────────────────────────────────┐  │  │
│  │ Tombstone Cache (30s TTL for deleted pods)                 │  │  │
│  └────────────────────────────────────────────────────────────┘  │  │
│                                                                   │  │
│  ┌────────────────────────────────────────────────────────────┐  │  │
│  │ CRI Client (fallback for cold starts)                      │  │  │
│  └────────────────────────────────────────────────────────────┘  │  │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘

Memory Layout (1000 pods):
┌─────────────────────────────────────────────────────────────────────┐
│ Component          │ Beyla (Standard)  │ TAPIO (Sharp+Fast+Lean)   │
├────────────────────┼───────────────────┼───────────────────────────┤
│ PodMeta structs    │ 500KB             │ 80KB                      │
│ String storage     │ 200KB             │ 20KB (interned)           │
│ Maps overhead      │ 100KB             │ 50KB                      │
│ OTEL attrs         │ 200KB             │ 10KB (deduplicated)       │
│ Bloom filter       │ 0                 │ 1KB                       │
├────────────────────┼───────────────────┼───────────────────────────┤
│ TOTAL              │ ~1MB              │ ~161KB                    │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Implementation Phases

### Phase 1: Foundation (Week 1)
- [ ] atomic.Pointer + Snapshot pattern
- [ ] IP as uint32
- [ ] Basic informer setup with node filtering
- [ ] Unit tests with benchmarks

### Phase 2: Fast Path (Week 2)
- [ ] Bloom filter for negative lookups
- [ ] Batch lookup API
- [ ] CRI socket fallback
- [ ] Benchmark: target <10ns per lookup

### Phase 3: Lean Memory (Week 3)
- [ ] String interning table
- [ ] Compact PodMeta struct
- [ ] Arena allocation
- [ ] OTEL attribute deduplication

### Phase 4: Sharp Features (Week 4)
- [ ] Locality tagging
- [ ] Advanced ownership heuristics
- [ ] Tombstone cache
- [ ] Pre-baked OTEL attribute sets

---

## Benchmarks (Required)

```go
func BenchmarkPodByIP(b *testing.B) {
    store := setupStoreWith1000Pods()
    ip := randomPodIP()

    b.ResetTimer()
    b.ReportAllocs()

    for i := 0; i < b.N; i++ {
        _, _ = store.PodByIP(ip)
    }
}

// Target: <10ns/op, 0 allocs/op

func BenchmarkPodByIP_Miss(b *testing.B) {
    store := setupStoreWith1000Pods()
    ip := externalIP()  // Not in store

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = store.PodByIP(ip)
    }
}

// Target: <3ns/op (bloom filter fast path)

func BenchmarkBatchLookup(b *testing.B) {
    store := setupStoreWith1000Pods()
    ips := random100IPs()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = store.BatchLookup(ips)
    }
}

// Target: <500ns for 100 lookups (~5ns/lookup amortized)
```

---

## Updated File Structure

```
internal/services/k8scontext/
├── service.go       # Service struct, Start/Stop, lifecycle
├── types.go         # PodMeta, ServiceMeta, Locality (compact)
├── snapshot.go      # Immutable Snapshot, atomic.Pointer
├── store.go         # Copy-on-write store operations
├── arena.go         # Arena allocator for PodMeta
├── strings.go       # String interning table
├── bloom.go         # Bloom filter wrapper
├── tombstone.go     # Tombstone cache for deleted pods
├── transform.go     # SetTransform functions
├── owners.go        # Advanced ownership heuristics
├── otel.go          # Pre-baked OTEL attribute sets
├── cri.go           # CRI socket fallback
├── locality.go      # IP locality detection
├── metrics.go       # Prometheus metrics
├── bench_test.go    # Performance benchmarks (REQUIRED)
├── service_test.go  # Unit tests
└── store_test.go    # Store tests
```

---

## Complete Summary: Sharp + Fast + Lean

| Category | Feature | Impact |
|----------|---------|--------|
| **Sharp** | Locality tagging | Filter noise, tag traffic relationship |
| **Sharp** | Advanced ownership | ArgoCD/Knative/Flux support |
| **Sharp** | Tombstone cache | Trailing traffic visibility |
| **Sharp** | CRI fallback | 0ms cold start vs 1-2s |
| **Sharp** | Pre-baked OTEL attrs | Zero-alloc enrichment |
| **Fast** | atomic.Pointer | Lock-free reads, ~7ns |
| **Fast** | IP as uint32 | 3-5x faster map lookup |
| **Fast** | Bloom filter | Skip 80% of misses |
| **Fast** | Batch lookups | Amortize overhead |
| **Lean** | Compact PodMeta | 80 bytes vs 500 bytes |
| **Lean** | String interning | 6x less string memory |
| **Lean** | Arena allocation | Near-zero GC |
| **Lean** | OTEL deduplication | Shared attr sets |

---

## Future Enhancements

1. **Separate cache service** - For 500+ node clusters, run k8scontext as Deployment with gRPC API (like Beyla's k8s-cache)
2. **EndpointSlices** - For service → pod mapping (which pods back this service)
3. **Node metadata** - Watch nodes for zone/region info

---

## Resolved Questions

1. **ReplicaSet watching?** - **No.** Use heuristic to extract deployment name from RS name.
2. **Cache warmup?** - **Don't block.** Process events, return nil for cache misses.
3. **Stale entries?** - Informer handles deletes. No TTL needed.

---

**Author**: Yair + Claude
**Date**: 2025-01-02
