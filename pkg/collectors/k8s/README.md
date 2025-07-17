# Kubernetes Collector

The Kubernetes collector provides comprehensive event collection from Kubernetes clusters, monitoring resources and converting them to Tapio domain events.

## Architecture

This module follows the Tapio 5-level dependency hierarchy:

```
pkg/collectors/k8s/
├── go.mod                    # Independent module
├── core/                     # Public interfaces and types
│   ├── interfaces.go         # Collector contracts
│   ├── types.go             # K8s-specific types
│   └── errors.go            # Error definitions
├── internal/                # Internal implementation
│   ├── collector.go         # Main collector logic
│   ├── processor.go         # Event processing
│   ├── watcher_base.go      # Base watcher implementation
│   ├── watcher_pod.go       # Pod-specific watcher
│   └── watchers.go          # Other resource watchers
├── cmd/                     # Standalone executables
│   └── collector/           # Test collector binary
├── testdata/                # Test fixtures
└── collector.go             # Public API exports
```

## Features

- **Multi-Resource Watching**: Pods, Nodes, Services, Deployments, Events, ConfigMaps, Secrets
- **Real-time Event Streaming**: Uses Kubernetes watch API for instant updates
- **Automatic Reconnection**: Handles API disconnections gracefully
- **Flexible Authentication**: Supports kubeconfig and in-cluster authentication
- **Namespace Filtering**: Watch specific namespace or all namespaces
- **Label/Field Selectors**: Fine-grained resource filtering
- **Event Deduplication**: Intelligent event fingerprinting

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

// Process events
for event := range collector.Events() {
    // Events are domain.Event types with KubernetesEventPayload
    fmt.Printf("K8s Event: %+v\n", event)
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

The collector converts Kubernetes API objects to domain events:

- **Resource Events**: Pod, Node, Service state changes
- **Kubernetes Events**: Warning and Normal events from the Event API
- **Lifecycle Events**: Resource creation, updates, and deletion

Each event includes:
- Original Kubernetes metadata
- Computed severity based on event type
- Resource relationships and context
- Human-readable messages

## Performance Considerations

- Uses shared informers to minimize API calls
- Configurable resync period for cache refresh
- Rate limiting to prevent overwhelming downstream systems
- Efficient event deduplication

## Security Notes

- ConfigMap and Secret watching disabled by default
- Supports RBAC with minimal required permissions
- No sensitive data logged or included in events
- Respects Kubernetes security contexts