# OTEL Attribute Standards for Tapio/Ahti Integration

**Status**: Design Document
**Date**: 2025-10-14
**Author**: Architecture Team
**Context**: Ensuring OTEL ecosystem compatibility for Ahti's OTEL plugin

---

## Problem Statement

Ahti will have an **OTEL plugin** that exports Tapio's correlation results to OTLP-compatible backends (Grafana, Prometheus, Datadog, etc.). These backends expect standard OTEL attribute names for K8s resources.

**Current state:**
- Tapio's `Entity.Attributes` is `map[string]string` with no naming standards
- Enrichment Service (not yet built) will populate these attributes
- Ahti OTEL plugin needs predictable attribute names for export

**Goal:**
- Adopt OTEL K8s naming standards for `Entity.Attributes`
- Enable drop-in compatibility with OTEL ecosystem
- Avoid translation/mapping layer in Ahti

---

## Design Principles

### What We're Adopting

**OTEL naming standards for resource attributes** (NOT their metrics):
- Standard field names: `k8s.pod.name`, `k8s.namespace.name`, etc.
- Standard enum values: `"running"`, `"terminated"` (not custom values)
- Namespacing convention: `k8s.*` for Kubernetes attributes

### What We're NOT Adopting

- OTEL metric names (`k8s.pod.cpu.usage`) - we don't collect resource metrics
- OTEL metric conventions - Tapio collects events, not metrics
- Their terminology abuse - we call them "naming standards" not "semantics"

### Philosophical Note

OTEL calls these "semantic conventions" but they're just **naming standards** (vocabulary/lexicon agreement, not actual semantics). Tapio's graph model (Entities/Relationships) represents actual semantic relations.

From Saussure: OTEL standardizes the signifier, we define the signified and the structural relations.

---

## Architecture

### Data Flow

```
┌────────────────────────────────────────────────────────────┐
│ Tapio: Enrichment Service                                  │
│                                                            │
│ ObserverEvent → Query K8sContext → Create TapioEvent      │
│                                    with OTEL attributes    │
└─────────────────────┬──────────────────────────────────────┘
                      ↓
              NATS JetStream
                      ↓
┌─────────────────────┴──────────────────────────────────────┐
│ Ahti: Correlation Engine                                   │
│                                                            │
│ TapioEvent → Correlate → Results with OTEL attributes     │
└─────────────────────┬──────────────────────────────────────┘
                      ↓
              ┌───────┴────────┐
              │  OTEL Plugin   │
              └───────┬────────┘
                      ↓
       ┌──────────────┼──────────────┐
       ↓              ↓               ↓
   Grafana      Prometheus       Datadog
   (expects     (expects         (expects
    OTEL attrs) OTEL attrs)      OTEL attrs)
```

### Why This Matters

**Without OTEL standards:**
```go
// Tapio creates custom names
Entity{Attributes: {"pod": "frontend", "ns": "prod"}}
// Ahti must translate
attributes["k8s.pod.name"] = entity.Attributes["pod"]  // ❌ Mapping layer
```

**With OTEL standards:**
```go
// Tapio uses OTEL names
Entity{Attributes: {"k8s.pod.name": "frontend", "k8s.namespace.name": "prod"}}
// Ahti exports directly
exportOTLP(entity.Attributes)  // ✅ No translation
```

---

## OTEL Attribute Standards

### Reference Document

Source: https://opentelemetry.io/docs/specs/semconv/system/k8s-metrics/

### Core K8s Resource Attributes

| Attribute Name | Type | Description | Example |
|----------------|------|-------------|---------|
| `k8s.namespace.name` | string | Namespace name | `"production"` |
| `k8s.node.name` | string | Node name | `"node-1"` |
| `k8s.pod.name` | string | Pod name | `"frontend-7d8f9-xyz"` |
| `k8s.pod.uid` | string | Pod UID | `"abc-123-def"` |
| `k8s.container.name` | string | Container name | `"nginx"` |
| `k8s.container.status.state` | string | Container state | `"running"`, `"terminated"`, `"waiting"` |
| `k8s.volume.name` | string | Volume name | `"data-volume"` |
| `k8s.volume.type` | string | Volume type | `"persistentVolumeClaim"`, `"emptyDir"`, `"configMap"` |

### Standard Enum Values

**Container States** (`k8s.container.status.state`):
- `"running"` - Container is running
- `"terminated"` - Container has terminated
- `"waiting"` - Container is waiting to start

