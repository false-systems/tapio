# TASK-003: Implement Prometheus Label Transformation

**Priority:** P0 - Critical (Blocks v1.0 - Correctness Issue)
**Estimated Effort:** 3-4 hours
**Skills Required:** Go, OpenTelemetry, Prometheus metrics
**Depends On:** TASK-001 (PodContext with OTELAttributes)

---

## Context

**CRITICAL BUG:** Prometheus rejects metric labels with dots (`.`) in the name, but OTEL semantic conventions use dot notation.

**Current problem:**
```go
// ❌ INVALID for Prometheus export
metric.WithAttributes(
    attribute.String("k8s.pod.name", "my-pod"),      // Dots not allowed!
    attribute.String("service.name", "my-service"),  // Dots not allowed!
)
```

**What happens:** Prometheus scrape fails or silently drops metrics with invalid labels.

**Beyla's solution:** Convert OTEL dot notation to Prometheus snake_case when exporting metrics:
- Internal: `k8s.pod.name` (OTEL convention)
- Export: `k8s_pod_name` (Prometheus-compatible)

**Reference:** `/Users/yair/projects/tapio/docs/BEYLA_PATTERNS_IMPLEMENTATION.md` (Section 3)

---

## Objective

Implement automatic label transformation for Prometheus export:
1. Create constants for both OTEL and Prometheus label names
2. Implement conversion function (`ToPrometheusLabel`)
3. Update all metric calls to use converted labels
4. Ensure OTEL traces/spans still use dot notation (not affected)

---

## Implementation Steps

### Step 1: Create `pkg/domain/attributes.go`

**New file:** `pkg/domain/attributes.go`

```go
package domain

import "strings"

// OTEL Semantic Conventions (internal representation)
// Use dot notation as per OTEL spec
// Reference: https://opentelemetry.io/docs/specs/semconv/resource/k8s/
const (
	// Service attributes
	OTELServiceName      = "service.name"
	OTELServiceNamespace = "service.namespace"
	OTELServiceVersion   = "service.version"
	OTELServiceInstance  = "service.instance.id"

	// Kubernetes resource attributes
	OTELPodName        = "k8s.pod.name"
	OTELPodUID         = "k8s.pod.uid"
	OTELNamespaceName  = "k8s.namespace.name"
	OTELNodeName       = "k8s.node.name"
	OTELContainerName  = "k8s.container.name"
	OTELDeploymentName = "k8s.deployment.name"
	OTELReplicaSetName = "k8s.replicaset.name"
	OTELStatefulSetName = "k8s.statefulset.name"
	OTELDaemonSetName   = "k8s.daemonset.name"

	// Network attributes
	OTELNetProtocol = "net.protocol.name"
	OTELNetPeerIP   = "net.peer.ip"
	OTELNetPeerPort = "net.peer.port"
	OTELNetHostIP   = "net.host.ip"
	OTELNetHostPort = "net.host.port"

	// HTTP attributes
	OTELHTTPMethod     = "http.request.method"
	OTELHTTPStatusCode = "http.response.status_code"
	OTELHTTPRoute      = "http.route"
)

// Prometheus Label Names (export format)
// Use snake_case for Prometheus compatibility
// Prometheus rejects dots in label names
const (
	// Service attributes
	PromServiceName      = "service_name"
	PromServiceNamespace = "service_namespace"
	PromServiceVersion   = "service_version"
	PromServiceInstance  = "service_instance_id"

	// Kubernetes resource attributes
	PromPodName         = "k8s_pod_name"
	PromPodUID          = "k8s_pod_uid"
	PromNamespaceName   = "k8s_namespace_name"
	PromNodeName        = "k8s_node_name"
	PromContainerName   = "k8s_container_name"
	PromDeploymentName  = "k8s_deployment_name"
	PromReplicaSetName  = "k8s_replicaset_name"
	PromStatefulSetName = "k8s_statefulset_name"
	PromDaemonSetName   = "k8s_daemonset_name"

	// Network attributes
	PromNetProtocol = "net_protocol_name"
	PromNetPeerIP   = "net_peer_ip"
	PromNetPeerPort = "net_peer_port"
	PromNetHostIP   = "net_host_ip"
	PromNetHostPort = "net_host_port"

	// HTTP attributes
	PromHTTPMethod     = "http_request_method"
	PromHTTPStatusCode = "http_response_status_code"
	PromHTTPRoute      = "http_route"
)

// ToPrometheusLabel converts OTEL dot notation to Prometheus snake_case
// Example: "k8s.pod.name" → "k8s_pod_name"
//
// Beyla pattern: Convert dots to underscores for Prometheus compatibility
// Reference: vendor/go.opentelemetry.io/obi/pkg/export/attributes
func ToPrometheusLabel(otelAttr string) string {
	return strings.ReplaceAll(otelAttr, ".", "_")
}

// AttributeMapper provides bidirectional mapping between OTEL and Prometheus
// For cases where we need to convert back (e.g., reading Prometheus data)
var AttributeMapper = map[string]string{
	OTELServiceName:      PromServiceName,
	OTELServiceNamespace: PromServiceNamespace,
	OTELServiceVersion:   PromServiceVersion,
	OTELServiceInstance:  PromServiceInstance,
	OTELPodName:          PromPodName,
	OTELPodUID:           PromPodUID,
	OTELNamespaceName:    PromNamespaceName,
	OTELNodeName:         PromNodeName,
	OTELContainerName:    PromContainerName,
	OTELDeploymentName:   PromDeploymentName,
	OTELReplicaSetName:   PromReplicaSetName,
	OTELStatefulSetName:  PromStatefulSetName,
	OTELDaemonSetName:    PromDaemonSetName,
	OTELNetProtocol:      PromNetProtocol,
	OTELNetPeerIP:        PromNetPeerIP,
	OTELNetPeerPort:      PromNetPeerPort,
	OTELNetHostIP:        PromNetHostIP,
	OTELNetHostPort:      PromNetHostPort,
	OTELHTTPMethod:       PromHTTPMethod,
	OTELHTTPStatusCode:   PromHTTPStatusCode,
	OTELHTTPRoute:        PromHTTPRoute,
}
```

