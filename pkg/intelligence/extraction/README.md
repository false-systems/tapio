# K8s Context Extraction

This package implements comprehensive Kubernetes context extraction for the Tapio platform, enabling multi-dimensional correlation based on K8s native structure.

## Overview

The K8s context extraction enriches events with deep Kubernetes metadata, enabling automatic correlation through:
- Ownership relationships (Pod → ReplicaSet → Deployment)
- Label selectors (Service → Pods)
- Node topology (Pods on same node)
- Resource dependencies (ConfigMaps, Secrets, PVCs)

## Components

### K8sContextExtractor
The main extraction engine that:
- Enriches events with K8s context based on various identifiers
- Supports three extraction depths: Shallow, Medium, Deep
- Automatically determines extraction depth based on event criticality
- Maintains performance metrics

### K8sCache
High-performance cache using K8s informers with custom indexes:
- Container ID index for eBPF events
- Pod IP index for network events
- Fast lookups without API calls

### K8sExtractionStage
Pipeline stage implementing the ProcessingStage interface for integration with the Tapio intelligence pipeline.

## Usage

### Direct Usage
```go
// Create extractor
k8sClient := kubernetes.NewForConfigOrDie(config)
logger := zap.NewNop()
extractor, err := NewK8sContextExtractor(k8sClient, logger)

// Process event
event := &domain.UnifiedEvent{
    Kernel: &domain.KernelData{
        ContainerID: "docker://abc123",
    },
}
err = extractor.Process(ctx, event)

// Event now has rich K8s context
fmt.Printf("Pod: %s/%s\n", event.K8sContext.Namespace, event.K8sContext.Name)
fmt.Printf("Workload: %s/%s\n", event.K8sContext.WorkloadKind, event.K8sContext.WorkloadName)
```

### Pipeline Integration
```go
// Create pipeline with K8s extraction
pipeline, err := CreateK8sEnrichedPipeline(k8sClient, logger)

// Or add to existing pipeline
stage, err := NewK8sExtractionStage(k8sClient, logger)
builder.AddStage(stage)
```

## Extraction Depths

### Shallow (Default)
- Basic identity (name, namespace, UID)
- Labels and annotations
- Node placement

### Medium (Business Impact > 0.7)
- Everything from Shallow
- Ownership references
- Topology (services, nodes)
- Resource specifications

### Deep (Critical/Error Events)
- Everything from Medium
- Dependencies (ConfigMaps, Secrets, PVCs)
- State and conditions
- Operational context

## Multi-Dimensional Correlation

The extracted K8s context enables automatic correlation across multiple dimensions:

### 1. Ownership Dimension
```
Pod (OOMKilled) → ReplicaSet (scaling) → Deployment
```

### 2. Spatial Dimension
```
Node (memory pressure) → All Pods on node
```

### 3. Dependency Dimension
```
ConfigMap (updated) → All Pods using it
```

### 4. Semantic Dimension
```
Service (endpoints not ready) → Matching Pods (by selector)
```

## Performance

- Cache-based lookups avoid API calls
- Custom indexes for O(1) lookups by container ID or IP
- Extraction depth based on event criticality
- Metrics tracking for monitoring

## Example: Automatic Correlation

Given these events:
1. Pod OOMKilled (owner: ReplicaSet-123)
2. ReplicaSet-123 scaling up (owner: Deployment-frontend)
3. Service-frontend endpoints not ready (selector: app=frontend)
4. Node-1 memory pressure

The K8s context automatically reveals:
- Events 1-3 are related through ownership chain
- Event 3 correlates to Event 1 through label selectors
- Event 4 affects Event 1 through node topology

This enables Tapio to tell the complete story without manual correlation rules.