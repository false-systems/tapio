# Deployments Observer

## Overview

The Deployments Observer tracks Kubernetes deployment-related changes including Deployments, ConfigMaps, and Secrets. It provides rich correlation context for the correlation engine to identify relationships between configuration changes and system behavior.

## Features

### Resource Monitoring
- **Deployments**: Create, update, delete, scale, rollback detection
- **ConfigMaps**: Data changes with key-level tracking
- **Secrets**: Change detection (values not logged)
- **Services**: Related service discovery for topology mapping

### Change Detection

The observer detects and classifies meaningful changes:

#### Change Types
- **Scale**: Replica count changes
- **Image**: Container image updates
- **Resource**: CPU/memory limit changes
- **Strategy**: Deployment strategy changes (RollingUpdate ↔ Recreate)
- **Config**: Configuration data changes

#### Impact Classification
- **High**: Image changes, strategy changes, resource limits
- **Medium**: Scale changes, config updates
- **Low**: Label/annotation changes

#### Restart Detection
Automatically identifies changes that require pod restarts:
- Image updates
- Resource limit changes
- Config changes (via ConfigMap/Secret)

### Correlation Intelligence

Events are enriched with metadata for correlation engine:

```go
event.Metadata.Labels = {
    "change_type": "image",           // Primary change type
    "impact": "high",                  // Impact level
    "requires_restart": "true",        // Pod restart needed
    "related_event_types": "[...]",    // Events to correlate with
}
```

**Related Event Types by Change:**
- **Image changes** → `container.oom`, `container.restart`, `container.exit`, `network.connection`
- **Scale changes** → `container.create`, `network.connection`, `memory.allocation`
- **Config changes** → `container.restart`, `k8s.configmap`
- **Resource changes** → `container.oom`, `memory.allocation`

### Correlation Context

Each event includes rich correlation context:

```go
type CorrelationContext struct {
    Deployment DeploymentContext   // Name, namespace, labels, replicas, strategy
    Containers []ContainerContext  // Image, ports, env vars, volume mounts
    Volumes    []VolumeContext     // ConfigMaps, Secrets, PVCs
    Services   []ServiceContext    // Related services (auto-discovered)
    Owners     []OwnerContext      // Owner references
}
```

**Container Context includes:**
- Image and name
- Exposed ports
- Environment variables with ConfigMap/Secret references
- Volume mount paths

**Volume Context tracks:**
- ConfigMap volumes (with names for correlation)
- Secret volumes (for change tracking)
- PVC volumes (for storage correlation)

## Architecture

### Components

```
┌─────────────────────────────────────────────────────┐
│              Deployments Observer                    │
├─────────────────────────────────────────────────────┤
│                                                      │
│  ┌──────────────┐      ┌──────────────┐           │
│  │ K8s Informers│─────▶│ Event Handler│           │
│  │ - Deployments│      │ - Add        │           │
│  │ - ConfigMaps │      │ - Update     │           │
│  │ - Secrets    │      │ - Delete     │           │
│  └──────────────┘      └───────┬──────┘           │
│                                 │                   │
│                        ┌────────▼─────────┐        │
│                        │ Change Detection │        │
│                        │ - detectChanges()│        │
│                        │ - Impact level   │        │
│                        │ - Restart check  │        │
│                        └────────┬─────────┘        │
│                                 │                   │
│                        ┌────────▼─────────┐        │
│                        │  Context Gather  │        │
│                        │  - Containers    │        │
│                        │  - Volumes       │        │
│                        │  - Services      │        │
│                        └────────┬─────────┘        │
│                                 │                   │
│                        ┌────────▼─────────┐        │
│                        │  Event Enrichment│        │
│                        │  - Change labels │        │
│                        │  - Correlation   │        │
│                        └────────┬─────────┘        │
│                                 │                   │
└─────────────────────────────────┼───────────────────┘
                                  │
                                  ▼
                    ┌──────────────────────────┐
                    │  Correlation Engine      │
                    └──────────────────────────┘
```

### File Structure

```
deployments/
├── observer.go              # Main observer implementation
├── changes.go               # Change detection logic
├── changes_test.go          # Change detection tests
├── config.go                # Configuration
├── observer_*_test.go       # Various test suites
└── README.md                # This file
```

## Usage

### Configuration

```go
config := &Config{
    Name:               "deployments",
    BufferSize:         1000,
    TrackConfigMaps:    true,
    TrackSecrets:       true,
    Namespaces:         []string{"production", "staging"},
    ResyncPeriod:       30 * time.Second,
    DeduplicationWindow: 5 * time.Second,
}

observer, err := NewObserver("deployments", config)
if err != nil {
    log.Fatal(err)
}
```

