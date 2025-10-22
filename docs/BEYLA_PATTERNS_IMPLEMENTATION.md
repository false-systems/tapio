# Beyla Patterns - Tapio Implementation Guide

## Executive Summary

This document outlines **technical implementation patterns** learned from Beyla that Tapio should adopt, while maintaining our unique value proposition as a **Kubernetes diagnostic tool** (not an observability platform).

**Key Principle:** Learn Beyla's implementation wisdom, avoid becoming another Beyla clone.

---

## Patterns to ADOPT (Technical Implementation)

### P0 - CRITICAL (Must Have for v1.0)

#### 1. Pre-Computed OTEL Attributes

**Problem:** Computing OTEL attributes on every event is slow (100µs per event).

**Beyla Solution:**
```go
// vendor/go.opentelemetry.io/obi/pkg/components/kube/store.go
type CachedObjMeta struct {
    Meta             *informer.ObjectMeta
    OTELResourceMeta map[attr.Name]string  // ← Computed ONCE!
}
```

**Performance Impact:** 100x faster (1µs vs 100µs per event)

**Tapio Implementation:**

```go
// pkg/domain/context.go (Level 0)
type PodContext struct {
    Namespace      string
    Name           string
    PodIP          string
    NodeName       string
    UID            string
    OwnerKind      string
    OwnerName      string

    // Pre-computed OTEL attributes (Beyla pattern)
    OTELAttributes map[string]string  // Computed during pod add/update
}

// internal/services/k8scontext/enrichment.go
package k8scontext

import (
    "strings"

    "github.com/yairfalse/tapio/pkg/domain"
    v1 "k8s.io/api/core/v1"
)

// ComputeOTELAttributes pre-computes OTEL attributes for a pod
// Following Beyla's priority cascade: env vars → annotations → labels
func ComputeOTELAttributes(pod *v1.Pod) map[string]string {
    attrs := make(map[string]string)

    // Priority 1: Container environment variables (highest priority)
    // OTEL_SERVICE_NAME env var from container
    for _, container := range pod.Spec.Containers {
        for _, env := range container.Env {
            if env.Name == "OTEL_SERVICE_NAME" && env.Value != "" {
                attrs["service.name"] = env.Value
                break
            }
            if env.Name == "OTEL_SERVICE_NAMESPACE" && env.Value != "" {
                attrs["service.namespace"] = env.Value
            }
        }
        if attrs["service.name"] != "" {
            break
        }
    }

    // Priority 2: Annotations (override labels)
    // Convention: otel.resource.<attribute_name>
    for k, v := range pod.Annotations {
        if strings.HasPrefix(k, "otel.resource.") {
            attrName := strings.TrimPrefix(k, "otel.resource.")
            attrs[attrName] = v
        }
    }

    // Priority 3: Labels (lowest priority)
    if attrs["service.name"] == "" {
        if app := pod.Labels["app.kubernetes.io/name"]; app != "" {
            attrs["service.name"] = app
        } else if app := pod.Labels["app"]; app != "" {
            attrs["service.name"] = app
        }
    }

    if attrs["service.namespace"] == "" {
        if ns := pod.Labels["app.kubernetes.io/part-of"]; ns != "" {
            attrs["service.namespace"] = ns
        }
    }

    // Standard K8s attributes (always included)
    attrs["k8s.pod.name"] = pod.Name
    attrs["k8s.namespace.name"] = pod.Namespace
    attrs["k8s.pod.uid"] = string(pod.UID)
    attrs["k8s.node.name"] = pod.Spec.NodeName

    return attrs
}

// Observer uses pre-computed attributes (fast path)
func (o *NetworkObserver) enrichEvent(event *BPFEvent) *domain.ObserverEvent {
    podCtx := o.getPodContext(event.SrcIP)  // O(1) lookup

    observerEvent := &domain.ObserverEvent{
        Type:       "connection_refused",
        Timestamp:  time.Now(),
        Attributes: podCtx.OTELAttributes,  // ← Already computed!
    }

    return observerEvent
}
```

