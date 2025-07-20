# K8s Collector UnifiedEvent Implementation

## Summary

This document describes the changes made to implement UnifiedEvent architecture in the Kubernetes collector, transforming it from the old domain.Event model to the new UnifiedEvent format with rich semantic correlation.

## Architecture Changes

### Before: Two-Step Conversion
```
K8s API → RawEvent → domain.Event → Analytics
```

### After: Direct Conversion
```
K8s API → RawEvent → UnifiedEvent → Analytics
```

## Key Changes

### 1. Interface Updates (`core/interfaces.go`)

Changed the collector interface to return UnifiedEvents:

```go
// Before
type Collector interface {
    Events() <-chan domain.Event
}

// After  
type Collector interface {
    Events() <-chan domain.UnifiedEvent
}
```

### 2. Event Processor Rewrite (`internal/processor.go`)

Complete rewrite of the event processor to create UnifiedEvents with:

- **Semantic Context**: Intent, category, tags, narrative, confidence
- **Entity Context**: Type, name, namespace, UID, labels, attributes
- **Impact Context**: Severity, business impact, affected services, SLO impact
- **Kubernetes Data**: Preserved all K8s-specific information

Key functions added:
- `determineSemanticIntent()`: Maps K8s events to intents
- `determineSemanticCategory()`: Classifies events into categories
- `generateSemanticTags()`: Creates correlation tags
- `calculateBusinessImpact()`: Scores business impact (0.0-1.0)
- `determineAffectedServices()`: Identifies impacted services
- `isCustomerFacing()`: Detects customer impact

### 3. Semantic Intent Mapping

Created comprehensive intent mapping for K8s resources:

#### Pod Intents
- `pod-created`: New pod added
- `pod-running`: Pod started successfully
- `pod-failed`: Pod crashed or failed
- `pod-completed`: Job pod finished
- `pod-terminated`: Pod deleted
- `pod-evicted`: OOM or resource pressure

#### Node Intents
- `node-joined`: Node added to cluster
- `node-removed`: Node left cluster
- `node-state-change`: Node condition changed

#### Service Intents
- `service-created`: New service
- `service-updated`: Service modified
- `service-removed`: Service deleted

### 4. Business Impact Calculation

Implemented intelligent impact scoring:

```go
// Base scores by severity
critical: 0.9
high: 0.7
warning: 0.4
info: 0.1

// Adjustments by resource type
+0.2 for Node issues (affect multiple workloads)
+0.1 for Service issues (affect availability)
+0.2 for kube-system namespace (critical infrastructure)
```

### 5. Category Classification

Events are classified into operational categories:
- **availability**: Service availability issues
- **reliability**: Failures and errors
- **performance**: Performance degradation
- **operations**: Normal operational events
- **resource-management**: Resource constraints

## Testing

All tests updated and passing:
- Updated `processor_test.go` to verify UnifiedEvent structure
- Fixed severity to use strings instead of constants
- Added semantic context verification

## Performance

- **Zero-conversion architecture**: No intermediate transformations
- **Direct mapping**: K8s resources → UnifiedEvent
- **Efficient processing**: Single pass through events
- **Buffered channels**: Non-blocking event delivery

## Example Output

### Pod Creation
```json
{
  "id": "evt_k8s_pod_abc123",
  "semantic": {
    "intent": "pod-created",
    "category": "operations",
    "tags": ["kubernetes", "Pod", "workload"],
    "confidence": 0.9
  },
  "impact": {
    "severity": "info",
    "businessImpact": 0.2,
    "customerFacing": true
  }
}
```

### Node Failure
```json
{
  "id": "evt_k8s_node_xyz789",
  "semantic": {
    "intent": "node-state-change",
    "category": "availability",
    "tags": ["kubernetes", "Node", "infrastructure", "critical-path"],
    "confidence": 0.95
  },
  "impact": {
    "severity": "critical",
    "businessImpact": 0.9,
    "sloImpact": true
  }
}
```

## Benefits

1. **Rich Context**: Every event has semantic meaning
2. **Better Correlation**: Intent-based event grouping
3. **Business Alignment**: Impact scores drive prioritization
4. **Zero Overhead**: Direct conversion without intermediate steps
5. **Future-Proof**: Extensible structure for new fields

## Migration Notes

For consumers of the K8s collector:

1. Update event channel type from `domain.Event` to `domain.UnifiedEvent`
2. Access severity via `event.GetSeverity()` method
3. Use semantic fields for correlation instead of raw data
4. Leverage impact scores for alerting priorities

## Verification

All changes verified with:
```bash
gofmt -l . | grep -v vendor | wc -l  # Returns 0
go build ./...                        # Success
go test ./...                         # All tests pass
```