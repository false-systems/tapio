# KubeAPI Collector

The KubeAPI collector monitors Kubernetes cluster events through the Kubernetes API, streaming them as raw events with zero business logic.

## Architecture

This collector follows the minimal collector pattern:

```
K8s API Events → KubeAPI Collector → Raw Events → Pipeline
                       ↓
                  API watching only
                  No interpretation
                  Zero business logic
```

## Features

- Watches core K8s resources: pods, services, nodes, events, deployments, replicasets, namespaces
- Real-time event streaming via K8s watch API
- Automatic reconnection handling
- Supports kubeconfig and in-cluster authentication
- Namespace filtering
- Label/field selectors

## Usage

```go
import "github.com/yairfalse/tapio/pkg/collectors/kubeapi"

// Create collector
collector, err := kubeapi.NewCollector("kubeapi")
if err != nil {
    log.Fatal(err)
}

// Start collection
ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}

// Process raw events
for event := range collector.Events() {
    // event.Type == "kubeapi"
    // event.Data contains raw K8s object JSON
    fmt.Printf("K8s Event: %+v\n", event)
}

// Stop collection
collector.Stop()
```

## Configuration

Via registry:
```yaml
collectors:
  enabled:
    - kubeapi
  kubeapi:
    namespace: "default"    # Empty for all namespaces
    kubeconfig: "/path"     # Or empty for auto-detection
```

## Event Format

All events have type "kubeapi" and contain raw K8s object data:

```json
{
  "timestamp": "2025-07-31T10:30:00Z",
  "type": "kubeapi",
  "data": {
    "api_version": "v1",
    "kind": "Pod",
    "name": "nginx-pod",
    "namespace": "default",
    "uid": "12345",
    "resource": "pods",
    "action": "ADDED",
    "object": { ... }  // Full K8s object
  },
  "metadata": {
    "collector": "kubeapi",
    "event": "api_event"
  }
}
```

## Authentication

- **Kubeconfig**: Automatically searches standard locations
- **In-Cluster**: Uses service account token when running in K8s

## Minimal Pattern

This collector follows the minimal pattern:
- No business logic
- No event enrichment
- No correlation or interpretation
- Just raw K8s API events
- All intelligence in the pipeline layer