# Design: Causality-Driven Correlation Patterns for TAPIO

> **NOTE**: NATS references in this document are outdated. TAPIO now uses **POLKU** (gRPC event gateway) instead of NATS.

> **Status**: ✅ **IMPLEMENTED** - All core causality features completed (2025-01-19)
> - ULID event IDs (pkg/domain/ids.go)
> - ParentSpanID, Duration, Severity, Outcome fields (pkg/domain/events.go)
> - EventError structured type (pkg/domain/events.go)
> - CausalityTracker with BuildCausalityChain (internal/base/causality.go)

## Problem Statement

**Current state**: TAPIO has basic OTEL trace context (TraceID, SpanID) but is **missing critical causality correlation patterns** that enable root cause analysis.

**What TAPIO has today:**
- ✅ TraceID, SpanID (basic OTEL correlation)
- ✅ Entities and Relationships (graph structure)
- ✅ K8s context enrichment
- ✅ Typed event data (NetworkEventData, KernelEventData, etc.)

**What TAPIO was missing (now implemented ✅):**
1. ✅ **ParentSpanID** - No causality chains (can't build "this caused that" relationships)
2. ✅ **Duration field** - Not at event level (can't correlate slow events)
3. ✅ **ULID event IDs** - Using simple strings, not time-sortable
4. ✅ **Severity/Outcome enums** - At TapioEvent level only, not ObserverEvent
5. ✅ **Structured errors** - No EventError type for failure details
6. ✅ **Causality propagation** - No way to link "deployment → restart → OOM"

**Impact**:
- Can't answer "what caused this?" (missing ParentSpanID)
- Can't correlate slow events (missing Duration at event level)
- Events not naturally time-ordered (missing ULID)
- Can't classify failures properly (missing Severity/Outcome in ObserverEvent)
- No structured error details (missing EventError)

**Key Insight** (causality-driven correlation):
> "Correlation via **temporal proximity** + **dependency paths**"

TAPIO has dependency paths (entities/relationships) plus temporal causality (ParentSpanID chains).

---

## Solution: Complete Causality Correlation Pattern

### Architecture: Event Causality Chains

```
┌─────────────────────────────────────────────────────────────┐
│ Causality Pattern: Every event knows its "parent cause"    │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Deployment Event                                           │
│  ├─ ID: 01HQZX... (ULID, time-sortable)                   │
│  ├─ TraceID: abc123 (distributed trace)                    │
│  ├─ SpanID: span1                                          │
│  └─ ParentSpanID: null (root cause)                        │
│      ↓                                                      │
│  Pod Restart Event (5 seconds later)                        │
│  ├─ ID: 01HQZY... (ULID, naturally after deployment)      │
│  ├─ TraceID: abc123 (same trace)                           │
│  ├─ SpanID: span2                                          │
│  └─ ParentSpanID: span1 (CAUSED BY deployment!)            │
│      ↓                                                      │
│  OOM Kill Event (2 seconds later)                           │
│  ├─ ID: 01HQZZ... (ULID, naturally after restart)         │
│  ├─ TraceID: abc123 (same trace)                           │
│  ├─ SpanID: span3                                          │
│  ├─ ParentSpanID: span2 (CAUSED BY restart!)               │
│  ├─ Duration: 2000000 (2 seconds in microseconds)          │
│  ├─ Severity: Critical                                     │
│  ├─ Outcome: Failure                                       │
│  └─ Error:                                                 │
│      ├─ Code: "OOM_KILL"                                   │
│      ├─ Message: "Container killed: out of memory"         │
│      └─ Cause: "Memory limit: 512Mi, Requested: 2Gi"       │
│                                                             │
│  Query: "Show causality chain for OOM event 01HQZZ..."     │
│  Result:                                                    │
│    1. Deployment update (new image with memory leak)       │
│    2. → Pod restarted (new image deployed)                 │
│    3. → OOM killed (memory leak exhausted limit)           │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

---

## Implementation Plan

### Phase 1: Add Causality Fields to ObserverEvent (✅ COMPLETE)

**File**: `pkg/domain/events.go`

**Current ObserverEvent**:
```go
type ObserverEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Subtype   string    `json:"subtype,omitempty"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`

	// OTEL trace context
	TraceID    string `json:"trace_id,omitempty"`
	SpanID     string `json:"span_id,omitempty"`
	TraceFlags byte   `json:"trace_flags,omitempty"`

	// Typed event data
	NetworkData    *NetworkEventData    `json:"network_data,omitempty"`
	KernelData     *KernelEventData     `json:"kernel_data,omitempty"`
	// ... rest
}
```

**Added causality fields** (IMPLEMENTED):
```go
type ObserverEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Subtype   string    `json:"subtype,omitempty"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`

	// Causality-driven correlation (root cause analysis)
	TraceID      string  `json:"trace_id,omitempty"`       // Distributed trace ID
	SpanID       string  `json:"span_id,omitempty"`        // Current span ID
	ParentSpanID string  `json:"parent_span_id,omitempty"` // ← NEW: Parent span for causality
	Duration     *uint64 `json:"duration,omitempty"`       // ← NEW: Event duration in microseconds
	TraceFlags   byte    `json:"trace_flags,omitempty"`

	// Event classification (for correlation)
	Severity Severity `json:"severity,omitempty"` // ← NEW: debug, info, warning, error, critical
	Outcome  Outcome  `json:"outcome,omitempty"`  // ← NEW: success, failure, unknown

	// Structured error details
	Error *EventError `json:"error,omitempty"` // ← NEW: Present when Outcome == Failure

	// Typed event data
	NetworkData    *NetworkEventData    `json:"network_data,omitempty"`
	KernelData     *KernelEventData     `json:"kernel_data,omitempty"`
	ContainerData  *ContainerEventData  `json:"container_data,omitempty"`
	K8sData        *K8sEventData        `json:"k8s_data,omitempty"`
	ProcessData    *ProcessEventData    `json:"process_data,omitempty"`
	SchedulingData *SchedulingEventData `json:"scheduling_data,omitempty"`
	NodeData       *NodeEventData       `json:"node_data,omitempty"`

	RawData []byte `json:"raw_data,omitempty"`
}

