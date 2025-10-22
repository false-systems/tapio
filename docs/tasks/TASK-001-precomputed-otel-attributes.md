# TASK-001: Implement Pre-Computed OTEL Attributes

**Priority:** P0 - Critical (Blocks v1.0)
**Estimated Effort:** 4-6 hours
**Skills Required:** Go, Kubernetes client-go, OTEL

---

## Context

Currently, observers compute OTEL attributes on **every event** by parsing pod labels, annotations, and environment variables. This is slow (~100µs per event).

**Beyla's solution:** Pre-compute OTEL attributes once when a pod is added/updated, cache them in PodContext, and reuse them for all events.

**Performance impact:** 100x faster (1µs vs 100µs per event)

**Reference:** `/Users/yair/projects/tapio/docs/BEYLA_PATTERNS_IMPLEMENTATION.md` (Section 1)

---

## Objective

Implement pre-computed OTEL attributes following Beyla's pattern:
1. Add `OTELAttributes` field to `PodContext`
2. Compute attributes once during pod add/update (not per event)
3. Use priority cascade: env vars → annotations → labels (Beyla pattern)
4. Update observers to use cached attributes

---

## Implementation Steps

### Step 1: Create `pkg/domain/context.go`

**New file:** `pkg/domain/context.go`

```go
package domain

// PodContext represents cached K8s pod metadata with pre-computed OTEL attributes
type PodContext struct {
	// K8s metadata
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	PodIP     string `json:"pod_ip"`
	NodeName  string `json:"node_name"`

	// Owner references (for correlation)
	OwnerKind string `json:"owner_kind,omitempty"` // Deployment, StatefulSet, DaemonSet
	OwnerName string `json:"owner_name,omitempty"`

	// Pre-computed OTEL attributes (Beyla pattern)
	// Computed once during pod add/update, reused for all events
	OTELAttributes map[string]string `json:"otel_attributes"`
}

// DeploymentContext represents cached K8s deployment metadata
type DeploymentContext struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Replicas  int32  `json:"replicas"`

	// Pre-computed OTEL attributes
	OTELAttributes map[string]string `json:"otel_attributes"`
}

// NodeContext represents cached K8s node metadata
type NodeContext struct {
	Name string `json:"name"`
	UID  string `json:"uid"`

	// Node labels (taints, instance type, etc.)
	Labels map[string]string `json:"labels"`

	// Pre-computed OTEL attributes
	OTELAttributes map[string]string `json:"otel_attributes"`
}
```

**Acceptance Criteria:**
- ✅ File created at `pkg/domain/context.go`
- ✅ `PodContext` has `OTELAttributes map[string]string` field
- ✅ Follows existing domain package conventions (no external dependencies)

---

### Step 2: Create `internal/services/k8scontext/enrichment.go`

**New file:** `internal/services/k8scontext/enrichment.go`

