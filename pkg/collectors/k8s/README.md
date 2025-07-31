# Kubernetes Collector - UnifiedEvent Implementation

The Kubernetes collector monitors Kubernetes cluster events and resources, converting them directly into UnifiedEvents for the Tapio observability platform with rich semantic correlation.

## Architecture

This module follows the Tapio 5-level dependency hierarchy and implements a zero-conversion architecture for optimal performance:

```
K8s API Events → K8s Collector → UnifiedEvent → Analytics Pipeline
                     ↓
              Direct conversion
              No intermediate steps
              Rich semantic context
```

### Directory Structure

```
pkg/collectors/k8s/
├── go.mod                    # Independent module
├── core/                     # Public interfaces and types
│   ├── interfaces.go         # Collector contracts (UnifiedEvent channel)
│   ├── types.go             # K8s-specific types
│   └── errors.go            # Error definitions
├── internal/                # Internal implementation
│   ├── collector.go         # Main collector logic
│   ├── processor.go         # UnifiedEvent conversion with semantic enrichment
│   ├── watcher_base.go      # Base watcher implementation
│   ├── watcher_pod.go       # Pod-specific watcher
│   └── watchers.go          # Other resource watchers
├── cmd/                     # Standalone executables
│   └── collector/           # Test collector binary
├── testdata/                # Test fixtures
└── collector.go             # Public API exports
```

## Features

### Core Capabilities
- **Multi-Resource Watching**: Pods, Nodes, Services, Deployments, Events, ConfigMaps, Secrets
- **Real-time Event Streaming**: Uses Kubernetes watch API for instant updates
- **Automatic Reconnection**: Handles API disconnections gracefully
- **Flexible Authentication**: Supports kubeconfig and in-cluster authentication
- **Namespace Filtering**: Watch specific namespace or all namespaces
- **Label/Field Selectors**: Fine-grained resource filtering
- **Event Deduplication**: Intelligent event fingerprinting

### UnifiedEvent Enhancements
- **Rich Semantic Correlation**: Every event includes intent, category, and semantic tags
- **Infrastructure Impact Assessment**: Automatic calculation of infrastructure impact scores
- **Intent Detection**: Identifies event purpose (e.g., "pod-created", "node-failed", "service-updated")
- **Category Classification**: Groups events (availability, reliability, performance, operations)
- **Affected Services Tracking**: Determines which services are impacted by events
- **System-Critical Detection**: Identifies if events affect system-critical services
- **SLO Impact Analysis**: Flags events that may affect Service Level Objectives

## Usage

```go
import "github.com/yairfalse/tapio/pkg/collectors/k8s"

// Create collector with default config
config := k8s.DefaultConfig()
collector, err := k8s.NewCollector(config)
if err != nil {
    log.Fatal(err)
}

// Start collection
ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}

// Process UnifiedEvents with rich semantic context
for event := range collector.Events() {
    // event is *domain.UnifiedEvent
    fmt.Printf("Event ID: %s\n", event.ID)
    fmt.Printf("Intent: %s\n", event.Semantic.Intent)
    fmt.Printf("Category: %s\n", event.Semantic.Category)
    fmt.Printf("Infrastructure Impact: %.2f\n", event.Impact.InfrastructureImpact)
    
    // Access K8s-specific data
    if event.Kubernetes != nil {
        fmt.Printf("K8s Action: %s\n", event.Kubernetes.Action)
        fmt.Printf("K8s Reason: %s\n", event.Kubernetes.Reason)
    }
    
    // Check if system-critical
    if event.Impact.SystemCritical {
        fmt.Printf("ALERT: System-critical service affected!\n")
    }
}

// Check health
health := collector.Health()
fmt.Printf("Connected to cluster: %v\n", health.Connected)
fmt.Printf("Cluster version: %s\n", health.ClusterInfo.Version)

// Stop collection
collector.Stop()
```

## Configuration