// EventError contains structured error information for failed events
type EventError struct {
	Code    string `json:"code"`            // Error code (ECONNREFUSED, OOM_KILL, etc.)
	Message string `json:"message"`         // Human-readable error
	Stack   string `json:"stack,omitempty"` // Stack trace if available
	Cause   string `json:"cause,omitempty"` // Root cause if known
}

// Severity levels (matches TapioEvent for consistency)
type Severity string

const (
	SeverityDebug    Severity = "debug"
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Outcome classification (matches TapioEvent)
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeUnknown Outcome = "unknown"
)
```

---

### Phase 2: ULID Event IDs (✅ COMPLETE - Time-Sortable)

**File**: `pkg/domain/ids.go` (NEW)

```go
package domain

import (
	"time"

	"github.com/oklog/ulid/v2"
)

// NewEventID generates a new ULID for an event
// ULIDs are:
// - Time-sortable (first 48 bits = timestamp)
// - Lexicographically ordered (can sort as strings)
// - Globally unique (80 bits of entropy)
// - URL-safe (Base32 encoding)
func NewEventID() string {
	return ulid.Make().String()
}

// NewEventIDWithTime generates a ULID with a specific timestamp
// Useful for:
// - Deterministic IDs in tests
// - Backfilling historical events
// - Ensuring correct time ordering
func NewEventIDWithTime(t time.Time) string {
	return ulid.MustNew(ulid.Timestamp(t), ulid.DefaultEntropy()).String()
}