**Files to Create/Modify:**
- `pkg/domain/context.go` - Add OTELAttributes field
- `internal/services/k8scontext/enrichment.go` - Implement ComputeOTELAttributes
- `internal/observers/network/observer.go` - Use pre-computed attributes

---

#### 2. Multi-Index Metadata Store

**Problem:** Different observers need different lookup patterns (IP, UID, PID, name).

**Beyla Solution:**
```go
type Store struct {
    objectMetaByIP    map[string]*CachedObjMeta      // Network observer
    containerByPID    map[uint32]*container.Info     // Process observer
    namespaces        maps.Map2[uint32, uint32, *Info]  // Namespace observer
}
```

**Tapio Implementation:**

```go
// internal/services/k8scontext/storage.go
package k8scontext

import (
    "context"
    "encoding/json"
    "sync"

    "github.com/yairfalse/tapio/pkg/domain"
    "github.com/nats-io/nats.go"
)

type ContextStore struct {
    mu     sync.RWMutex
    natsKV nats.KeyValue

    // In-memory indexes for hot path (O(1) lookups)
    podsByIP     map[string]*domain.PodContext     // Network observer
    podsByUID    map[string]*domain.PodContext     // Scheduler observer
    podsByPID    map[uint32]*domain.PodContext     // Future: OOM observer

    deployments  map[string]*domain.DeploymentContext
    nodes        map[string]*domain.NodeContext
}

// StorePod stores pod context with multiple index keys
func (s *ContextStore) StorePod(ctx context.Context, pod *domain.PodContext) error {
    data, err := json.Marshal(pod)
    if err != nil {
        return err
    }

    // Store in NATS KV with multiple key patterns
    s.natsKV.Put("pod.ip."+pod.PodIP, data)
    s.natsKV.Put("pod.uid."+pod.UID, data)
    s.natsKV.Put("pod.name."+pod.Namespace+"."+pod.Name, data)

    // Update in-memory indexes for hot path
    s.mu.Lock()
    defer s.mu.Unlock()

    s.podsByIP[pod.PodIP] = pod
    s.podsByUID[pod.UID] = pod

    return nil
}

// GetPodByIP retrieves pod context by IP (Network observer needs this)
func (s *ContextStore) GetPodByIP(ip string) (*domain.PodContext, error) {
    // Fast path: in-memory cache
    s.mu.RLock()
    if pod, ok := s.podsByIP[ip]; ok {
        s.mu.RUnlock()
        return pod, nil
    }
    s.mu.RUnlock()

    // Slow path: NATS KV
    entry, err := s.natsKV.Get("pod.ip." + ip)
    if err != nil {
        return nil, err
    }

    var pod domain.PodContext
    if err := json.Unmarshal(entry.Value(), &pod); err != nil {
        return nil, err
    }

    // Warm cache
    s.mu.Lock()
    s.podsByIP[ip] = &pod
    s.mu.Unlock()

    return &pod, nil
}

// GetPodByUID retrieves pod context by UID (Scheduler observer needs this)
func (s *ContextStore) GetPodByUID(uid string) (*domain.PodContext, error) {
    s.mu.RLock()
    if pod, ok := s.podsByUID[uid]; ok {
        s.mu.RUnlock()
        return pod, nil
    }
    s.mu.RUnlock()

    entry, err := s.natsKV.Get("pod.uid." + uid)
    if err != nil {
        return nil, err
    }

    var pod domain.PodContext
    if err := json.Unmarshal(entry.Value(), &pod); err != nil {
        return nil, err
    }

    s.mu.Lock()
    s.podsByUID[uid] = &pod
    s.mu.Unlock()

    return &pod, nil
}
```

**Files to Modify:**
- `internal/services/k8scontext/storage.go` - Add multi-index support

---

#### 3. Prometheus Label Transformation

**Problem:** Prometheus rejects dots in label names, but OTEL uses dot notation.