```go
package k8scontext

import (
	"strings"

	v1 "k8s.io/api/core/v1"
)

// ComputeOTELAttributes computes OTEL resource attributes for a pod
// Following Beyla's priority cascade: env vars → annotations → labels
//
// Priority cascade (highest to lowest):
// 1. Container environment variables (OTEL_SERVICE_NAME, OTEL_SERVICE_NAMESPACE)
// 2. Pod annotations (otel.resource.*)
// 3. Pod labels (app.kubernetes.io/name, app, etc.)
//
// Reference: vendor/go.opentelemetry.io/obi/pkg/components/kube/store.go:cacheResourceMetadata
func ComputeOTELAttributes(pod *v1.Pod) map[string]string {
	attrs := make(map[string]string)

	// Priority 1: Container environment variables (highest priority)
	// Check first container for OTEL_* env vars
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			switch env.Name {
			case "OTEL_SERVICE_NAME":
				if env.Value != "" {
					attrs["service.name"] = env.Value
				}
			case "OTEL_SERVICE_NAMESPACE":
				if env.Value != "" {
					attrs["service.namespace"] = env.Value
				}
			}
		}
		// Break after first container (convention: first container is main app)
		if attrs["service.name"] != "" {
			break
		}
	}

	// Priority 2: Pod annotations (override labels, but not env vars)
	// Convention: otel.resource.<attribute_name> = <value>
	for k, v := range pod.Annotations {
		if strings.HasPrefix(k, "otel.resource.") {
			attrName := strings.TrimPrefix(k, "otel.resource.")
			// Only set if not already set by env var
			if _, exists := attrs[attrName]; !exists {
				attrs[attrName] = v
			}
		}
	}

	// Priority 3: Pod labels (lowest priority)
	// Standard Kubernetes labels
	if attrs["service.name"] == "" {
		// Try standard K8s labels in order
		if app := pod.Labels["app.kubernetes.io/name"]; app != "" {
			attrs["service.name"] = app
		} else if app := pod.Labels["app"]; app != "" {
			attrs["service.name"] = app
		}
	}

	if attrs["service.namespace"] == "" {
		if partOf := pod.Labels["app.kubernetes.io/part-of"]; partOf != "" {
			attrs["service.namespace"] = partOf
		}
	}

	// Standard K8s attributes (always included)
	attrs["k8s.pod.name"] = pod.Name
	attrs["k8s.namespace.name"] = pod.Namespace
	attrs["k8s.pod.uid"] = string(pod.UID)
	attrs["k8s.node.name"] = pod.Spec.NodeName

	// Add owner reference if available
	if len(pod.OwnerReferences) > 0 {
		owner := pod.OwnerReferences[0]
		attrs["k8s."+strings.ToLower(owner.Kind)+".name"] = owner.Name
		// Examples:
		// - k8s.deployment.name
		// - k8s.statefulset.name
		// - k8s.daemonset.name
	}

	return attrs
}

// ComputeOTELAttributesForDeployment computes OTEL attributes for a deployment
func ComputeOTELAttributesForDeployment(deployment interface{}) map[string]string {
	// TODO: Implement when needed (lower priority than pods)
	return make(map[string]string)
}

// ComputeOTELAttributesForNode computes OTEL attributes for a node
func ComputeOTELAttributesForNode(node interface{}) map[string]string {
	// TODO: Implement when needed (lower priority than pods)
	return make(map[string]string)
}
```

**Acceptance Criteria:**
- ✅ File created at `internal/services/k8scontext/enrichment.go`
- ✅ `ComputeOTELAttributes(pod)` implements priority cascade
- ✅ Priority order: env vars (highest) → annotations → labels (lowest)
- ✅ Returns map with standard K8s attributes + service.name if available
- ✅ Code comments explain Beyla pattern reference

---

### Step 3: Update Context Service to Pre-Compute Attributes

**File to modify:** `internal/services/k8scontext/service.go`

**Find the pod informer event handlers** (likely `handlePodAdd`, `handlePodUpdate`) and update them to call `ComputeOTELAttributes`:

```go
// Example - adapt to actual code structure
func (s *Service) handlePodAdd(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	// Create PodContext with pre-computed OTEL attributes (Beyla pattern)
	podCtx := &domain.PodContext{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		UID:       string(pod.UID),
		PodIP:     pod.Status.PodIP,
		NodeName:  pod.Spec.NodeName,

		// Extract owner reference
		OwnerKind: getOwnerKind(pod),
		OwnerName: getOwnerName(pod),

		// Pre-compute OTEL attributes ONCE (not per event!)
		OTELAttributes: ComputeOTELAttributes(pod),
	}

	// Store in Context Service cache
	if err := s.store.StorePod(context.Background(), podCtx); err != nil {
		s.logger.Error("failed to store pod context", "error", err)
	}
}

func (s *Service) handlePodUpdate(oldObj, newObj interface{}) {
	// Similar to handlePodAdd
	// Re-compute OTEL attributes if pod spec/labels/annotations changed
	// (Most updates don't change these, so we could optimize later)
	pod, ok := newObj.(*v1.Pod)
	if !ok {
		return
	}

	podCtx := &domain.PodContext{
		Name:           pod.Name,
		Namespace:      pod.Namespace,
		UID:            string(pod.UID),
		PodIP:          pod.Status.PodIP,
		NodeName:       pod.Spec.NodeName,
		OwnerKind:      getOwnerKind(pod),
		OwnerName:      getOwnerName(pod),
		OTELAttributes: ComputeOTELAttributes(pod),  // Re-compute on update
	}

	if err := s.store.StorePod(context.Background(), podCtx); err != nil {
		s.logger.Error("failed to update pod context", "error", err)
	}
}

// Helper to extract owner kind
func getOwnerKind(pod *v1.Pod) string {
	if len(pod.OwnerReferences) > 0 {
		return pod.OwnerReferences[0].Kind
	}
	return ""
}

// Helper to extract owner name
func getOwnerName(pod *v1.Pod) string {
	if len(pod.OwnerReferences) > 0 {
		return pod.OwnerReferences[0].Name
	}
	return ""
}
```