```go
config := k8s.Config{
    Name:            "my-k8s-collector",
    Enabled:         true,
    EventBufferSize: 2000,
    
    // Authentication
    KubeConfig: "/path/to/kubeconfig",  // Or empty for auto-detection
    InCluster:  false,                   // Set true when running in cluster
    
    // Scope
    Namespace: "default",                // Empty string for all namespaces
    
    // Resource selection
    WatchPods:        true,
    WatchNodes:       true,
    WatchServices:    true,
    WatchDeployments: true,
    WatchEvents:      true,
    WatchConfigMaps:  false,             // Disabled by default for security
    WatchSecrets:     false,             // Disabled by default for security
    
    // Filtering
    LabelSelector: "app=myapp",
    FieldSelector: "status.phase=Running",
    
    // Performance
    ResyncPeriod:   30 * time.Minute,
    EventRateLimit: 5000,
}
```

## Authentication

### Kubeconfig

The collector automatically searches for kubeconfig in standard locations:
- `$HOME/.kube/config`
- Path specified in config
- `KUBECONFIG` environment variable

### In-Cluster

When running inside a Kubernetes pod:
```go
config.InCluster = true
config.KubeConfig = ""
```

The collector will use the service account token mounted at `/var/run/secrets/kubernetes.io/serviceaccount/`.

## Building

This module can be built independently:

```bash
cd pkg/collectors/k8s
go build ./...
go test ./...
```

## Running the Standalone Collector

```bash
# Build the collector
cd cmd/collector
go build -o k8s-collector

# Run with kubeconfig
./k8s-collector -kubeconfig ~/.kube/config

# Run in-cluster
./k8s-collector -in-cluster

# Watch specific namespace
./k8s-collector -namespace production
```

## Testing

Run tests with:

```bash
go test -v ./...
```

For integration tests (requires Kubernetes access):

```bash
K8S_INTEGRATION_TESTS=1 go test -v ./...
```

## Event Processing

The collector converts Kubernetes API objects directly to UnifiedEvents with rich semantic context:

### Event Types Processed
- **Resource Events**: Pod, Node, Service state changes
- **Kubernetes Events**: Warning and Normal events from the Event API
- **Lifecycle Events**: Resource creation, updates, and deletion

### UnifiedEvent Structure

Each UnifiedEvent includes:

#### Semantic Context
- **Intent**: Purpose of the event (e.g., "pod-created", "node-failed")
- **Category**: Operational category (availability, reliability, performance)
- **Tags**: Correlation tags for cross-layer analysis
- **Narrative**: Human-readable description
- **Confidence**: Confidence score in semantic classification

#### Entity Context
- **Type**: Resource type (Pod, Node, Service, etc.)
- **Name**: Resource name
- **Namespace**: Kubernetes namespace
- **UID**: Unique identifier
- **Labels**: Resource labels
- **Attributes**: Additional metadata

#### Impact Context
- **Severity**: info, warning, high, critical
- **BusinessImpact**: Score from 0.0 to 1.0
- **AffectedServices**: List of impacted services
- **CustomerFacing**: Boolean flag
- **SLOImpact**: Boolean flag for SLO violations

#### Kubernetes Data
- **EventType**: Normal or Warning
- **Reason**: K8s event reason
- **Message**: Detailed message
- **Action**: ADDED, MODIFIED, DELETED, ERROR
- **APIVersion**: Resource API version
- **ResourceVersion**: K8s resource version

## Event Examples

### Pod Creation Event
```json
{
  "id": "evt_k8s_pod_abc123",
  "type": "kubernetes",
  "source": "k8s",
  "timestamp": "2025-07-20T10:30:00Z",
  "semantic": {
    "intent": "pod-created",
    "category": "operations",
    "tags": ["kubernetes", "Pod", "workload", "container"],
    "narrative": "Kubernetes Pod event: Pod api-server-7d8f9: Running",
    "confidence": 0.9
  },
  "entity": {
    "type": "Pod",
    "name": "api-server-7d8f9",
    "namespace": "production",
    "uid": "12345-67890",
    "labels": {
      "app": "api-server",
      "version": "v2.1.0"
    }
  },
  "kubernetes": {
    "eventType": "Normal",
    "reason": "Running",
    "action": "ADDED",
    "message": "Pod api-server-7d8f9: Running",
    "objectKind": "Pod"
  },
  "impact": {
    "severity": "info",
    "infrastructureImpact": 0.2,
    "affectedServices": ["production-workload"],
    "systemCritical": true,
    "sloImpact": false
  }
}
```

