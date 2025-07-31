# K8s Collector

Minimal K8s collector that watches Kubernetes API events and streams them as raw events with zero business logic.

## Features

- Watches core K8s resources (pods, services, nodes, events, deployments, replicasets) 
- Streams raw K8s API events without interpretation
- No event enrichment or correlation
- No business logic - all intelligence in pipeline layer

## Usage

```go
collector, err := k8s.NewCollector("k8s")
if err != nil {
    log.Fatal(err)
}

ctx := context.Background()
collector.Start(ctx)

for event := range collector.Events() {
    // Raw K8s API event data
    fmt.Printf("Event: %+v\n", event)
}
```

## Event Format

All events have type "k8s" and contain raw K8s object data:

```json
{
  "timestamp": "2023-...",
  "type": "k8s", 
  "data": {
    "api_version": "v1",
    "kind": "Pod",
    "name": "my-pod",
    "namespace": "default", 
    "uid": "...",
    "resource": "pods",
    "action": "ADDED",
    "object": { ... }
  },
  "metadata": {
    "collector": "k8s",
    "event": "api_event"
  }
}
```

This follows the minimal collector pattern - collectors only collect raw events, all intelligence belongs in the pipeline layer.