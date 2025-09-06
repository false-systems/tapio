# Lifecycle Observer

The Lifecycle Observer monitors Kubernetes resource lifecycle events, tracking the creation, updates, and deletion of pods, services, and other Kubernetes objects.

## What It Monitors

The observer tracks Kubernetes resource lifecycle events:

- **Pod Lifecycle**: Creation, updates, termination, restarts
- **Service Changes**: Endpoints updates, service creation/deletion
- **ConfigMap/Secret Updates**: Configuration changes
- **Deployment Rollouts**: Rolling updates, scaling events
- **Node Events**: Node joins, cordons, drains
- **Namespace Operations**: Creation, deletion, quotas

## Architecture

```
┌─────────────────────────────────────────┐
│         Kubernetes API Server           │
│     (Watch API / Informers)             │
└────────────┬────────────────────────────┘
             │
             ▼
┌─────────────────────────────────────────┐
│       Lifecycle Observer                │
│   - Watches resource changes            │
│   - Tracks state transitions            │
│   - Correlates related events           │
└────────────┬────────────────────────────┘
             │
             ▼
┌─────────────────────────────────────────┐
│       Lifecycle Events                  │
│   EventType: "lifecycle.*"              │
│   Resource metadata and transitions     │
└─────────────────────────────────────────┘
```

## Event Types

- `lifecycle.pod.created` - New pod created
- `lifecycle.pod.started` - Pod containers started
- `lifecycle.pod.terminated` - Pod terminated
- `lifecycle.pod.failed` - Pod failed to start
- `lifecycle.service.created` - Service created
- `lifecycle.service.updated` - Service endpoints changed
- `lifecycle.deployment.scaled` - Deployment scaled
- `lifecycle.node.ready` - Node became ready
- `lifecycle.node.notready` - Node became not ready

## Metrics

- `lifecycle_events_processed_total` - Total lifecycle events processed
- `lifecycle_pod_transitions_total` - Pod state transitions by type
- `lifecycle_service_updates_total` - Service update events
- `lifecycle_resource_age_seconds` - Age of resources when events occur
- `lifecycle_restart_count` - Container restart counts

## Configuration

```go
config := &Config{
    KubeConfig:       "", // Path to kubeconfig (empty for in-cluster)
    ResyncPeriod:     30 * time.Second,
    EventChannelSize: 10000,
    WatchNamespaces:  []string{"default", "kube-system"},
    ResourceTypes: []string{
        "pods",
        "services", 
        "deployments",
        "configmaps",
        "secrets",
    },
}
```

## Usage

```go
observer, err := lifecycle.NewObserver(logger, config)
if err != nil {
    return err
}

// Start monitoring
if err := observer.Start(ctx); err != nil {
    return err
}

// Process events
for event := range observer.Events() {
    switch event.Type {
    case domain.EventTypeLifecyclePodCreated:
        // Handle new pod
    case domain.EventTypeLifecyclePodTerminated:
        // Handle pod termination
    }
}
```

## Features

- **State Tracking**: Maintains current state of all watched resources
- **Transition Detection**: Identifies state transitions (Pending → Running → Terminated)
- **Relationship Mapping**: Tracks relationships between resources (Pod → Service → Deployment)
- **Event Correlation**: Groups related lifecycle events together
- **Graceful Degradation**: Continues operating even if API server is temporarily unavailable

## Platform Support

- **Kubernetes**: Full support via client-go
- **Non-Kubernetes**: Mock event generation for testing

## Why "Lifecycle"?

This observer focuses on the lifecycle of Kubernetes resources - their birth, life, and death. It provides visibility into how resources change over time, which is essential for understanding cluster dynamics and troubleshooting issues.