**Acceptance Criteria:**
- ✅ `handlePodAdd` calls `ComputeOTELAttributes` and stores result in `PodContext`
- ✅ `handlePodUpdate` re-computes OTEL attributes on pod changes
- ✅ Owner references extracted and stored in `PodContext`
- ✅ Context Service tests updated (if they exist)

---

### Step 4: Update Network Observer to Use Pre-Computed Attributes

**File to modify:** `internal/observers/network/observer.go`

**Find where the observer enriches events** with K8s metadata and update to use `PodContext.OTELAttributes`:

**Before (slow - computing per event):**
```go
func (o *NetworkObserver) enrichEvent(bpfEvent *TCPEvent) {
	pod := o.getPodByIP(bpfEvent.SrcIP)

	// ❌ BAD: Computing on every event!
	serviceName := pod.Labels["app.kubernetes.io/name"]
	if serviceName == "" {
		serviceName = pod.Labels["app"]
	}

	event.NetworkData.PodName = pod.Name
	event.NetworkData.Namespace = pod.Namespace
	// ... more attribute parsing
}
```

**After (fast - using pre-computed):**
```go
func (o *NetworkObserver) enrichEvent(bpfEvent *TCPEvent) {
	// Get PodContext (already has pre-computed OTEL attributes)
	podCtx := o.getPodContext(bpfEvent.SrcIP)  // Returns *domain.PodContext

	// ✅ GOOD: Just use pre-computed values!
	event.NetworkData.PodName = podCtx.Name
	event.NetworkData.Namespace = podCtx.Namespace

	// For OTEL export, attributes are already computed
	// (This will be used when we add OTEL span generation)
	// span.SetAttributes(podCtx.OTELAttributes...)
}
```

**Note:** If `getPodByIP` currently returns `*v1.Pod`, update it to return `*domain.PodContext` instead (or create a new method).

**Acceptance Criteria:**
- ✅ Observer uses `domain.PodContext` instead of parsing pod labels/annotations
- ✅ No per-event attribute computation (use cached `OTELAttributes`)
- ✅ Observer integration tests still pass

---

### Step 5: Update Deployment Observer (If Applicable)

**File to modify:** `internal/observers/deployments/observer.go`

**Similar changes** - use `domain.DeploymentContext` with pre-computed OTEL attributes.

**Acceptance Criteria:**
- ✅ Deployment observer uses cached OTEL attributes
- ✅ No per-event computation

---

## Testing Requirements

### Unit Tests

**File:** `internal/services/k8scontext/enrichment_test.go`