**Acceptance Criteria:**
- ✅ File created at `pkg/domain/attributes.go`
- ✅ Constants for OTEL attributes (dot notation)
- ✅ Constants for Prometheus labels (snake_case)
- ✅ `ToPrometheusLabel` function converts dots to underscores
- ✅ Follows domain package conventions (no external dependencies)

---

### Step 2: Update `internal/base/metrics.go` - Apply Transformation

**File to modify:** `internal/base/metrics.go`

**Find all metric recording methods** and update them to use `ToPrometheusLabel`:

**Before (BROKEN - dots in labels):**
```go
func (m *ObserverMetrics) RecordEvent(ctx context.Context, observerName string, event *domain.ObserverEvent) {
	attrs := []attribute.KeyValue{
		attribute.String("observer.name", observerName),
	}

	if event != nil {
		attrs = append(attrs,
			attribute.String("event.type", event.Type),
			// ❌ WRONG: Using OTEL attributes directly for Prometheus
			// These will be rejected by Prometheus!
		)
	}

	m.EventsProcessed.Add(ctx, 1, metric.WithAttributes(attrs...))
}
```

**After (CORRECT - converted labels):**
```go
package base

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// RecordEvent records a successfully processed event
// Labels are converted to Prometheus-compatible format (dots → underscores)
func (m *ObserverMetrics) RecordEvent(ctx context.Context, observerName string, event *domain.ObserverEvent) {
	attrs := []attribute.KeyValue{
		// Use Prometheus-compatible label name
		attribute.String("observer_name", observerName),
	}

	if event != nil {
		attrs = append(attrs,
			attribute.String("event_type", event.Type),
			// Add more attributes as needed
		)
	}

	m.EventsProcessed.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordEventWithContext records event with K8s context
// This is the MAIN method that uses PodContext.OTELAttributes
func (m *ObserverMetrics) RecordEventWithContext(ctx context.Context, observerName string, podCtx *domain.PodContext, eventType string) {
	// Convert OTEL attributes to Prometheus labels
	attrs := make([]attribute.KeyValue, 0, len(podCtx.OTELAttributes)+2)

	// Observer and event type (already snake_case)
	attrs = append(attrs,
		attribute.String("observer_name", observerName),
		attribute.String("event_type", eventType),
	)

	// Convert PodContext OTEL attributes to Prometheus format
	for k, v := range podCtx.OTELAttributes {
		// ✅ CORRECT: Convert dots to underscores
		promKey := domain.ToPrometheusLabel(k)
		attrs = append(attrs, attribute.String(promKey, v))
	}

	m.EventsProcessed.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordDrop records a dropped event
func (m *ObserverMetrics) RecordDrop(ctx context.Context, observerName string, eventType string) {
	attrs := []attribute.KeyValue{
		attribute.String("observer_name", observerName),
		attribute.String("event_type", eventType),
	}

	m.EventsDropped.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordError records an error
func (m *ObserverMetrics) RecordError(ctx context.Context, observerName string, event *domain.ObserverEvent) {
	attrs := []attribute.KeyValue{
		attribute.String("observer_name", observerName),
	}

	if event != nil {
		attrs = append(attrs,
			attribute.String("event_type", event.Type),
		)
		if IsErrorEvent(event) {
			attrs = append(attrs, ErrorTypeAttribute(event.Type))
		}
	}

	m.ErrorsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordProcessingTime records processing duration
func (m *ObserverMetrics) RecordProcessingTime(ctx context.Context, observerName string, event *domain.ObserverEvent, durationMs float64) {
	attrs := []attribute.KeyValue{
		attribute.String("observer_name", observerName),
	}

	if event != nil {
		attrs = append(attrs,
			attribute.String("event_type", event.Type),
		)
	}

	// Histogram automatically creates exemplars with trace context from ctx
	m.ProcessingTime.Record(ctx, durationMs, metric.WithAttributes(attrs...))
}

// RecordPipelineQueueDepth records queue depth
func (m *ObserverMetrics) RecordPipelineQueueDepth(ctx context.Context, observerName string, depth int64) {
	attrs := []attribute.KeyValue{
		attribute.String("observer_name", observerName),
	}
	m.PipelineQueueDepth.Record(ctx, depth, metric.WithAttributes(attrs...))
}

// RecordPipelineQueueUtilization records queue utilization (0.0-1.0)
func (m *ObserverMetrics) RecordPipelineQueueUtilization(ctx context.Context, observerName string, utilization float64) {
	attrs := []attribute.KeyValue{
		attribute.String("observer_name", observerName),
	}
	m.PipelineQueueUtilization.Record(ctx, utilization, metric.WithAttributes(attrs...))
}

// Helper: Convert event domain to Prometheus-compatible attribute
func EventDomainAttribute(eventType string) attribute.KeyValue {
	// Derive domain from event type
	// network.connection_established → network
	// kernel.oom_kill → kernel
	domain := extractDomain(eventType)
	return attribute.String("event_domain", domain)
}

func extractDomain(eventType string) string {
	// Extract domain prefix before first dot or underscore
	// For now, simple implementation
	// TODO: Improve parsing if event types become more complex
	if len(eventType) == 0 {
		return "unknown"
	}
	// Simple heuristic: return type as-is
	return eventType
}

// Helper: Error type attribute
func ErrorTypeAttribute(eventType string) attribute.KeyValue {
	return attribute.String("error_type", eventType)
}

// Helper: Check if event is error
func IsErrorEvent(event *domain.ObserverEvent) bool {
	// TODO: Add proper error detection logic
	// For now, check if event type contains "error" or "failed"
	return false
}
```