**Volume Types** (`k8s.volume.type`):
- `"configMap"`
- `"downwardAPI"`
- `"emptyDir"`
- `"local"`
- `"persistentVolumeClaim"`
- `"secret"`

---

## Implementation Plan

### Phase 1: Update Entity Attribute Population (Enrichment Service)

**Location:** `internal/services/enrichment/` (not yet created)

**When creating Pod entities:**
```go
func enrichPodEntity(pod *v1.Pod, clusterID string) domain.Entity {
    return domain.Entity{
        Type:      domain.EntityTypePod,
        ID:        string(pod.UID),
        Name:      pod.Name,
        ClusterID: clusterID,
        Namespace: pod.Namespace,
        Labels:    pod.Labels,
        Attributes: map[string]string{
            // ✅ OTEL standard attributes
            "k8s.pod.uid":        string(pod.UID),
            "k8s.pod.name":       pod.Name,
            "k8s.namespace.name": pod.Namespace,
            "k8s.node.name":      pod.Spec.NodeName,
        },
    }
}
```

**When creating Container entities:**
```go
func enrichContainerEntity(container *v1.Container, status v1.ContainerStatus) domain.Entity {
    state := "unknown"
    if status.State.Running != nil {
        state = "running"
    } else if status.State.Terminated != nil {
        state = "terminated"
    } else if status.State.Waiting != nil {
        state = "waiting"
    }

    return domain.Entity{
        Type: domain.EntityTypeContainer,
        ID:   status.ContainerID,
        Name: container.Name,
        Attributes: map[string]string{
            // ✅ OTEL standard attributes
            "k8s.container.name":         container.Name,
            "k8s.container.status.state": state,  // Standard enum value
            "k8s.container.image":        container.Image,
        },
    }
}
```

**When creating Volume entities:**
```go
func enrichVolumeEntity(volume v1.Volume, namespace string) domain.Entity {
    volumeType := getVolumeType(volume)  // Returns OTEL standard type

    return domain.Entity{
        Type:      domain.EntityTypePVC,
        ID:        fmt.Sprintf("%s/%s", namespace, volume.Name),
        Name:      volume.Name,
        Namespace: namespace,
        Attributes: map[string]string{
            // ✅ OTEL standard attributes
            "k8s.volume.name": volume.Name,
            "k8s.volume.type": volumeType,  // "persistentVolumeClaim", "emptyDir", etc.
        },
    }
}

func getVolumeType(volume v1.Volume) string {
    // Map K8s volume source to OTEL standard type
    switch {
    case volume.PersistentVolumeClaim != nil:
        return "persistentVolumeClaim"
    case volume.EmptyDir != nil:
        return "emptyDir"
    case volume.ConfigMap != nil:
        return "configMap"
    case volume.Secret != nil:
        return "secret"
    case volume.DownwardAPI != nil:
        return "downwardAPI"
    case volume.HostPath != nil:
        return "local"
    default:
        return "unknown"
    }
}
```

### Phase 2: Ahti OTEL Plugin (Direct Export)

**Location:** `ahti/internal/plugins/otel/` (future)

**Export without translation:**
```go
func (p *OTELPlugin) ExportCorrelation(result CorrelationResult) error {
    // Convert TapioEvent entities to OTEL resource attributes
    for _, entity := range result.Entities {
        resource := otelResource.NewWithAttributes(
            semconv.SchemaURL,
            // ✅ Entity.Attributes already has OTEL standard names
            convertToOTELAttributes(entity.Attributes)...,
        )

        // Export to OTLP backend
        return p.exporter.Export(ctx, resource)
    }
}

func convertToOTELAttributes(attrs map[string]string) []attribute.KeyValue {
    result := make([]attribute.KeyValue, 0, len(attrs))
    for k, v := range attrs {
        // No translation needed - already OTEL standard names
        result = append(result, attribute.String(k, v))
    }
    return result
}
```

### Phase 3: Validation & Testing