### Node Failure Event
```json
{
  "id": "evt_k8s_node_xyz789",
  "type": "system",
  "source": "k8s",
  "timestamp": "2025-07-20T10:31:00Z",
  "semantic": {
    "intent": "node-state-change",
    "category": "availability",
    "tags": ["kubernetes", "Node", "infrastructure", "cluster", "error", "critical-path"],
    "narrative": "Kubernetes Node event: Node worker-3 is not ready: KubeletNotReady",
    "confidence": 0.95
  },
  "entity": {
    "type": "Node",
    "name": "worker-3",
    "attributes": {
      "api_version": "v1",
      "event_type": "MODIFIED"
    }
  },
  "kubernetes": {
    "eventType": "Warning",
    "reason": "NotReady",
    "action": "MODIFIED",
    "message": "Node worker-3 is not ready: KubeletNotReady",
    "objectKind": "Node"
  },
  "impact": {
    "severity": "critical",
    "infrastructureImpact": 0.9,
    "affectedServices": ["cluster-scheduler", "node-management"],
    "systemCritical": false,
    "sloImpact": true
  }
}
```

### Pod OOMKilled Event
```json
{
  "id": "evt_k8s_oom_def456",
  "type": "kubernetes",
  "source": "k8s",
  "timestamp": "2025-07-20T10:32:00Z",
  "semantic": {
    "intent": "pod-evicted",
    "category": "resource-management",
    "tags": ["kubernetes", "Event", "observability", "audit"],
    "narrative": "Kubernetes Event: Container exceeded memory limit",
    "confidence": 0.95
  },
  "entity": {
    "type": "Event",
    "name": "api-server-7d8f9.oomkilled",
    "namespace": "production"
  },
  "kubernetes": {
    "eventType": "Warning",
    "reason": "OOMKilled",
    "message": "Container api-server exceeded memory limit (2Gi)",
    "object": "Pod/api-server-7d8f9",
    "objectKind": "Pod"
  },
  "impact": {
    "severity": "high",
    "infrastructureImpact": 0.8,
    "affectedServices": ["production-workload"],
    "systemCritical": true,
    "sloImpact": true
  }
}
```

## Semantic Intent Mapping

The processor maps K8s events to semantic intents:

### Pod Intents
- `pod-created`: Pod added to cluster
- `pod-running`: Pod transitioned to running state
- `pod-failed`: Pod failed to start or crashed
- `pod-completed`: Pod completed successfully
- `pod-terminated`: Pod was deleted
- `pod-evicted`: Pod was evicted (OOM, node pressure)
- `pod-state-change`: Generic pod state transition

### Node Intents
- `node-joined`: Node added to cluster
- `node-removed`: Node removed from cluster
- `node-state-change`: Node condition changed

### Service Intents
- `service-created`: Service created
- `service-updated`: Service configuration changed
- `service-removed`: Service deleted

### Operation Intents
- `operation-failed`: Generic operation failure
- `backoff-restart`: Container in crash loop backoff
- `k8s-event`: Generic Kubernetes event

## Performance Considerations

- **Zero-conversion architecture**: Direct K8s→UnifiedEvent transformation
- **Efficient resource watching**: Uses K8s watch API with shared informers
- **Configurable resync period**: For cache refresh
- **Rate limiting**: Prevents overwhelming downstream systems
- **Event deduplication**: Intelligent fingerprinting
- **Buffered channels**: Non-blocking event processing

## Security Notes

- ConfigMap and Secret watching disabled by default
- Supports RBAC with minimal required permissions
- No sensitive data logged or included in events
- Respects Kubernetes security contexts

## Required RBAC Permissions

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: tapio-k8s-collector
rules:
- apiGroups: [""]
  resources: ["pods", "nodes", "services", "events"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["get", "list", "watch"]
```

## Troubleshooting

### Common Issues

1. **No events received**
   - Check RBAC permissions
   - Verify kubeconfig or service account
   - Check namespace access

2. **High memory usage**
   - Reduce event buffer size
   - Enable specific resource types only
   - Increase resync period

3. **Connection errors**
   - Verify cluster connectivity
   - Check firewall rules
   - Validate authentication

### Debug Mode

Enable debug logging:
```bash
export K8S_COLLECTOR_DEBUG=true
./k8s-collector
```

## Future Enhancements

- [ ] Custom Resource Definition (CRD) support
- [ ] Advanced label-based correlation
- [ ] Predictive failure detection
- [ ] Cost impact calculation
- [ ] Multi-cluster federation
- [ ] Webhook admission event capture