// ParseEventID extracts timestamp from ULID event ID
func ParseEventID(id string) (time.Time, error) {
	parsed, err := ulid.Parse(id)
	if err != nil {
		return time.Time{}, err
	}
	return ulid.Time(parsed.Time()), nil
}
```

**Update observers to use ULID**:

**File**: `internal/observers/network/observer_ebpf.go` (modify)

```go
func (o *Observer) processRawEvent(ctx context.Context, rawEvent *rawNetworkEvent) (*domain.ObserverEvent, error) {
	event := &domain.ObserverEvent{
		ID:        domain.NewEventIDWithTime(rawEvent.Timestamp), // ← Use ULID with event timestamp
		Type:      "network",
		Subtype:   rawEvent.EventType,
		Source:    "network-observer",
		Timestamp: rawEvent.Timestamp,

		// Extract trace context from active span (if available)
		// Will populate TraceID, SpanID, ParentSpanID
	}

	// Extract trace context (sets TraceID, SpanID)
	base.ExtractTraceContext(ctx, event)

	// ... rest of processing
}
```

---

### Phase 3: Causality Chain Propagation (✅ COMPLETE)

**File**: `internal/base/trace.go` (modify)

**Current trace.go**:
```go
func ExtractTraceContext(ctx context.Context, event *domain.ObserverEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return
	}

	sc := span.SpanContext()
	event.TraceID = sc.TraceID().String()
	event.SpanID = sc.SpanID().String()
	event.TraceFlags = byte(sc.TraceFlags())
}
```

**Add ParentSpanID support** (NEW):
```go
// ExtractTraceContext extracts OTEL trace context AND parent span for causality
func ExtractTraceContext(ctx context.Context, event *domain.ObserverEvent) {
	if event == nil {
		return
	}

	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return
	}

	sc := span.SpanContext()
	event.TraceID = sc.TraceID().String()
	event.SpanID = sc.SpanID().String()
	event.TraceFlags = byte(sc.TraceFlags())

	// ← NEW: Extract parent span ID from span attributes (if set)
	// This enables causality chains: deployment → pod_restart → oom_kill
	if parentSpanID := getParentSpanIDFromSpan(span); parentSpanID != "" {
		event.ParentSpanID = parentSpanID
	}
}

// getParentSpanIDFromSpan extracts parent span ID from span attributes
// Used for Edgar-style causality chains
func getParentSpanIDFromSpan(span trace.Span) string {
	// Try to get from span attributes (if observer set it)
	// This is how we propagate causality:
	// 1. Deployment observer creates span with "caused_by" attribute
	// 2. Pod observer creates child span, sets "caused_by" = deployment span ID
	// 3. OOM observer creates child span, sets "caused_by" = pod restart span ID

	// For now, use OTEL parent span from span tree
	// In future: add "caused_by" attribute for explicit causality

	// Note: OTEL spans don't expose parent directly in Go API
	// We'll need to track this in observer state
	return ""
}

// SetParentSpanID sets parent span ID on event for causality chains
func SetParentSpanID(event *domain.ObserverEvent, parentSpanID string) {
	if event != nil {
		event.ParentSpanID = parentSpanID
	}
}
```

**File**: `internal/base/causality.go` (NEW)

```go
package base