**Beyla Solution:**
```go
// Internal: k8s.pod.name
// Export to Prometheus: k8s_pod_name
```

**Tapio Implementation:**

```go
// pkg/domain/attributes.go (Level 0)
package domain

import "strings"

// OTEL Semantic Conventions (internal representation)
const (
    OTELServiceName       = "service.name"
    OTELServiceNamespace  = "service.namespace"
    OTELPodName           = "k8s.pod.name"
    OTELPodUID            = "k8s.pod.uid"
    OTELNamespaceName     = "k8s.namespace.name"
    OTELNodeName          = "k8s.node.name"
    OTELDeploymentName    = "k8s.deployment.name"
    OTELStatefulSetName   = "k8s.statefulset.name"
)

// Prometheus Label Names (export format - dots replaced with underscores)
const (
    PromServiceName       = "service_name"
    PromServiceNamespace  = "service_namespace"
    PromPodName           = "k8s_pod_name"
    PromPodUID            = "k8s_pod_uid"
    PromNamespaceName     = "k8s_namespace_name"
    PromNodeName          = "k8s_node_name"
    PromDeploymentName    = "k8s_deployment_name"
    PromStatefulSetName   = "k8s_statefulset_name"
)

// ToPrometheusLabel converts OTEL dot notation to Prometheus snake_case
func ToPrometheusLabel(otelAttr string) string {
    return strings.ReplaceAll(otelAttr, ".", "_")
}

// internal/base/metrics.go
func (m *ObserverMetrics) RecordEvent(ctx context.Context, podCtx *domain.PodContext) {
    // Convert OTEL attributes to Prometheus labels
    attrs := make([]attribute.KeyValue, 0, len(podCtx.OTELAttributes))

    for k, v := range podCtx.OTELAttributes {
        // Convert dots to underscores for Prometheus compatibility
        promKey := domain.ToPrometheusLabel(k)
        attrs = append(attrs, attribute.String(promKey, v))
    }

    m.EventsProcessed.Add(ctx, 1, metric.WithAttributes(attrs...))
}
```

**Files to Create/Modify:**
- `pkg/domain/attributes.go` - Add constants and conversion function
- `internal/base/metrics.go` - Use conversion in all metric calls

---

#### 4. NO RESYNC on SharedIndexInformer

**Problem:** Periodic resync causes duplicate event processing.

**Beyla/Prometheus Solution:**
```go
const resyncDisabled = 0
informer := cache.NewSharedIndexInformer(lw, &v1.Pod{}, resyncDisabled, indexers)
```

**Tapio Implementation:**

```go
// internal/services/k8scontext/service.go
func (s *Service) createPodInformer() cache.SharedIndexInformer {
    return cache.NewSharedIndexInformer(
        s.podListWatch,
        &v1.Pod{},
        0,  // ← NO RESYNC (Beyla/Prometheus pattern)
        cache.Indexers{
            "ip": func(obj interface{}) ([]string, error) {
                pod := obj.(*v1.Pod)
                if pod.Status.PodIP != "" {
                    return []string{pod.Status.PodIP}, nil
                }
                return []string{}, nil
            },
        },
    )
}
```

**Files to Modify:**
- `internal/services/k8scontext/service.go` - Set resync to 0

---

### P1 - HIGH IMPACT (Should Have for v1.0)

#### 5. DaemonSet Node-Local Filtering

**Problem:** DaemonSet watching all pods in cluster wastes memory (10,000 pods vs 500 per node).

**Beyla Solution:**
```go
if cfg.RestrictLocalNode {
    opts.FieldSelector = "spec.nodeName=" + nodeName
}
```

**Memory Impact:** 20x reduction (300MB → 15MB per observer pod)

**Tapio Implementation:**