**Acceptance Criteria:**
- ✅ All metric methods updated to use Prometheus-compatible labels
- ✅ `RecordEventWithContext` converts `PodContext.OTELAttributes` using `ToPrometheusLabel`
- ✅ No dots in label names (verify with regex search)
- ✅ Existing metric tests updated and passing

---

### Step 3: Update Observer Metric Calls

**Files to modify:**
- `internal/observers/network/observer.go`
- `internal/observers/deployments/observer.go`
- `internal/observers/status/observer.go` (if exists)

**Update observer code to use new `RecordEventWithContext` method:**

**Example (Network Observer):**
```go
// Before (no context)
func (o *NetworkObserver) recordEvent(eventType string) {
	o.metrics.RecordEvent(context.Background(), "network", nil)
}

// After (with PodContext)
func (o *NetworkObserver) recordEvent(podCtx *domain.PodContext, eventType string) {
	// PodContext has pre-computed OTEL attributes (from TASK-001)
	// Metrics layer will convert them to Prometheus format
	o.metrics.RecordEventWithContext(
		context.Background(),
		"network",
		podCtx,
		eventType,
	)
}
```

**Acceptance Criteria:**
- ✅ Observers pass `PodContext` to metric methods
- ✅ Observers use `RecordEventWithContext` instead of basic `RecordEvent`
- ✅ Observer tests updated

---

### Step 4: Verify OTEL Traces Still Use Dot Notation

**IMPORTANT:** OTEL traces/spans should continue using dot notation (not affected by this change).

**File to check:** Any code that creates OTEL spans (likely in observers)