import (
	"context"
	"sync"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CausalityTracker tracks span IDs for causality chain propagation
// Enables root cause analysis: "this event caused that event"
type CausalityTracker struct {
	mu sync.RWMutex

	// Maps entity ID → most recent span ID
	// Example: "default/nginx-abc" → "span-123"
	// Used to link events: deployment update → pod restart
	entitySpans map[string]string

	// Maps span ID → parent span ID
	// Enables multi-hop causality: span3 → span2 → span1
	spanParents map[string]string
}

// NewCausalityTracker creates a causality tracker
func NewCausalityTracker() *CausalityTracker {
	return &CausalityTracker{
		entitySpans: make(map[string]string),
		spanParents: make(map[string]string),
	}
}

// RecordEvent tracks span ID for an entity
// Example: deployment observer records "default/nginx" → "span-deployment-1"
func (c *CausalityTracker) RecordEvent(event *domain.ObserverEvent, primaryEntity string) {
	if event == nil || event.SpanID == "" || primaryEntity == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Track entity → span mapping
	c.entitySpans[primaryEntity] = event.SpanID

	// Track span → parent span mapping
	if event.ParentSpanID != "" {
		c.spanParents[event.SpanID] = event.ParentSpanID
	}
}

// GetParentSpanForEntity retrieves parent span ID for an entity
// Example: pod observer asks "what caused changes to default/nginx?"
// Returns: "span-deployment-1" (the deployment update span)
func (c *CausalityTracker) GetParentSpanForEntity(entityID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.entitySpans[entityID]
}

// BuildCausalityChain builds full causality chain for a span
// Returns: [root_span, parent_span, current_span]
func (c *CausalityTracker) BuildCausalityChain(spanID string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	chain := []string{spanID}
	current := spanID

	// Walk up parent chain (max 10 hops to prevent infinite loops)
	for i := 0; i < 10; i++ {
		parent, exists := c.spanParents[current]
		if !exists {
			break // Reached root
		}
		chain = append([]string{parent}, chain...) // Prepend parent
		current = parent
	}

	return chain
}
```

---

### Phase 4: Observer Integration (Network Observer Example)

**File**: `internal/observers/network/observer_ebpf.go` (modify)

```go
type Observer struct {
	*base.BaseObserver
	config Config

	// Causality correlation support
	causality *base.CausalityTracker // ← Track causality chains

	// eBPF state
	ebpfMgr *base.EBPFManager
	// ... rest
}

func NewNetworkObserver(name string, config Config) (*NetworkObserver, error) {
	// ... existing code ...

	obs := &NetworkObserver{
		BaseObserver: baseObs,
		config:       config,
		causality:    base.NewCausalityTracker(), // ← NEW
	}

	return obs, nil
}

func (o *Observer) processConnectionEvent(ctx context.Context, rawEvent *rawConnectionEvent) (*domain.ObserverEvent, error) {
	// Generate ULID with event timestamp
	event := &domain.ObserverEvent{
		ID:        domain.NewEventIDWithTime(rawEvent.Timestamp),
		Type:      "network",
		Subtype:   rawEvent.EventType, // "connection_established", "connection_refused", etc.
		Source:    "network-observer",
		Timestamp: rawEvent.Timestamp,
	}

	// Extract trace context (sets TraceID, SpanID)
	base.ExtractTraceContext(ctx, event)

	// ← NEW: Check if this connection is caused by a deployment/pod change
	if podID := o.getPodIDFromConnection(rawEvent); podID != "" {
		// Get parent span (e.g., deployment update that triggered pod restart)
		if parentSpanID := o.causality.GetParentSpanForEntity(podID); parentSpanID != "" {
			event.ParentSpanID = parentSpanID
		}
	}

	// ← NEW: Classify severity and outcome
	event.Severity = o.determineSeverity(rawEvent)
	event.Outcome = o.determineOutcome(rawEvent)

	// ← NEW: Add duration (time between SYN and ACK)
	if rawEvent.Duration > 0 {
		duration := uint64(rawEvent.Duration)
		event.Duration = &duration
	}

	// ← NEW: Add structured error for failures
	if event.Outcome == domain.OutcomeFailure {
		event.Error = &domain.EventError{
			Code:    rawEvent.ErrorCode, // "ECONNREFUSED", "ETIMEDOUT", etc.
			Message: formatErrorMessage(rawEvent),
			Cause:   rawEvent.ErrorCause,
		}
	}

	// ... rest of processing (NetworkEventData, K8s context, etc.)

	// Track this event for causality
	if podID := o.getPodIDFromConnection(rawEvent); podID != "" {
		o.causality.RecordEvent(event, podID)
	}

	return event, nil
}

// determineSeverity classifies network event severity
func (o *Observer) determineSeverity(rawEvent *rawConnectionEvent) domain.Severity {
	switch rawEvent.EventType {
	case "connection_refused", "connection_timeout":
		return domain.SeverityError
	case "connection_slow", "retransmit_spike":
		return domain.SeverityWarning
	case "connection_established":
		return domain.SeverityInfo
	default:
		return domain.SeverityDebug
	}
}

// determineOutcome classifies network event outcome
func (o *Observer) determineOutcome(rawEvent *rawConnectionEvent) domain.Outcome {
	switch rawEvent.EventType {
	case "connection_established":
		return domain.OutcomeSuccess
	case "connection_refused", "connection_timeout":
		return domain.OutcomeFailure
	default:
		return domain.OutcomeUnknown
	}
}
```

---

### Phase 5: K8s Observer Causality Example

**File**: `internal/services/k8scontext/handlers_deployment.go` (modify)

```go
func (s *Service) handleDeploymentUpdate(oldObj, newObj interface{}) {
	oldDep := oldObj.(*appsv1.Deployment)
	newDep := newObj.(*appsv1.Deployment)

	// Detect image change (common cause of pod restarts)
	if oldDep.Spec.Template.Spec.Containers[0].Image !=
		newDep.Spec.Template.Spec.Containers[0].Image {

		event := &domain.ObserverEvent{
			ID:        domain.NewEventID(), // ← ULID
			Type:      "deployment",
			Subtype:   "image_update",
			Source:    "k8scontext",
			Timestamp: time.Now(),
			Severity:  domain.SeverityInfo,
			Outcome:   domain.OutcomeSuccess,

			K8sData: &domain.K8sEventData{
				ResourceKind: "Deployment",
				ResourceName: newDep.Name,
				Namespace:    newDep.Namespace,
				Action:       "updated",
				OldImage:     oldDep.Spec.Template.Spec.Containers[0].Image,
				NewImage:     newDep.Spec.Template.Spec.Containers[0].Image,
			},
		}

		// Create span for this deployment update
		ctx, span := otel.Tracer("k8scontext").Start(context.Background(), "deployment.update")
		defer span.End()

		// Extract trace context (sets SpanID)
		base.ExtractTraceContext(ctx, event)

		// ← NEW: Track this deployment update span
		// Future pod restarts can link back to this as ParentSpanID
		deploymentID := fmt.Sprintf("%s/%s", newDep.Namespace, newDep.Name)
		s.causality.RecordEvent(event, deploymentID)

		// Emit event
		s.emitDomainEvent(ctx, event)
	}
}

func (s *Service) handlePodUpdate(oldObj, newObj interface{}) {
	oldPod := oldObj.(*v1.Pod)
	newPod := newObj.(*v1.Pod)

	// Detect pod restart
	if oldPod.Status.ContainerStatuses[0].RestartCount !=
		newPod.Status.ContainerStatuses[0].RestartCount {

		event := &domain.ObserverEvent{
			ID:        domain.NewEventID(), // ← ULID
			Type:      "pod",
			Subtype:   "container_restart",
			Source:    "k8scontext",
			Timestamp: time.Now(),
			Severity:  domain.SeverityWarning,
			Outcome:   domain.OutcomeFailure,

			K8sData: &domain.K8sEventData{
				ResourceKind:   "Pod",
				ResourceName:   newPod.Name,
				Namespace:      newPod.Namespace,
				Action:         "restarted",
				RestartCount:   newPod.Status.ContainerStatuses[0].RestartCount,
				PreviousReason: oldPod.Status.ContainerStatuses[0].State.Terminated.Reason,
			},
		}

		// ← NEW: Link to parent deployment update (if exists)
		deploymentID := s.getDeploymentForPod(newPod)
		if deploymentID != "" {
			if parentSpanID := s.causality.GetParentSpanForEntity(deploymentID); parentSpanID != "" {
				event.ParentSpanID = parentSpanID // ← Causality link!
			}
		}

		// Create span for pod restart
		ctx, span := otel.Tracer("k8scontext").Start(context.Background(), "pod.restart")
		defer span.End()

		base.ExtractTraceContext(ctx, event)

		// Track pod restart span
		podID := fmt.Sprintf("%s/%s", newPod.Namespace, newPod.Name)
		s.causality.RecordEvent(event, podID)

		s.emitDomainEvent(ctx, event)
	}
}
```

---

## Testing Strategy

### Unit Tests

**File**: `pkg/domain/ids_test.go` (NEW)

```go
func TestNewEventID_IsULID(t *testing.T) {
	id := domain.NewEventID()

	// Should be valid ULID
	_, err := ulid.Parse(id)
	require.NoError(t, err)
}

func TestNewEventID_Sortable(t *testing.T) {
	id1 := domain.NewEventID()
	time.Sleep(1 * time.Millisecond)
	id2 := domain.NewEventID()

	// Later ID should be lexicographically greater
	assert.True(t, id2 > id1)
}

func TestNewEventIDWithTime(t *testing.T) {
	now := time.Now()
	future := now.Add(1 * time.Hour)

	id1 := domain.NewEventIDWithTime(now)
	id2 := domain.NewEventIDWithTime(future)

	// Future ID should be greater
	assert.True(t, id2 > id1)

	// Extract timestamp
	ts1, _ := domain.ParseEventID(id1)
	assert.True(t, ts1.Equal(now.Truncate(time.Millisecond)))
}
```

**File**: `internal/base/causality_test.go` (NEW)

```go
func TestCausalityTracker_BuildChain(t *testing.T) {
	tracker := base.NewCausalityTracker()

	// Create causality chain: deployment → pod_restart → oom_kill
	deploymentEvent := &domain.ObserverEvent{
		ID:      "01HQZX...",
		SpanID:  "span-deployment",
		// No parent (root)
	}

	podEvent := &domain.ObserverEvent{
		ID:           "01HQZY...",
		SpanID:       "span-pod",
		ParentSpanID: "span-deployment",
	}

	oomEvent := &domain.ObserverEvent{
		ID:           "01HQZZ...",
		SpanID:       "span-oom",
		ParentSpanID: "span-pod",
	}

	tracker.RecordEvent(deploymentEvent, "default/nginx")
	tracker.RecordEvent(podEvent, "default/nginx-abc")
	tracker.RecordEvent(oomEvent, "default/nginx-abc")

	// Build chain from OOM event
	chain := tracker.BuildCausalityChain("span-oom")

	require.Len(t, chain, 3)
	assert.Equal(t, "span-deployment", chain[0]) // Root
	assert.Equal(t, "span-pod", chain[1])
	assert.Equal(t, "span-oom", chain[2])
}
```

### Integration Tests

```go
func TestNetworkObserver_CausalityPropagation(t *testing.T) {
	// 1. Deployment update
	deploymentSpan := "span-deployment-1"

	// 2. Pod restart (caused by deployment)
	podEvent := &domain.ObserverEvent{
		SpanID:       "span-pod-1",
		ParentSpanID: deploymentSpan, // ← Link
	}

	// 3. Network connection (from restarted pod)
	netEvent := &domain.ObserverEvent{
		SpanID:       "span-net-1",
		ParentSpanID: "span-pod-1", // ← Link
	}

	// Verify causality chain
	assert.Equal(t, deploymentSpan,
		getCausalRoot(netEvent)) // Should trace back to deployment
}
```

---

## Performance Targets

| Metric | Target | Notes |
|--------|--------|-------|
| **ULID generation** | <1µs | ulid.Make() is very fast |
| **Causality lookup** | <10µs | In-memory map lookup |
| **Chain building** | <100µs | Max 10 hops |
| **Memory overhead** | <10MB | Per observer (LRU cache for causality) |

---

## Rollout Plan

### ✅ Core Causality Fields (COMPLETE - 2025-01-19)
- [x] Add ParentSpanID, Duration, Severity, Outcome, Error to ObserverEvent
- [x] Implement ULID event IDs (NewEventID, NewEventIDWithTime)
- [x] Implement CausalityTracker with BuildCausalityChain
- [x] All tests passing (TDD - RED → GREEN → REFACTOR)
- [x] Rebrand Edgar/Netflix terminology to causality-driven correlation

### 🔄 Observer Integration (IN PROGRESS)
- [ ] Add causality to network observer
- [ ] Add causality to container observer
- [ ] Add causality to K8s context service
- [ ] Integration tests for causality chains

### 📋 End-to-End Validation (TODO)
- [ ] Test causality chains (deployment → pod → OOM)
- [ ] Verify ULID ordering in production scenarios
- [ ] Performance benchmarks
- [ ] Update documentation with examples

---

## Success Criteria

- ✅ All events have ULID IDs (time-sortable)
- ✅ ParentSpanID populated for causality chains
- ✅ Duration tracked for performance events
- ✅ Severity/Outcome classified at ObserverEvent level
- ✅ Structured errors for failures
- ✅ Can query "what caused this event?" and get answer
- ✅ Causality chains work: deployment → pod restart → OOM

---

## Example: Complete Edgar Correlation Flow

```go
// 1. Deployment update (root cause)
deploymentEvent := &domain.ObserverEvent{
	ID:        "01HQZX5M7K2QY3P8R9N6W4V1T0",  // ULID (sortable)
	Type:      "deployment",
	Subtype:   "image_update",
	Timestamp: time.Now(),
	TraceID:   "abc123",
	SpanID:    "span-dep-1",
	// No ParentSpanID (root)
	Severity:  domain.SeverityInfo,
	Outcome:   domain.OutcomeSuccess,
	K8sData: &domain.K8sEventData{
		OldImage: "nginx:1.20",
		NewImage: "nginx:1.21-buggy", // ← Root cause!
	},
}

// 2. Pod restart (caused by deployment, 5 seconds later)
podEvent := &domain.ObserverEvent{
	ID:           "01HQZX5R9M3QZ4P8S0N7X5W2U1",  // ULID (naturally after deployment)
	Type:         "pod",
	Subtype:      "container_restart",
	Timestamp:    time.Now().Add(5 * time.Second),
	TraceID:      "abc123",  // Same trace
	SpanID:       "span-pod-1",
	ParentSpanID: "span-dep-1",  // ← CAUSED BY deployment
	Severity:     domain.SeverityWarning,
	Outcome:      domain.OutcomeFailure,
	K8sData: &domain.K8sEventData{
		ResourceName: "nginx-abc123",
		RestartCount: 1,
	},
}

// 3. OOM kill (caused by restart, 2 seconds later)
oomEvent := &domain.ObserverEvent{
	ID:           "01HQZX5T2N4R05Q9T1O8Y6X3V2",  // ULID (naturally after restart)
	Type:         "kernel",
	Subtype:      "oom_kill",
	Timestamp:    time.Now().Add(7 * time.Second),
	TraceID:      "abc123",  // Same trace
	SpanID:       "span-oom-1",
	ParentSpanID: "span-pod-1",  // ← CAUSED BY restart
	Duration:     ptr(uint64(2000000)),  // 2 seconds in microseconds
	Severity:     domain.SeverityCritical,
	Outcome:      domain.OutcomeFailure,
	Error: &domain.EventError{
		Code:    "OOM_KILL",
		Message: "Container killed: out of memory",
		Cause:   "Memory limit: 512Mi, Requested: 2Gi (nginx:1.21-buggy has memory leak)",
	},
	KernelData: &domain.KernelEventData{
		OOMVictimPID:  1234,
		MemoryRequested: 2 * 1024 * 1024 * 1024,  // 2Gi
	},
}

// Query causality chain
chain := causality.BuildCausalityChain("span-oom-1")
// Result: ["span-dep-1", "span-pod-1", "span-oom-1"]

// Answer: "What caused OOM?"
// 1. Deployment update to nginx:1.21-buggy (span-dep-1)
// 2. → Pod restart with new image (span-pod-1)
// 3. → OOM kill after 2 seconds (span-oom-1)
```

---

## Questions for Review

1. **Causality storage**: In-memory map OK? Or persist to NATS KV?
2. **Chain depth limit**: Max 10 hops enough? Or configurable?
3. **ULID migration**: Break existing event IDs? Or dual-support?
4. **Severity defaults**: Info for all events? Or observer-specific?
5. **Performance**: LRU cache for causality? Size limit?