**Unit tests for attribute population:**
```go
func TestEnrichPodEntity_OTELAttributes(t *testing.T) {
    pod := &v1.Pod{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "frontend-abc",
            Namespace: "production",
            UID:       "uid-123",
        },
        Spec: v1.PodSpec{
            NodeName: "node-1",
        },
    }

    entity := enrichPodEntity(pod, "cluster-1")

    // Verify OTEL standard attribute names
    assert.Equal(t, "frontend-abc", entity.Attributes["k8s.pod.name"])
    assert.Equal(t, "production", entity.Attributes["k8s.namespace.name"])
    assert.Equal(t, "uid-123", entity.Attributes["k8s.pod.uid"])
    assert.Equal(t, "node-1", entity.Attributes["k8s.node.name"])
}

func TestEnrichContainerEntity_OTELStates(t *testing.T) {
    tests := []struct {
        state    v1.ContainerState
        expected string
    }{
        {v1.ContainerState{Running: &v1.ContainerStateRunning{}}, "running"},
        {v1.ContainerState{Terminated: &v1.ContainerStateTerminated{}}, "terminated"},
        {v1.ContainerState{Waiting: &v1.ContainerStateWaiting{}}, "waiting"},
    }

    for _, tt := range tests {
        status := v1.ContainerStatus{State: tt.state}
        entity := enrichContainerEntity(&v1.Container{Name: "test"}, status)

        // Verify OTEL standard state value
        assert.Equal(t, tt.expected, entity.Attributes["k8s.container.status.state"])
    }
}
```

**Integration test with Ahti export:**
```go
func TestOTELPlugin_ExportWithStandardAttributes(t *testing.T) {
    // Create TapioEvent with OTEL-standard attributes
    event := domain.TapioEvent{
        Entities: []domain.Entity{
            {
                Type: domain.EntityTypePod,
                Attributes: map[string]string{
                    "k8s.pod.name":       "frontend",
                    "k8s.namespace.name": "prod",
                },
            },
        },
    }

    // Export via OTEL plugin
    plugin := NewOTELPlugin(mockExporter)
    err := plugin.Export(event)
    require.NoError(t, err)

    // Verify exported attributes match OTEL standards
    exported := mockExporter.GetLastExport()
    assert.Equal(t, "frontend", exported.Attributes["k8s.pod.name"])
}
```

---

## Entity Type Mapping

### Complete Attribute Mapping

| Entity Type | OTEL Attributes | Notes |
|-------------|-----------------|-------|
| **Pod** | `k8s.pod.name`<br>`k8s.pod.uid`<br>`k8s.namespace.name`<br>`k8s.node.name` | Core pod identification |
| **Container** | `k8s.container.name`<br>`k8s.container.status.state`<br>`k8s.container.image` | Include state enum |
| **Node** | `k8s.node.name`<br>`k8s.node.uid` | Node identification |
| **Deployment** | `k8s.deployment.name`<br>`k8s.namespace.name` | Workload controller |
| **Service** | `k8s.service.name`<br>`k8s.namespace.name`<br>`k8s.service.type` | Service discovery |
| **PVC** | `k8s.volume.name`<br>`k8s.volume.type`<br>`k8s.namespace.name` | Storage resources |
| **Namespace** | `k8s.namespace.name` | Cluster organization |

### Extended Attributes (Optional)

These are not in OTEL K8s standards but commonly used:

| Attribute | Description | Example |
|-----------|-------------|---------|
| `k8s.cluster.name` | Multi-cluster support | `"prod-us-west"` |
| `k8s.deployment.uid` | Deployment UID | `"deploy-123"` |
| `k8s.service.type` | Service type | `"ClusterIP"`, `"LoadBalancer"` |
| `k8s.replicaset.name` | ReplicaSet name | `"frontend-7d8f9"` |

**Decision:** Include these for completeness, but prioritize core OTEL standards first.

---

## Verification Checklist

Before marking this design complete:

- [ ] Enrichment Service implements OTEL attribute population
- [ ] All 12 entity types have OTEL attribute mappings
- [ ] Enum values match OTEL standards exactly
- [ ] Unit tests verify attribute names and values
- [ ] Integration test with Ahti OTEL plugin export
- [ ] Documentation updated with examples
- [ ] `make verify-full` passes

---

## Migration Notes

### Existing Code Impact

**Currently:**
- `Entity.Attributes` field exists but is unpopulated (events.go:101)
- No Enrichment Service yet (future work)
- K8sContext service exists (internal/services/k8scontext/)

**This design affects:**
1. **Enrichment Service** (not yet built) - primary implementation site
2. **Ahti OTEL Plugin** (not yet built) - consumes these attributes
3. **No breaking changes** - Entity structure unchanged, just population standards

### Backward Compatibility