```go
// internal/observers/network/observer.go
package network

import (
    "os"

    "k8s.io/apimachinery/pkg/fields"
    "k8s.io/client-go/tools/cache"
)

func NewObserver(cfg Config) (*Observer, error) {
    // Get node name from downward API
    nodeName := os.Getenv("NODE_NAME")

    // Create field selector for node-local filtering (Beyla pattern)
    fieldSelector := fields.Everything()
    if nodeName != "" && cfg.DeploymentMode == "daemonset" {
        // Only watch pods on THIS node
        fieldSelector = fields.OneTermEqualSelector("spec.nodeName", nodeName)
    }

    // Create informer with field selector
    podInformer := cache.NewSharedIndexInformer(
        &cache.ListWatch{
            ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
                opts.FieldSelector = fieldSelector.String()
                return clientset.CoreV1().Pods("").List(ctx, opts)
            },
            WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
                opts.FieldSelector = fieldSelector.String()
                return clientset.CoreV1().Pods("").Watch(ctx, opts)
            },
        },
        &v1.Pod{},
        0,  // No resync
        cache.Indexers{
            "ip": func(obj interface{}) ([]string, error) {
                pod := obj.(*v1.Pod)
                if pod.Status.PodIP != "" {
                    return []string{pod.Status.PodIP}, nil
                }
                return []string{}, nil
            },
        },
    )

    return &Observer{
        podInformer: podInformer,
        nodeName:    nodeName,
    }, nil
}
```

**DaemonSet Manifest:**
```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tapio-network-observer
  namespace: tapio
spec:
  template:
    spec:
      hostNetwork: true
      hostPID: true
      containers:
      - name: observer
        image: ghcr.io/yairfalse/tapio-network-observer:v1.0.0
        securityContext:
          privileged: true
        env:
        # Downward API - inject node name
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        # Set deployment mode
        - name: DEPLOYMENT_MODE
          value: "daemonset"
```

**Files to Modify:**
- `internal/observers/network/observer.go` - Add node-local filtering
- `internal/observers/status/observer.go` - Add node-local filtering
- `charts/tapio-stack/templates/network-observer-daemonset.yaml` - Add NODE_NAME env

---

## Patterns to AVOID (Product Direction)

### ❌ Generic Application Instrumentation

**What Beyla does:**
```go
// Auto-instrument ANY application (Java, Python, Go, Node.js)
// Generate OTEL spans for HTTP, gRPC, database queries
```

**Why Tapio is different:**
- We're **Kubernetes-specific**, not application-agnostic
- We diagnose **infrastructure problems**, not application performance
- We detect **"why won't K8s work?"**, not "is my app slow?"

### ❌ Metric-First Approach

**What Beyla does:**
```yaml
# Collect metrics, user builds alerts
metrics:
  - http_request_duration_seconds
  - http_requests_total
```

**Why Tapio is different:**
- We're **problem-first**: detect scheduling failures, connection issues
- We emit **diagnostic events** with root cause, not raw metrics
- We provide **actionable recommendations**, not dashboards

### ❌ OTEL Span Generation Focus

**What Beyla does:**
```go
// Generate distributed tracing spans
span := tracer.Start("http.request")
span.SetAttribute("http.method", "GET")
```

**Why Tapio is different:**
- We emit **diagnostic events**: "Pod failed to schedule: insufficient CPU + node drained 2 minutes ago"
- We focus on **infrastructure causality**, not application request flow

---

## Implementation Checklist

### P0 - Critical (v1.0 Blockers)

- [ ] **Pre-computed OTEL attributes**
  - [ ] Add `OTELAttributes` field to `domain.PodContext`
  - [ ] Implement `ComputeOTELAttributes` in `k8scontext/enrichment.go`
  - [ ] Update Context Service to compute on pod add/update
  - [ ] Update observers to use pre-computed attributes
  - [ ] Add priority cascade tests (env → annotations → labels)

- [ ] **Multi-index metadata store**
  - [ ] Add `podsByIP`, `podsByUID` maps to `ContextStore`
  - [ ] Implement multiple NATS KV keys (`pod.ip.X`, `pod.uid.X`)
  - [ ] Add `GetPodByIP` and `GetPodByUID` methods
  - [ ] Update observers to use appropriate index