**Example (should remain unchanged):**
```go
// ✅ CORRECT: OTEL spans use dot notation
span.SetAttributes(
	attribute.String("k8s.pod.name", podName),      // Dots OK for traces!
	attribute.String("service.name", serviceName),  // Dots OK for traces!
)
```

**Rule:**
- **Metrics → Prometheus labels → snake_case** (this task)
- **Traces/Spans → OTEL attributes → dot notation** (unchanged)

**Acceptance Criteria:**
- ✅ Span attributes still use OTEL dot notation
- ✅ No breaking changes to trace export

---

## Testing Requirements

### Unit Tests

**File:** `pkg/domain/attributes_test.go`

```go
package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToPrometheusLabel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "k8s pod name",
			input:    "k8s.pod.name",
			expected: "k8s_pod_name",
		},
		{
			name:     "service name",
			input:    "service.name",
			expected: "service_name",
		},
		{
			name:     "multiple dots",
			input:    "http.request.method",
			expected: "http_request_method",
		},
		{
			name:     "no dots",
			input:    "event_type",
			expected: "event_type",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToPrometheusLabel(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAttributeMapper(t *testing.T) {
	// Verify all OTEL→Prom mappings are correct
	assert.Equal(t, "service_name", AttributeMapper[OTELServiceName])
	assert.Equal(t, "k8s_pod_name", AttributeMapper[OTELPodName])
	assert.Equal(t, "k8s_namespace_name", AttributeMapper[OTELNamespaceName])

	// Verify all mapped values are snake_case (no dots)
	for otel, prom := range AttributeMapper {
		assert.NotContains(t, prom, ".", "Prometheus label should not contain dots: %s → %s", otel, prom)
	}
}
```

### Integration Tests

**File:** `internal/base/metrics_test.go`

```go
package base

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestRecordEventWithContext_PrometheusLabels(t *testing.T) {
	metrics, err := NewObserverMetrics("test")
	assert.NoError(t, err)

	podCtx := &domain.PodContext{
		Name:      "test-pod",
		Namespace: "default",
		OTELAttributes: map[string]string{
			"k8s.pod.name":       "test-pod",        // Dot notation
			"k8s.namespace.name": "default",         // Dot notation
			"service.name":       "my-service",      // Dot notation
		},
	}

	// Record event with context
	metrics.RecordEventWithContext(context.Background(), "network", podCtx, "connection_established")

	// Verify: Metric recorded with converted labels
	// (This test would require access to metric internals or mock exporter)
	// For now, just verify no panic/error
}
```

### Manual Verification

**Test Prometheus scrape endpoint:**

```bash
# Start observer with metrics
go run ./cmd/observers

# Scrape metrics endpoint
curl http://localhost:9090/metrics | grep observer_events_processed

# Expected output (snake_case labels):
# observer_events_processed_total{observer_name="network",event_type="connection_established",k8s_pod_name="test-pod",service_name="my-service"} 42

# Should NOT see dots in labels:
# ❌ observer_events_processed_total{k8s.pod.name="test-pod"} 42  # WRONG!
```

**Acceptance Criteria:**
- ✅ All Prometheus metrics use snake_case labels
- ✅ No dots in any metric label names
- ✅ Prometheus scrape succeeds (no rejected metrics)

---

## Definition of Done

- ✅ `pkg/domain/attributes.go` created with OTEL and Prometheus constants
- ✅ `ToPrometheusLabel` function implemented and tested
- ✅ `internal/base/metrics.go` updated to convert labels
- ✅ `RecordEventWithContext` method converts `PodContext.OTELAttributes`
- ✅ All observers updated to use new metric methods
- ✅ OTEL spans still use dot notation (unchanged)
- ✅ Unit tests pass (label conversion)
- ✅ Integration tests pass (metric recording)
- ✅ Manual verification: Prometheus scrape shows snake_case labels only
- ✅ No dots in any Prometheus metric label (verified with grep/regex)
- ✅ Code follows Tapio standards
- ✅ PR description includes before/after metric examples

---

## References

- **Beyla Pattern:** `/Users/yair/projects/tapio/docs/BEYLA_PATTERNS_IMPLEMENTATION.md` (Section 3)
- **Prometheus Label Naming:** https://prometheus.io/docs/practices/naming/#labels
- **OTEL Semantic Conventions:** https://opentelemetry.io/docs/specs/semconv/
- **TASK-001:** Pre-computed OTEL attributes (provides `PodContext.OTELAttributes`)

---

## Questions?

Ask the architect (Yair) for clarification on:
- Metric naming conventions
- Additional attributes to include
- Prometheus exporter configuration