**No compatibility issues** - this is net new functionality:
- `Entity.Attributes` field already exists
- Currently unpopulated (empty map)
- Adding OTEL-standard keys is additive

---

## References

### OTEL Documentation
- [K8s Metrics Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/system/k8s-metrics/)
- [Resource Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/resource/)

### Tapio Documentation
- `pkg/domain/events.go` - Entity/TapioEvent definitions
- `docs/002-tapio-observer-consolidation.md` - Observer architecture
- `CLAUDE.md` - Production standards

### Related Design Docs
- K8sContext Service: `internal/services/k8scontext/`
- Network Observer: `internal/observers/network/DESIGN.md`
- Enrichment Service: TBD (future design doc)

---

## Open Questions

1. **Should we validate attribute names?**
   - Option A: Validate against OTEL standard list (strict)
   - Option B: Allow any `k8s.*` attributes (flexible)
   - **Decision:** Start with Option A (strict validation), relax if needed

2. **How to handle non-K8s entities?**
   - Network entities (IP, port) don't have OTEL standards
   - **Decision:** Use `tapio.*` namespace for custom attributes

3. **Should we version these conventions?**
   - OTEL standards evolve over time
   - **Decision:** Document OTEL spec version used (currently 1.x)

---

## Success Criteria

This design is successful when:

1. ✅ Ahti OTEL plugin exports without attribute translation
2. ✅ Grafana dashboards work with standard queries (`k8s.pod.name`)
3. ✅ Prometheus/Datadog recognize Tapio's exported data
4. ✅ Zero custom attribute mapping in Ahti
5. ✅ All 12 entity types have complete OTEL attribute mappings

---

## Appendix: Example TapioEvent

**Complete example with OTEL attributes:**

```go
tapioEvent := domain.TapioEvent{
    ID:        "evt-123",
    Type:      domain.EventTypeNetwork,
    Subtype:   "tcp_connection",
    Severity:  domain.SeverityInfo,
    Outcome:   domain.OutcomeSuccess,
    Timestamp: time.Now(),

    Entities: []domain.Entity{
        {
            Type:      domain.EntityTypePod,
            ID:        "pod-uid-123",
            Name:      "frontend-7d8f9-xyz",
            ClusterID: "prod-cluster",
            Namespace: "production",
            Labels: map[string]string{
                "app":     "frontend",
                "version": "v2.1",
            },
            Attributes: map[string]string{
                // ✅ OTEL standard attributes
                "k8s.pod.uid":        "pod-uid-123",
                "k8s.pod.name":       "frontend-7d8f9-xyz",
                "k8s.namespace.name": "production",
                "k8s.node.name":      "node-1",
            },
        },
        {
            Type: domain.EntityTypeContainer,
            ID:   "container-id-456",
            Name: "nginx",
            Attributes: map[string]string{
                // ✅ OTEL standard attributes
                "k8s.container.name":         "nginx",
                "k8s.container.status.state": "running",
                "k8s.container.image":        "nginx:1.21",
            },
        },
        {
            Type: domain.EntityTypeService,
            ID:   "svc-uid-789",
            Name: "backend-service",
            Attributes: map[string]string{
                // ✅ OTEL standard attributes
                "k8s.service.name":   "backend-service",
                "k8s.namespace.name": "production",
                // Extended attribute
                "k8s.service.type": "ClusterIP",
            },
        },
    },

    Relationships: []domain.Relationship{
        {
            Type:   domain.RelationshipContains,
            Source: domain.Entity{Type: domain.EntityTypePod, ID: "pod-uid-123"},
            Target: domain.Entity{Type: domain.EntityTypeContainer, ID: "container-id-456"},
        },
        {
            Type:   domain.RelationshipConnectsTo,
            Source: domain.Entity{Type: domain.EntityTypePod, ID: "pod-uid-123"},
            Target: domain.Entity{Type: domain.EntityTypeService, ID: "svc-uid-789"},
        },
    },

    NetworkData: &domain.NetworkEventData{
        Protocol: "TCP",
        SrcIP:    "10.0.1.5",
        DstIP:    "10.0.2.10",
        SrcPort:  45678,
        DstPort:  8080,
    },
}
```

When Ahti exports this via OTEL plugin, backends will understand:
- Pod name: `k8s.pod.name = "frontend-7d8f9-xyz"`
- Namespace: `k8s.namespace.name = "production"`
- Container state: `k8s.container.status.state = "running"`

No translation needed. Drop-in OTEL compatibility. ✅