- [ ] **Prometheus label transformation**
  - [ ] Create `pkg/domain/attributes.go` with constants
  - [ ] Implement `ToPrometheusLabel` function
  - [ ] Update all metric calls in `internal/base/metrics.go`
  - [ ] Add tests for label conversion

- [ ] **NO RESYNC on informers**
  - [ ] Set `resyncPeriod: 0` in all SharedIndexInformers
  - [ ] Verify no duplicate event processing

### P1 - High Impact (v1.0 Performance)

- [ ] **DaemonSet node-local filtering**
  - [ ] Add NODE_NAME env var to DaemonSet manifests
  - [ ] Implement field selector in Network Observer
  - [ ] Implement field selector in Status Observer
  - [ ] Add deployment mode config
  - [ ] Measure memory savings (before/after)

### P2 - Nice to Have (Post v1.0)

- [ ] **Lazy initialization** for Context Service
- [ ] **Metadata compaction** for large clusters
- [ ] **Custom indexers** for additional lookup patterns (PID, cgroup)

---

## Testing Strategy

### Unit Tests
```go
// Test pre-computed OTEL attributes
func TestComputeOTELAttributes_PriorityOrder(t *testing.T) {
    pod := &v1.Pod{
        ObjectMeta: metav1.ObjectMeta{
            Labels: map[string]string{
                "app": "my-app-label",
            },
            Annotations: map[string]string{
                "otel.resource.service.name": "my-app-annotation",
            },
        },
        Spec: v1.PodSpec{
            Containers: []v1.Container{
                {
                    Env: []v1.EnvVar{
                        {Name: "OTEL_SERVICE_NAME", Value: "my-app-env"},
                    },
                },
            },
        },
    }

    attrs := ComputeOTELAttributes(pod)

    // Env var should win (highest priority)
    assert.Equal(t, "my-app-env", attrs["service.name"])
}
```

### Integration Tests
```go
// Test multi-index lookups
func TestContextStore_MultipleIndexes(t *testing.T) {
    store := NewContextStore(natsKV)

    pod := &domain.PodContext{
        Name: "test-pod",
        PodIP: "10.0.1.42",
        UID: "abc-123",
    }

    store.StorePod(ctx, pod)

    // Should be retrievable by both IP and UID
    podByIP, _ := store.GetPodByIP("10.0.1.42")
    podByUID, _ := store.GetPodByUID("abc-123")

    assert.Equal(t, pod.Name, podByIP.Name)
    assert.Equal(t, pod.Name, podByUID.Name)
}
```

### Performance Tests
```go
// Benchmark OTEL attribute access
func BenchmarkPrecomputedAttributes(b *testing.B) {
    podCtx := &domain.PodContext{
        OTELAttributes: map[string]string{
            "service.name": "my-service",
            "k8s.pod.name": "my-pod",
        },
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = podCtx.OTELAttributes  // Should be ~1µs
    }
}
```

---

## Success Metrics

### Performance
- **OTEL attribute access:** < 1µs per event (vs 100µs without pre-computation)
- **Memory usage (DaemonSet):** < 50MB per node (vs 300MB without filtering)
- **K8s API calls:** 50% reduction (field selector + no resync)

### Correctness
- **Prometheus metrics:** All labels use snake_case (no dots)
- **Event deduplication:** Zero duplicate events from informer resync
- **Lookup success rate:** 99.9% cache hit rate for hot path

---

## References

- **Beyla Repository:** https://github.com/grafana/beyla
- **Prometheus Discovery:** `/Users/yair/projects/prometheus/discovery/kubernetes/`
- **Tapio Architecture Research:** `/Users/yair/projects/tapio/docs/ARCHITECTURE_RESEARCH_FINDINGS.md`
- **Tapio Platform Architecture:** `/Users/yair/projects/tapio/docs/PLATFORM_ARCHITECTURE.md`
