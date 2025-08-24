# Etcd API Collector

K8s resource monitoring via etcd API - focused on capturing Kubernetes resource changes through etcd watch operations.

## Overview

The etcd API collector monitors Kubernetes resources by connecting directly to etcd and watching for changes in the `/registry/` prefix. This approach provides comprehensive visibility into all K8s resource modifications without requiring cluster-level permissions.

## Architecture

```
pkg/collectors/etcd-api/
├── collector.go    # Main API collector implementation
├── config.go       # API-specific configuration
├── types.go        # K8s/API-specific types
└── README.md       # This documentation
```

## Features

- **Direct etcd Connection**: Bypasses Kubernetes API server overhead
- **Complete K8s Visibility**: Monitors all resource types in `/registry/` prefix
- **Rich Metadata**: Extracts namespace, name, kind, and operation details
- **TLS Support**: Secure connections to etcd clusters
- **Authentication**: Username/password and certificate-based auth
- **Configurable Watch Prefix**: Customizable monitoring scope
- **Resource Type Mapping**: Converts etcd paths to K8s resource kinds

## Configuration

```go
config := etcdapi.Config{
    BufferSize:   10000,
    Endpoints:    []string{"etcd1:2379", "etcd2:2379", "etcd3:2379"},
    Username:     "monitoring",
    Password:     "secret",
    WatchPrefix:  "/registry/",  // Watch all K8s resources
    DialTimeout:  5,
    TLS: &etcdapi.TLSConfig{
        CertFile: "/path/to/client.crt",
        KeyFile:  "/path/to/client.key",
        CAFile:   "/path/to/ca.crt",
    },
}
```

## Usage

```go
import "github.com/yairfalse/tapio/pkg/collectors/etcd-api"

// Create API collector
collector, err := etcdapi.NewCollector("etcd-api", config)
if err != nil {
    log.Fatal(err)
}

// Start monitoring
ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}
defer collector.Stop()

// Process K8s resource events
for event := range collector.Events() {
    // event.Data contains rich K8s metadata:
    // - operation: PUT/DELETE
    // - event_data: etcd key/value with revisions
    // - k8s_metadata: extracted namespace, name, kind
    fmt.Printf("K8s Event: %s\n", event.Data)
}
```

## Event Format

Events contain strongly-typed data with K8s metadata:

```json
{
  "operation": "PUT",
  "event_data": {
    "key": "/registry/pods/default/my-pod",
    "value": "{\"apiVersion\":\"v1\",\"kind\":\"Pod\"...}",
    "mod_revision": 123456,
    "create_revision": 123450,
    "version": 6,
    "resource_type": "pods"
  },
  "k8s_metadata": {
    "k8s_kind": "Pod",
    "k8s_namespace": "default",
    "k8s_name": "my-pod"
  }
}
```

## Monitored Resources

Automatically detects and categorizes:

- **Workloads**: Pods, Deployments, ReplicaSets, StatefulSets, DaemonSets, Jobs, CronJobs
- **Services**: Services, Endpoints, Ingresses
- **Config**: ConfigMaps, Secrets
- **Storage**: PersistentVolumes, PersistentVolumeClaims
- **Cluster**: Namespaces, Nodes, Events
- **Custom**: Any resource stored in etcd registry

## Security Considerations

- **Read-Only Access**: Only requires read permissions to etcd
- **No K8s Permissions**: Bypasses RBAC entirely by accessing etcd directly
- **TLS Encryption**: Supports mutual TLS authentication
- **Credential Management**: Secure username/password or certificate auth
- **Network Security**: Direct etcd access reduces attack surface

## Performance

- **Low Latency**: Direct etcd connection eliminates API server overhead
- **High Throughput**: Efficiently processes large volumes of K8s changes
- **Minimal Resource Usage**: Focused on etcd operations only
- **Buffer Management**: Configurable buffering prevents event loss
- **Connection Pooling**: Reuses etcd connections for efficiency

## Use Cases

1. **K8s Audit Trail**: Complete record of all resource modifications
2. **Compliance Monitoring**: Track configuration changes for compliance
3. **Security Analysis**: Detect unauthorized resource modifications
4. **Performance Analysis**: Correlate resource changes with system behavior
5. **Backup Coordination**: Trigger backups on critical resource changes

## Limitations

- **etcd Access Required**: Must have network access to etcd cluster
- **etcd Credentials**: Requires appropriate etcd authentication
- **Single Cluster**: Monitors one etcd cluster at a time
- **No Filtering**: Captures all changes (filtering done downstream)
- **Storage Overhead**: Full K8s object values captured

## Metrics

The collector exposes comprehensive OpenTelemetry metrics:

- `etcd_api_events_processed_total`: Total K8s events processed
- `etcd_api_errors_total`: Errors by type (connection, watch, parse)
- `etcd_api_processing_duration_ms`: Event processing latency
- `etcd_api_active_watches`: Number of active watch operations
- `etcd_api_api_latency_ms`: etcd API call latency

## Dependencies

- `go.etcd.io/etcd/client/v3`: etcd client library
- `go.opentelemetry.io/otel`: Observability and metrics
- `go.uber.org/zap`: Structured logging

## Health Checks

The collector provides detailed health information:

```go
health := collector.Health()
fmt.Printf("Healthy: %t, Connected: %s\n", 
    health.Healthy, 
    health.ComponentInfo["client_connected"])
```