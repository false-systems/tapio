# Design Doc 005: WASM Observer Implementation Guide

**Status**: Ready for Implementation (Post-v1.0)
**Date**: 2025-01-25
**Authors**: Yair + Claude (AI pair programming)
**Context**: Implementation blueprint for WASM observer
**Related**: Doc 004 (WASM Research), ADR 002 (Observer Consolidation)
**Prerequisites**: Tapio v1.0 shipped (network + scheduler observers + Context Service)

---

## Executive Summary

This document provides a **complete implementation blueprint** for the Tapio WASM observer, designed to be built as a **v1.1 feature** after Tapio v1.0 ships. Estimated effort: **4 weeks**. Target: SpinKube users running WASM workloads on Kubernetes.

**Core Value Proposition**: "See which WASM apps are running, track density (250 apps/node), detect resource anomalies, and correlate WASM failures with K8s infrastructure."

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Phase 1: SpinApp Detection (Week 1)](#phase-1-spinapp-detection)
3. [Phase 2: Density Tracking (Week 2)](#phase-2-density-tracking)
4. [Phase 3: Resource Anomaly Detection (Week 3)](#phase-3-resource-anomaly-detection)
5. [Phase 4: Integration & Polish (Week 4)](#phase-4-integration--polish)
6. [Testing Strategy](#testing-strategy)
7. [Deployment](#deployment)

---

## 1. Architecture Overview

### 1.1 Observer Design Pattern

Following CLAUDE.md standards and existing Tapio architecture:

```go
// internal/observers/wasm/observer.go
package wasm

type WasmObserver struct {
    *base.BaseObserver
    *base.EventChannelManager
    *base.LifecycleManager

    // K8s clients
    k8sClient     kubernetes.Interface
    dynamicClient dynamic.Interface

    // Context Service for enrichment
    contextClient pb.ContextServiceClient

    // Informers
    podInformer     cache.SharedIndexInformer
    spinAppInformer cache.SharedIndexInformer
    nodeInformer    cache.SharedIndexInformer

    // Metrics scraper (optional)
    metricsClient *prometheus.Client

    // Known WASM runtimeClasses
    wasmRuntimes []string

    // OTEL instrumentation
    tracer              trace.Tracer
    spinAppsGauge       metric.Int64Gauge
    densityGauge        metric.Float64Gauge
    anomaliesCounter    metric.Int64Counter
}
```

---

### 1.2 Detection Strategy

**Dual approach** - Watch both Pod-based and SpinApp-based WASM:

```
┌─────────────────────────────────────────┐
│   WASM Workload Detection               │
├─────────────────────────────────────────┤
│                                         │
│  Path 1: Pod-based WASM                │
│  ├── Watch: Pods                        │
│  ├── Filter: runtimeClassName matches   │
│  │   (wasmtime-*, wasmedge-*, spin-*)   │
│  └── Emit: wasm.component_started       │
│                                         │
│  Path 2: SpinApp-based WASM (SpinKube) │
│  ├── Watch: SpinApp CRDs                │
│  ├── Detect: spec.executor = spin      │
│  └── Emit: wasm.spinapp_deployed        │
│                                         │
└─────────────────────────────────────────┘
```

---

### 1.3 Data Flow

```
┌──────────────┐
│   SpinApp    │
│   Created    │
└──────┬───────┘
       │
       ▼
┌──────────────────────────────┐
│  WASM Observer               │
│  ├── SpinApp informer        │
│  │   └── handleSpinAppAdded  │
│  ├── Context Service lookup  │
│  │   └── getK8sContext       │
│  └── Emit domain event       │
└──────────────┬───────────────┘
               │
               ▼
┌──────────────────────────────┐
│  domain.ObserverEvent        │
│  Type: "wasm"                │
│  Subtype: "spinapp_deployed" │
│  WasmData: {...}             │
└──────────────┬───────────────┘
               │
               ▼
┌──────────────────────────────┐
│  Output Channels              │
│  ├── NATS (future)           │
│  ├── OTLP                    │
│  └── Stdout (dev)            │
└──────────────────────────────┘
```

---

## 2. Phase 1: SpinApp Detection (Week 1)

### 2.1 Goal

Detect and emit events for SpinApp lifecycle (create, scale, delete)

---

### 2.2 Implementation (TDD - RED → GREEN → REFACTOR)

#### Step 1: RED - Write Failing Tests

```go
// internal/observers/wasm/observer_test.go
package wasm

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestWasmObserver_DetectSpinApp(t *testing.T) {
    // RED: This will fail - observer doesn't exist yet
    observer, err := NewWasmObserver("wasm", Config{
        RuntimeClasses: []string{"wasmtime-spin-v2"},
    })
    require.NoError(t, err)
    require.NotNil(t, observer)

    // Mock SpinApp
    spinApp := &unstructured.Unstructured{
        Object: map[string]interface{}{
            "apiVersion": "core.spinoperator.dev/v1alpha1",
            "kind":       "SpinApp",
            "metadata": map[string]interface{}{
                "name":      "test-app",
                "namespace": "default",
            },
            "spec": map[string]interface{}{
                "image":    "ghcr.io/test/app:v1",
                "executor": "containerd-shim-spin",
                "replicas": int64(10),
            },
        },
    }

    ctx := context.Background()
    event := observer.processSpinApp(ctx, spinApp, "created")

    require.NotNil(t, event)
    assert.Equal(t, "wasm", event.Type)
    assert.Equal(t, "spinapp_deployed", event.Subtype)
    assert.Equal(t, "test-app", event.WasmData.SpinAppName)
    assert.Equal(t, int64(10), event.WasmData.Replicas)
}

// Run: go test ./internal/observers/wasm/...
// Expected: FAIL (functions don't exist) ✅ RED phase
```

---

#### Step 2: GREEN - Minimal Implementation

```go
// internal/observers/wasm/observer.go
package wasm

import (
    "context"
    "fmt"
    "strings"
    "time"

    "github.com/yairfalse/tapio/pkg/domain"
    "github.com/yairfalse/tapio/internal/observers/base"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/client-go/dynamic"
    "k8s.io/client-go/kubernetes"
)

type Config struct {
    RuntimeClasses []string // wasmtime-spin-v2, wasmedge, etc.
}

type WasmObserver struct {
    *base.BaseObserver
    *base.EventChannelManager
    *base.LifecycleManager

    k8sClient     kubernetes.Interface
    dynamicClient dynamic.Interface

    wasmRuntimes []string
}

func NewWasmObserver(name string, cfg Config) (*WasmObserver, error) {
    obs := &WasmObserver{
        BaseObserver: base.NewBaseObserver(name, "wasm"),
        wasmRuntimes: cfg.RuntimeClasses,
    }

    // Initialize lifecycle manager
    obs.LifecycleManager = base.NewLifecycleManager(
        obs.startInternal,
        obs.stopInternal,
    )

    return obs, nil
}

func (o *WasmObserver) startInternal(ctx context.Context) error {
    // Initialize dynamic client for SpinApp CRDs
    spinAppGVR := schema.GroupVersionResource{
        Group:    "core.spinoperator.dev",
        Version:  "v1alpha1",
        Resource: "spinapps",
    }

    // Create informer for SpinApps
    spinAppInformer := o.dynamicClient.Resource(spinAppGVR).Informer()

    // Watch SpinApp events
    spinAppInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        AddFunc: func(obj interface{}) {
            spinApp := obj.(*unstructured.Unstructured)
            o.handleSpinAppAdded(ctx, spinApp)
        },
        UpdateFunc: func(oldObj, newObj interface{}) {
            spinApp := newObj.(*unstructured.Unstructured)
            o.handleSpinAppUpdated(ctx, spinApp)
        },
        DeleteFunc: func(obj interface{}) {
            spinApp := obj.(*unstructured.Unstructured)
            o.handleSpinAppDeleted(ctx, spinApp)
        },
    })

    // Start informer
    go spinAppInformer.Run(ctx.Done())

    return nil
}

func (o *WasmObserver) stopInternal() error {
    return nil
}

func (o *WasmObserver) handleSpinAppAdded(ctx context.Context, spinApp *unstructured.Unstructured) {
    event := o.processSpinApp(ctx, spinApp, "created")
    if event != nil {
        o.EmitEvent(ctx, event)
        o.RecordEvent(ctx) // OTEL metric
    }
}

func (o *WasmObserver) processSpinApp(ctx context.Context, spinApp *unstructured.Unstructured, action string) *domain.ObserverEvent {
    spec := spinApp.Object["spec"].(map[string]interface{})

    // Extract SpinApp fields
    image := spec["image"].(string)
    executor := spec["executor"].(string)
    replicas := spec["replicas"].(int64)

    // Create domain event
    return &domain.ObserverEvent{
        Type:    string(domain.EventTypeWasm),
        Subtype: "spinapp_deployed",

        WasmData: &domain.WasmEventData{
            Runtime:      "spin",
            SpinAppName:  spinApp.GetName(),
            Namespace:    spinApp.GetNamespace(),
            Executor:     executor,
            Replicas:     replicas,

            // K8s context (from Context Service - future)
            Labels: spinApp.GetLabels(),
        },

        Timestamp: time.Now(),
    }
}

// Run: go test ./internal/observers/wasm/...
// Expected: PASS ✅ GREEN phase
```

---

#### Step 3: REFACTOR - Add Edge Cases

```go
// Add test for missing fields
func TestWasmObserver_SpinAppMissingFields(t *testing.T) {
    observer, _ := NewWasmObserver("wasm", Config{})

    spinApp := &unstructured.Unstructured{
        Object: map[string]interface{}{
            "metadata": map[string]interface{}{
                "name": "incomplete-app",
            },
            "spec": map[string]interface{}{
                // Missing required fields
            },
        },
    }

    event := observer.processSpinApp(context.Background(), spinApp, "created")
    // Should handle gracefully, not panic
    assert.Nil(t, event) // Or emit error event
}

// Refactor processSpinApp with validation
func (o *WasmObserver) processSpinApp(ctx context.Context, spinApp *unstructured.Unstructured, action string) *domain.ObserverEvent {
    spec, ok := spinApp.Object["spec"].(map[string]interface{})
    if !ok {
        o.logger.Warn("SpinApp missing spec field",
            "name", spinApp.GetName(),
            "namespace", spinApp.GetNamespace(),
        )
        return nil
    }

    // Validate required fields
    image, _ := spec["image"].(string)
    executor, _ := spec["executor"].(string)
    replicas, _ := spec["replicas"].(int64)

    if image == "" || executor == "" {
        o.logger.Warn("SpinApp missing required fields",
            "name", spinApp.GetName(),
            "image", image,
            "executor", executor,
        )
        return nil
    }

    return &domain.ObserverEvent{
        Type:    string(domain.EventTypeWasm),
        Subtype: "spinapp_deployed",
        WasmData: &domain.WasmEventData{
            Runtime:     "spin",
            SpinAppName: spinApp.GetName(),
            Namespace:   spinApp.GetNamespace(),
            Executor:    executor,
            Replicas:    replicas,
            Labels:      spinApp.GetLabels(),
        },
        Timestamp: time.Now(),
    }
}

// Run: go test ./internal/observers/wasm/...
// Expected: PASS ✅ REFACTOR complete
```

---

### 2.3 Commit

```bash
git add internal/observers/wasm/
git commit -m "feat(wasm): add SpinApp detection with validation

- Watch SpinApp CRDs via dynamic client
- Emit spinapp_deployed events
- Validate required fields (image, executor)
- Test coverage: 85%

Closes #XXX"
```

---

## 3. Phase 2: Density Tracking (Week 2)

### 3.1 Goal

Track WASM pod density per node, alert when >200 apps/node

---

### 3.2 Implementation

#### TDD - RED Phase

```go
// internal/observers/wasm/density_test.go
func TestWasmObserver_DetectHighDensity(t *testing.T) {
    observer, _ := NewWasmObserver("wasm", Config{})

    // Mock node with 250 WASM pods
    nodeName := "node-1"
    wasmPods := 250

    event := observer.checkNodeDensity(context.Background(), nodeName, wasmPods)

    require.NotNil(t, event)
    assert.Equal(t, "wasm", event.Type)
    assert.Equal(t, "high_density", event.Subtype)
    assert.Equal(t, 250, event.WasmData.AppsOnNode)
    assert.Greater(t, event.WasmData.NodeDensity, 0.8) // >80% of max
}
```

---

#### GREEN Phase - Implementation

```go
// internal/observers/wasm/density.go
package wasm

const MaxWasmDensity = 250 // ZEISS benchmark

type DensityTracker struct {
    // Node → WASM pod count
    nodePodCount map[string]int
    mu           sync.RWMutex
}

func (o *WasmObserver) trackPodDensity(ctx context.Context, pod *v1.Pod) {
    if !o.isWasmPod(pod) {
        return
    }

    nodeName := pod.Spec.NodeName
    if nodeName == "" {
        return // Pod not scheduled yet
    }

    o.density.mu.Lock()
    o.density.nodePodCount[nodeName]++
    count := o.density.nodePodCount[nodeName]
    o.density.mu.Unlock()

    // Check if density is high
    if count > 200 { // 80% of max
        o.emitHighDensityEvent(ctx, nodeName, count)
    }

    // Update OTEL gauge
    o.densityGauge.Record(ctx, float64(count)/MaxWasmDensity,
        metric.WithAttributes(
            attribute.String("node", nodeName),
        ),
    )
}

func (o *WasmObserver) emitHighDensityEvent(ctx context.Context, nodeName string, count int) {
    event := &domain.ObserverEvent{
        Type:    string(domain.EventTypeWasm),
        Subtype: "high_density",

        WasmData: &domain.WasmEventData{
            NodeName:    nodeName,
            AppsOnNode:  count,
            NodeDensity: float64(count) / MaxWasmDensity,
        },

        Timestamp: time.Now(),
    }

    o.EmitEvent(ctx, event)
}

func (o *WasmObserver) isWasmPod(pod *v1.Pod) bool {
    if pod.Spec.RuntimeClassName == nil {
        return false
    }

    rc := *pod.Spec.RuntimeClassName
    for _, wasmRT := range o.wasmRuntimes {
        if strings.Contains(rc, wasmRT) {
            return true
        }
    }
    return false
}
```

---

### 2.3 Commit

```bash
git add internal/observers/wasm/density.go
git commit -m "feat(wasm): add node density tracking

- Track WASM pods per node
- Alert when >200 apps/node (80% of max)
- OTEL gauge for density metrics
- Test coverage: 82%"
```

---

## 4. Phase 3: Resource Anomaly Detection (Week 3)

### 4.1 Goal

Detect WASM pods using >90% of memory limit

---

### 4.2 Implementation

```go
// internal/observers/wasm/anomaly.go
package wasm

import (
    metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
)

type AnomalyDetector struct {
    metricsClient metricsv1beta1.MetricsV1beta1Interface
}

func (o *WasmObserver) detectResourceAnomalies(ctx context.Context, pod *v1.Pod) {
    // Get actual resource usage from metrics-server
    podMetrics, err := o.anomaly.metricsClient.PodMetricses(pod.Namespace).Get(
        ctx,
        pod.Name,
        metav1.GetOptions{},
    )
    if err != nil {
        o.logger.Debug("Failed to get pod metrics", "error", err)
        return
    }

    // Get memory limit from pod spec
    memoryLimit := getMemoryLimit(pod)
    if memoryLimit == 0 {
        return // No limit set
    }

    // Calculate usage from metrics
    memoryUsage := podMetrics.Containers[0].Usage.Memory().Value()
    memoryPercent := float64(memoryUsage) / float64(memoryLimit)

    // Alert if >90% usage
    if memoryPercent > 0.9 {
        o.emitResourceAnomalyEvent(ctx, pod, memoryUsage, memoryLimit, memoryPercent)
    }
}

func (o *WasmObserver) emitResourceAnomalyEvent(ctx context.Context, pod *v1.Pod, usage, limit int64, percent float64) {
    event := &domain.ObserverEvent{
        Type:    string(domain.EventTypeWasm),
        Subtype: "resource_anomaly",

        WasmData: &domain.WasmEventData{
            PodName:       pod.Name,
            Namespace:     pod.Namespace,
            NodeName:      pod.Spec.NodeName,
            MemoryUsage:   fmt.Sprintf("%dMi", usage/(1024*1024)),
            MemoryLimit:   fmt.Sprintf("%dMi", limit/(1024*1024)),
            MemoryPercent: percent * 100,
        },

        Timestamp: time.Now(),
    }

    o.EmitEvent(ctx, event)
    o.anomaliesCounter.Add(ctx, 1)
}

func getMemoryLimit(pod *v1.Pod) int64 {
    for _, container := range pod.Spec.Containers {
        if limit, ok := container.Resources.Limits[v1.ResourceMemory]; ok {
            return limit.Value()
        }
    }
    return 0
}
```

---

## 5. Phase 4: Integration & Polish (Week 4)

### 5.1 Context Service Integration

```go
// Enrich events with K8s context
func (o *WasmObserver) enrichWithK8sContext(ctx context.Context, event *domain.ObserverEvent) {
    // Fast gRPC call to Context Service
    ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
    defer cancel()

    podCtx, err := o.contextClient.GetPodByName(ctx, &pb.PodRequest{
        Name:      event.WasmData.PodName,
        Namespace: event.WasmData.Namespace,
    })
    if err != nil {
        o.logger.Warn("Failed to enrich K8s context", "error", err)
        return // Degrade gracefully
    }

    // Add pre-computed OTEL attributes (Beyla pattern)
    event.WasmData.DeploymentName = podCtx.OwnerName
    event.WasmData.Labels = podCtx.Labels
    event.Attributes = podCtx.OtelAttributes
}
```

---

### 5.2 Spin Metrics Scraping (Optional)

```go
// Scrape Spin Prometheus endpoint (if available)
func (o *WasmObserver) scrapeSpinMetrics(ctx context.Context, pod *v1.Pod) {
    // Check if pod has Spin metrics port annotation
    metricsPort, ok := pod.Annotations["spin.dev/metrics-port"]
    if !ok {
        return // No metrics endpoint
    }

    podIP := pod.Status.PodIP
    if podIP == "" {
        return
    }

    // Scrape Prometheus endpoint
    url := fmt.Sprintf("http://%s:%s/metrics", podIP, metricsPort)
    metrics, err := o.metricsClient.Scrape(ctx, url)
    if err != nil {
        o.logger.Debug("Failed to scrape Spin metrics", "error", err)
        return
    }

    // Emit metrics event
    event := &domain.ObserverEvent{
        Type:    string(domain.EventTypeWasm),
        Subtype: "metrics_collected",

        WasmData: &domain.WasmEventData{
            PodName:   pod.Name,
            Namespace: pod.Namespace,
            Metrics:   metrics,
        },

        Timestamp: time.Now(),
    }

    o.EmitEvent(ctx, event)
}
```

---

### 5.3 Helm Chart Integration

```yaml
# charts/tapio-stack/values.yaml
observers:
  wasm:
    enabled: false  # Opt-in

    # Known WASM runtimeClasses
    runtimeClasses:
      - wasmtime-spin-v2
      - wasmtime
      - wasmedge
      - spin
      - lunatic

    # Density threshold
    maxDensity: 250
    densityWarningThreshold: 200  # 80%

    # Resource anomaly detection
    memoryThreshold: 0.9  # Alert at 90% usage

    # Metrics scraping
    scrapeSpinMetrics: true
    scrapeInterval: 30s
```

---

## 6. Testing Strategy

### 6.1 Unit Tests

```bash
# Run unit tests
go test ./internal/observers/wasm/... -v -cover

# Coverage requirement
go test ./internal/observers/wasm/... -coverprofile=coverage.out
go tool cover -func=coverage.out | grep total
# Must be >80%
```

**Test files**:
- `observer_test.go` - Core observer lifecycle
- `spinapp_test.go` - SpinApp detection
- `density_test.go` - Density tracking
- `anomaly_test.go` - Resource anomaly detection

---

### 6.2 Integration Tests

```go
// internal/observers/wasm/integration_test.go
// +build integration

func TestWasmObserver_SpinKubeIntegration(t *testing.T) {
    // Requires real K8s cluster with SpinKube installed
    if testing.Short() {
        t.Skip("Skipping integration test")
    }

    // Deploy SpinApp
    spinApp := createTestSpinApp(t)
    defer deleteTestSpinApp(t, spinApp)

    // Start observer
    observer := setupWasmObserver(t)
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    err := observer.Start(ctx)
    require.NoError(t, err)

    // Wait for SpinApp deployment event
    event := waitForEvent(t, observer, "spinapp_deployed", 10*time.Second)
    assert.NotNil(t, event)
    assert.Equal(t, spinApp.Name, event.WasmData.SpinAppName)
}
```

---

### 6.3 E2E Tests (Local SpinKube Cluster)

```bash
# Create kind cluster with SpinKube
kind create cluster --name tapio-wasm-test

# Install SpinKube
helm repo add spinkube https://spinkube.github.io/charts
helm install spinkube spinkube/spinkube

# Deploy Tapio with WASM observer
helm install tapio ./charts/tapio-stack \
  --set observers.wasm.enabled=true

# Deploy test SpinApp
kubectl apply -f examples/spinapp-test.yaml

# Verify events
kubectl logs -l app=tapio-wasm-observer -f
# Should see: spinapp_deployed event
```

---

## 7. Deployment

### 7.1 Helm Installation

```bash
# Enable WASM observer in existing Tapio installation
helm upgrade tapio tapio/tapio-stack \
  --set observers.wasm.enabled=true
```

---

### 7.2 Prerequisites

**Required**:
- Tapio v1.0 installed (Context Service + Platform)
- Kubernetes 1.24+ (RuntimeClass support)

**Optional** (for full functionality):
- SpinKube installed (for SpinApp CRDs)
- metrics-server (for resource usage)
- Kwasm operator (for node labeling)

---

### 7.3 Graceful Degradation

**If SpinKube not installed**:
- Observer still works for Pod-based WASM (runtimeClassName detection)
- SpinApp informer fails gracefully

**If metrics-server not installed**:
- Resource anomaly detection disabled
- Logs warning message

**If Context Service unavailable**:
- Events emitted without K8s enrichment (pod name, namespace only)
- No deployment/label context

---

## 8. Documentation

### 8.1 User Guide

Create `docs/guides/wasm-observer.md`:

```markdown
# WASM Observer Guide

## Overview

The WASM observer tracks WebAssembly workloads running on Kubernetes, including SpinKube applications.

## Features

- SpinApp lifecycle tracking (create, scale, delete)
- Node density monitoring (alert at 200+ apps/node)
- Resource anomaly detection (>90% memory usage)
- Spin metrics scraping (optional)

## Installation

Enable in Helm values:

\`\`\`yaml
observers:
  wasm:
    enabled: true
\`\`\`

## Events Emitted

- `wasm.spinapp_deployed` - SpinApp created
- `wasm.spinapp_scaled` - Replicas changed
- `wasm.high_density` - Node has >200 WASM pods
- `wasm.resource_anomaly` - Pod using >90% memory
- `wasm.component_oom` - WASM component OOM killed

## Troubleshooting

### No events appearing

Check if SpinKube is installed:
\`\`\`bash
kubectl get crd spinapps.core.spinoperator.dev
\`\`\`

Check observer logs:
\`\`\`bash
kubectl logs -l app=tapio-wasm-observer
\`\`\`
```

---

### 8.2 Blog Post (Launch Announcement)

Draft `docs/blog/tapio-wasm-observer-launch.md`:

```markdown
# Announcing Tapio WASM Observer: First Observability for WebAssembly on Kubernetes

Today we're excited to announce the Tapio WASM Observer - the **first infrastructure observability tool** built specifically for WebAssembly workloads on Kubernetes.

## The Problem

Companies like ZEISS are achieving 60% cost reduction by running 250 WASM apps per node with SpinKube. But when failures happen, debugging is impossible:

- "Which WASM apps are running on which nodes?"
- "Why did my SpinApp get OOM killed?"
- "Is my node overloaded with WASM apps?"

**No observability tool could answer these questions** - until now.

## The Solution

Tapio WASM Observer provides infrastructure-level visibility:

- **SpinApp lifecycle tracking** - See deployments, scaling, deletions
- **Density monitoring** - Alert when approaching 250 apps/node
- **Resource anomalies** - Detect memory/CPU issues before failure
- **K8s correlation** - Connect WASM failures to infrastructure events

## Getting Started

\`\`\`bash
helm repo add tapio https://charts.tapio.io
helm install tapio tapio/tapio-stack --set observers.wasm.enabled=true
\`\`\`

## Works With Dylibso

Tapio complements Dylibso Observe:
- **Dylibso**: Application-level tracing (what your WASM code does)
- **Tapio**: Infrastructure observability (why your WASM infrastructure fails)

Together: Complete WASM observability.

## Learn More

- Docs: https://docs.tapio.io/wasm-observer
- GitHub: https://github.com/yairfalse/tapio
- Join us: #tapio on CNCF Slack
```

---

## 9. Success Criteria

### 9.1 Technical

- [ ] SpinApp lifecycle events emitted
- [ ] Density alerts trigger at 200 apps/node
- [ ] Resource anomalies detected (>90% usage)
- [ ] K8s context enrichment via Context Service
- [ ] Spin metrics scraping works
- [ ] Test coverage >80%
- [ ] No `map[string]interface{}` violations
- [ ] All commits <30 lines
- [ ] `make verify-full` passes

---

### 9.2 Business

- [ ] 10+ SpinKube users by EOY 2025
- [ ] ZEISS partnership/case study
- [ ] KubeCon talk acceptance
- [ ] CNCF blog post published
- [ ] Fermyon integration partnership
- [ ] Listed on SpinKube.dev ecosystem page

---

## 10. Future Enhancements (v1.2+)

### 10.1 Component Model Support

When Component Model matures:
- Detect inter-component calls
- Component-level resource attribution
- Component call graph visualization

---

### 10.2 OCI Image Parsing

Parse spin.toml from OCI images:
- Extract component definitions
- Map HTTP routes to components
- Component-level metrics

---

### 10.3 Advanced Correlation

Cross-observer correlation:
- WASM OOM → K8s scheduler pressure → Node eviction
- Network timeout → DNS failure → WASM API call failure

---

## Appendix: File Structure

```
internal/observers/wasm/
├── observer.go                 # Core observer
├── observer_test.go            # Unit tests
├── spinapp.go                  # SpinApp detection
├── spinapp_test.go
├── density.go                  # Density tracking
├── density_test.go
├── anomaly.go                  # Resource anomalies
├── anomaly_test.go
├── metrics.go                  # Spin metrics scraping
├── metrics_test.go
├── integration_test.go         # Integration tests
└── README.md                   # Observer-specific docs
```

---

**End of Implementation Guide**

**Next Steps**:
1. Ship Tapio v1.0 (network + scheduler observers)
2. Return to this doc when ready for v1.1
3. Follow TDD workflow (RED → GREEN → REFACTOR)
4. Ship in 4 weeks ✅