### Starting the Observer

```go
ctx := context.Background()
if err := observer.Start(ctx); err != nil {
    log.Fatal(err)
}

// Consume events
for event := range observer.Events() {
    // Process event
    // Correlation engine will analyze change context
}
```

### Filtering

```yaml
# Only track specific namespaces
namespaces:
  - production
  - staging

# Only track resources with annotation
annotation_filter: "tapio.io/track"

# Ignore system deployments
ignore_system_deployments: true
```

## Change Detection Examples

### Image Update

```
Old: nginx:1.21
New: nginx:1.22

Change:
  Type: image
  Impact: high
  RequiresRestart: true
  RelatedTypes: [container.oom, container.restart, container.exit]
```

### Scale Up

```
Old: replicas: 3
New: replicas: 5

Change:
  Type: scale
  Impact: medium
  RequiresRestart: false
  RelatedTypes: [container.create, network.connection]
```

### ConfigMap Change

```
Old: cache_ttl: 300
New: cache_ttl: 60

Change:
  Type: config
  Impact: medium
  RequiresRestart: true
  RelatedTypes: [container.restart, k8s.configmap]
```

## Correlation Scenarios

### Scenario 1: Bad Deploy Detection

```
Timeline:
15:00:00 - Deployment image updated (nginx:1.21 → nginx:1.22)
           Labels: {change_type: image, impact: high, requires_restart: true}

15:00:30 - Pods restarting (tracked by lifecycle observer)

15:01:00 - OOM kills detected (tracked by container-runtime observer)

15:01:30 - Error rate spike (tracked by network observer)

Correlation Result:
  "Image update at 15:00 caused OOM kills and errors"
  Confidence: 0.95
  Recommendation: "Rollback to nginx:1.21"
```

### Scenario 2: Config Change Impact

```
Timeline:
14:00:00 - ConfigMap "cache-config" updated
           Key changed: cache_enabled: true → false

14:00:15 - Deployment detects config hash change

14:00:20 - Pods restart with new config

14:00:40 - Database connections spike (cache disabled)

14:01:00 - Database timeouts begin

Correlation Result:
  "ConfigMap change disabled caching, causing DB overload"
  Confidence: 0.92
  Recommendation: "Revert ConfigMap change"
```

### Scenario 3: Scale Event Tracking

```
Timeline:
10:00:00 - HPA scales deployment 3 → 10 replicas

10:00:15 - 7 new pods created

10:00:30 - Network connections increase

10:00:45 - Memory allocation spike

Correlation Result:
  "Scale-up event caused expected resource increase"
  Confidence: 1.0
  Assessment: "Normal behavior"
```

## Testing

### Run Tests

```bash
# All tests
go test ./internal/observers/deployments -v

# Change detection tests only
go test ./internal/observers/deployments -run TestDetectChanges -v

# Coverage report
go test ./internal/observers/deployments -cover
```

### Test Coverage

- Change detection: >90%
- Event handling: >85%
- Integration scenarios: >80%

## Standards Compliance

✅ **Zero `map[string]interface{}`** - All typed structs
✅ **No TODOs or stubs** - Complete implementation
✅ **>80% test coverage** - Comprehensive tests
✅ **Direct OpenTelemetry** - No custom wrappers
✅ **Proper error handling** - All errors wrapped with context
✅ **Resource cleanup** - Proper defer usage

## Metrics

The observer exposes these OTEL metrics:

```
deployments_deployments_tracked_total    # Deployment changes tracked
deployments_config_changes_total         # ConfigMap/Secret changes
deployments_rollbacks_total              # Rollbacks detected
deployments_events_processed_total       # Total events processed
deployments_events_dropped_total         # Events dropped (buffer full)
deployments_processing_time_ms           # Event processing time
```

## Future Enhancements

Potential additions (not implemented):

- [ ] Helm release tracking
- [ ] Kustomize overlay detection
- [ ] Deployment health scoring
- [ ] Change velocity metrics
- [ ] Multi-cluster correlation
- [ ] GitOps source tracking

## See Also

- [Base Observer](../base/README.md) - Shared observer functionality
- [Correlation Engine](../../intelligence/README.md) - Event correlation
- [Container Runtime Observer](../container-runtime/README.md) - Container events
- [Network Observer](../network/README.md) - Network events