```go
package k8scontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestComputeOTELAttributes_PriorityOrder(t *testing.T) {
	tests := []struct {
		name     string
		pod      *v1.Pod
		expected map[string]string
	}{
		{
			name: "env var wins over annotation and label",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					UID:       "abc-123",
					Labels: map[string]string{
						"app": "my-app-label",
					},
					Annotations: map[string]string{
						"otel.resource.service.name": "my-app-annotation",
					},
				},
				Spec: v1.PodSpec{
					NodeName: "node-1",
					Containers: []v1.Container{
						{
							Name: "main",
							Env: []v1.EnvVar{
								{Name: "OTEL_SERVICE_NAME", Value: "my-app-env"},
							},
						},
					},
				},
			},
			expected: map[string]string{
				"service.name":       "my-app-env", // ← Env var wins!
				"k8s.pod.name":       "test-pod",
				"k8s.namespace.name": "default",
				"k8s.pod.uid":        "abc-123",
				"k8s.node.name":      "node-1",
			},
		},
		{
			name: "annotation wins over label",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					UID:       "abc-123",
					Labels: map[string]string{
						"app": "my-app-label",
					},
					Annotations: map[string]string{
						"otel.resource.service.name": "my-app-annotation",
					},
				},
				Spec: v1.PodSpec{
					NodeName:   "node-1",
					Containers: []v1.Container{{Name: "main"}},
				},
			},
			expected: map[string]string{
				"service.name":       "my-app-annotation", // ← Annotation wins!
				"k8s.pod.name":       "test-pod",
				"k8s.namespace.name": "default",
				"k8s.pod.uid":        "abc-123",
				"k8s.node.name":      "node-1",
			},
		},
		{
			name: "label used when no env or annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					UID:       "abc-123",
					Labels: map[string]string{
						"app.kubernetes.io/name": "my-app-label",
					},
				},
				Spec: v1.PodSpec{
					NodeName:   "node-1",
					Containers: []v1.Container{{Name: "main"}},
				},
			},
			expected: map[string]string{
				"service.name":       "my-app-label", // ← Label used as fallback
				"k8s.pod.name":       "test-pod",
				"k8s.namespace.name": "default",
				"k8s.pod.uid":        "abc-123",
				"k8s.node.name":      "node-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := ComputeOTELAttributes(tt.pod)

			for k, expectedVal := range tt.expected {
				actualVal, ok := attrs[k]
				assert.True(t, ok, "Expected attribute %s to exist", k)
				assert.Equal(t, expectedVal, actualVal, "Attribute %s value mismatch", k)
			}
		})
	}
}

func TestComputeOTELAttributes_OwnerReference(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "abc-123",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "Deployment",
					Name: "my-deployment",
				},
			},
		},
		Spec: v1.PodSpec{
			NodeName:   "node-1",
			Containers: []v1.Container{{Name: "main"}},
		},
	}

	attrs := ComputeOTELAttributes(pod)

	assert.Equal(t, "my-deployment", attrs["k8s.deployment.name"])
}
```

**Acceptance Criteria:**
- ✅ Tests verify priority cascade (env → annotation → label)
- ✅ Tests verify all standard K8s attributes are included
- ✅ Tests verify owner reference extraction
- ✅ All tests pass

---

## Performance Validation

### Before Implementation (Baseline)

Run this to measure current performance:

```bash
cd internal/observers/network
go test -bench=BenchmarkEnrichEvent -benchmem
```

**Expected baseline:** ~100µs per event (slow due to per-event attribute computation)

### After Implementation (Target)

Run the same benchmark:

```bash
go test -bench=BenchmarkEnrichEvent -benchmem
```

**Target:** < 1µs per event (100x improvement)

**Create benchmark if it doesn't exist:**

```go
// internal/observers/network/observer_test.go
func BenchmarkEnrichEvent(b *testing.B) {
	observer := setupTestObserver()
	bpfEvent := &TCPEvent{
		SrcIP: "10.0.1.42",
		DstIP: "10.0.1.50",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		observer.enrichEvent(bpfEvent)
	}
}
```

---

## Definition of Done

- ✅ `pkg/domain/context.go` created with `PodContext.OTELAttributes`
- ✅ `internal/services/k8scontext/enrichment.go` created with `ComputeOTELAttributes`
- ✅ Context Service pre-computes attributes on pod add/update
- ✅ Network Observer uses pre-computed attributes (no per-event computation)
- ✅ Deployment Observer uses pre-computed attributes (if applicable)
- ✅ Unit tests pass (priority cascade, owner references)
- ✅ Integration tests pass (observer tests)
- ✅ Performance benchmark shows < 1µs per event (100x improvement)
- ✅ Code follows Tapio standards (no `map[string]interface{}`, proper error handling)
- ✅ PR description includes benchmark results (before/after)

---

## References

- **Beyla Pattern:** `/Users/yair/projects/tapio/docs/BEYLA_PATTERNS_IMPLEMENTATION.md` (Section 1)
- **Beyla Source:** `vendor/go.opentelemetry.io/obi/pkg/components/kube/store.go:cacheResourceMetadata`
- **Architecture Research:** `/Users/yair/projects/tapio/docs/ARCHITECTURE_RESEARCH_FINDINGS.md`
- **OTEL Semantic Conventions:** https://opentelemetry.io/docs/specs/semconv/resource/k8s/

---

## Questions?

Ask the architect (Yair) for clarification on:
- Context Service implementation details
- Observer structure/patterns
- Performance validation approach
