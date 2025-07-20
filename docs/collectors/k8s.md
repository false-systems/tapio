# Kubernetes Collector

The Kubernetes Collector monitors and processes events from the Kubernetes API server, providing real-time insights into cluster resources and their state changes.

## Table of Contents

- [Architecture](#architecture)
- [Installation](#installation)
- [Configuration](#configuration)
- [Event Processing](#event-processing)
- [Resource Watchers](#resource-watchers)
- [Health Monitoring](#health-monitoring)
- [Security](#security)
- [Performance Tuning](#performance-tuning)
- [Troubleshooting](#troubleshooting)

## Architecture

The Kubernetes collector follows a modular architecture with clear separation of concerns:

```
┌─────────────────────────────────────────────────────┐
│                 Kubernetes API Server               │
└────────────────────────┬────────────────────────────┘
                         │ Watch API
┌────────────────────────▼────────────────────────────┐
│                 Resource Watchers                   │
│  ┌────────┐ ┌────────┐ ┌─────────┐ ┌────────┐     │
│  │  Pods  │ │ Nodes  │ │Services │ │ Events │ ... │
│  └────┬───┘ └───┬────┘ └────┬────┘ └────┬───┘     │
│       └─────────┴───────────┴───────────┘          │
│                         │                           │
│                    Raw Events                       │
└────────────────────────┬────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────┐
│                 Event Processor                     │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────┐  │
│  │ Type Mapper │  │ Enrichment   │  │ Severity │  │
│  │             │  │              │  │ Analysis │  │
│  └─────────────┘  └──────────────┘  └──────────┘  │
└────────────────────────┬────────────────────────────┘
                         │
                    Domain Events
                         │
┌────────────────────────▼────────────────────────────┐
│               Event Stream (gRPC)                   │
└─────────────────────────────────────────────────────┘
```

### Key Components

1. **Resource Watchers**: Monitor specific Kubernetes resources using informers
2. **Event Processor**: Converts raw K8s events to domain events with enrichment
3. **Health Monitor**: Tracks collector health and connection status
4. **Metrics Collector**: Gathers statistics about watched resources

## Installation

### Prerequisites

- Kubernetes cluster (1.24+)
- Valid kubeconfig or in-cluster configuration
- Go 1.24+ (for building from source)

### Deployment Options

#### 1. In-Cluster Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tapio-k8s-collector
  namespace: tapio-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tapio-k8s-collector
  template:
    metadata:
      labels:
        app: tapio-k8s-collector
    spec:
      serviceAccountName: tapio-k8s-collector
      containers:
      - name: collector
        image: tapio/k8s-collector:latest
        env:
        - name: TAPIO_COLLECTOR_NAME
          value: "k8s-production"
        - name: TAPIO_SERVER_ADDRESS
          value: "tapio-server:8080"
        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "500m"
```

#### 2. External Deployment

```bash
# Using kubeconfig
export KUBECONFIG=~/.kube/config
./tapio-k8s-collector --server=tapio-server:8080

# With custom config file
./tapio-k8s-collector --config=collector.yaml
```

### RBAC Configuration

The collector requires appropriate permissions to watch resources:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: tapio-k8s-collector
  namespace: tapio-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: tapio-k8s-collector
rules:
- apiGroups: [""]
  resources: ["pods", "nodes", "services", "events", "configmaps", "secrets"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: tapio-k8s-collector
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: tapio-k8s-collector
subjects:
- kind: ServiceAccount
  name: tapio-k8s-collector
  namespace: tapio-system
```

## Configuration

### Configuration File Format

```yaml
# collector.yaml
name: k8s-production
enabled: true
event_buffer_size: 1000

# Kubernetes settings
kubeconfig: ""  # Empty for in-cluster
in_cluster: true
namespace: ""  # Empty for all namespaces

# Resource watching
watch_pods: true
watch_nodes: true
watch_services: true
watch_deployments: true
watch_events: true
watch_configmaps: true
watch_secrets: false  # Disabled by default for security

# Performance tuning
resync_period: 30m
event_rate_limit: 100  # Events per second

# Filtering
label_selector: ""
field_selector: ""

# Server connection
server_address: "tapio-server:8080"
server_insecure: false
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `TAPIO_K8S_ENABLED` | Enable/disable collector | `true` |
| `TAPIO_K8S_NAMESPACE` | Namespace to watch | `""` (all) |
| `TAPIO_K8S_WATCH_PODS` | Watch pod events | `true` |
| `TAPIO_K8S_WATCH_NODES` | Watch node events | `true` |
| `TAPIO_K8S_WATCH_SERVICES` | Watch service events | `true` |
| `TAPIO_K8S_WATCH_EVENTS` | Watch K8s events | `true` |
| `TAPIO_K8S_RATE_LIMIT` | Max events per second | `100` |
| `TAPIO_SERVER_ADDRESS` | Tapio server address | `localhost:8080` |

## Event Processing

### Event Types

The collector processes the following Kubernetes event types:

1. **Resource Events**: ADDED, MODIFIED, DELETED
2. **Error Events**: Watch errors, API errors
3. **K8s Events**: Normal and Warning events from the Event API

### Event Enrichment

Each event is enriched with:

- **Context**: Namespace, labels, annotations, node information
- **Severity**: Determined by event type and content
- **Relationships**: Links to related resources
- **Metadata**: API version, resource version, timestamps

### Severity Mapping

| K8s Event Type/Reason | Tapio Severity |
|----------------------|----------------|
| Normal | Low |
| Warning | Warning |
| Failed, FailedCreate | High |
| Evicted, NodeNotReady | Critical |
| BackOff, Unhealthy | Warning |
| Pod/Node Deletion | Warning |
| Other Deletions | Low |
| API Errors | High |

## Resource Watchers

### Supported Resources

| Resource | Information Collected |
|----------|----------------------|
| **Pods** | Phase, container status, node placement, resource requests/limits |
| **Nodes** | Conditions, capacity, allocatable resources, system info |
| **Services** | Type, cluster IP, load balancer status, endpoints |
| **Deployments** | Replicas, conditions, rollout status |
| **Events** | Involved object, reason, message, count, timestamps |
| **ConfigMaps** | Metadata, size, usage |
| **Secrets** | Metadata only (content not exposed) |

### Filtering

You can filter watched resources using:

```yaml
# Label selector
label_selector: "app=production,tier!=debug"

# Field selector  
field_selector: "status.phase=Running"

# Namespace filtering
namespace: "production"  # Single namespace
# OR
namespaces: ["prod", "staging"]  # Multiple namespaces
exclude_namespaces: ["kube-system", "kube-public"]
```

## Health Monitoring

### Health Check Endpoint

```bash
# Check collector health
curl http://localhost:8081/health

# Response
{
  "status": "healthy",
  "message": "Collector operational",
  "connected": true,
  "cluster_info": {
    "name": "production-cluster",
    "version": "v1.24.0",
    "platform": "GKE"
  },
  "events_processed": 15234,
  "events_dropped": 0,
  "error_count": 0,
  "watchers_active": 6
}
```

### Metrics

The collector exposes Prometheus metrics:

```
# Event metrics
tapio_k8s_events_total{resource="pod",type="added"} 1234
tapio_k8s_events_processed_total 15234
tapio_k8s_events_dropped_total 0
tapio_k8s_events_errors_total 0

# Watcher metrics
tapio_k8s_watchers_active{resource="pod"} 1
tapio_k8s_api_calls_total 5678
tapio_k8s_api_errors_total 12

# Connection metrics
tapio_k8s_connected 1
tapio_k8s_reconnects_total 3
```

## Security

### Best Practices

1. **Least Privilege**: Grant only required RBAC permissions
2. **Namespace Isolation**: Limit to specific namespaces when possible
3. **Secret Protection**: Disable secret watching unless required
4. **Network Policies**: Restrict collector network access
5. **Resource Limits**: Set appropriate CPU/memory limits

### Sensitive Data Handling

- Secrets content is never exposed in events
- Environment variables in pods are filtered
- Sensitive annotations are redacted

## Performance Tuning

### Resource Requirements

| Cluster Size | CPU Request | Memory Request | CPU Limit | Memory Limit |
|--------------|-------------|----------------|-----------|--------------|
| Small (<100 pods) | 100m | 128Mi | 500m | 256Mi |
| Medium (<1000 pods) | 200m | 256Mi | 1000m | 512Mi |
| Large (>1000 pods) | 500m | 512Mi | 2000m | 1Gi |

### Optimization Tips

1. **Resync Period**: Increase for stable clusters (60m+)
2. **Rate Limiting**: Adjust based on cluster activity
3. **Filtering**: Use selectors to reduce watched resources
4. **Buffer Size**: Increase for bursty workloads

### Configuration Examples

```yaml
# High-performance configuration
event_buffer_size: 5000
resync_period: 60m
event_rate_limit: 500

# Resource-constrained configuration
event_buffer_size: 500
resync_period: 15m
event_rate_limit: 50
watch_configmaps: false
watch_secrets: false
```

## Troubleshooting

### Common Issues

#### 1. Connection Errors

```
Error: failed to create kubernetes client: unable to load kubeconfig
```

**Solution**: Verify kubeconfig or in-cluster configuration

```bash
# Test connection
kubectl cluster-info

# For in-cluster
ls -la /var/run/secrets/kubernetes.io/serviceaccount/
```

#### 2. Permission Denied

```
Error: pods is forbidden: User "system:serviceaccount:tapio-system:tapio-k8s-collector" cannot list resource "pods"
```

**Solution**: Apply RBAC configuration

```bash
kubectl apply -f rbac.yaml
```

#### 3. High Memory Usage

**Symptoms**: OOMKilled, high memory consumption

**Solutions**:
- Reduce event buffer size
- Enable filtering
- Increase memory limits
- Reduce watched resource types

#### 4. Event Drops

**Symptoms**: `events_dropped_total` metric increasing

**Solutions**:
- Increase buffer size
- Increase rate limit
- Add more collector replicas

### Debug Mode

Enable debug logging:

```bash
./tapio-k8s-collector --log-level=debug
```

Debug output includes:
- API requests/responses
- Event processing details
- Watcher lifecycle events
- Connection state changes

### Monitoring Commands

```bash
# Check watcher status
kubectl logs -n tapio-system deployment/tapio-k8s-collector | grep "watcher"

# Monitor event rate
kubectl exec -n tapio-system deployment/tapio-k8s-collector -- curl localhost:8081/metrics | grep events_total

# Check connection status
kubectl exec -n tapio-system deployment/tapio-k8s-collector -- curl localhost:8081/health | jq .connected
```

## Advanced Topics

### Multi-Cluster Setup

Deploy collectors in each cluster with unique names:

```yaml
env:
- name: TAPIO_COLLECTOR_NAME
  value: "k8s-cluster-west"
- name: TAPIO_CLUSTER_LABELS
  value: "region=us-west,env=production"
```

### Custom Event Processing

Extend the processor for custom logic:

```go
type CustomProcessor struct {
    *eventProcessor
}

func (p *CustomProcessor) ProcessEvent(ctx context.Context, raw RawEvent) (Event, error) {
    event, err := p.eventProcessor.ProcessEvent(ctx, raw)
    if err != nil {
        return event, err
    }
    
    // Add custom enrichment
    if pod, ok := raw.Object.(*corev1.Pod); ok {
        event.Attributes["custom_annotation"] = pod.Annotations["mycompany.com/owner"]
    }
    
    return event, nil
}
```

### Integration with CI/CD

Detect deployment events:

```yaml
# Watch for deployment updates
label_selector: "app.kubernetes.io/managed-by=helm"
field_selector: "metadata.namespace=production"
```

## Comparison with Other Collectors

| Feature | K8s Collector | Metrics Server | kube-state-metrics | K8s Events |
|---------|---------------|----------------|-------------------|------------|
| Real-time events | ✓ | ✗ | ✗ | ✓ |
| Resource metrics | ✗ | ✓ | ✓ | ✗ |
| Historical data | ✗ | ✗ | ✗ | Limited |
| Custom enrichment | ✓ | ✗ | ✗ | ✗ |
| Correlation support | ✓ | ✗ | ✗ | ✗ |

## Contributing

See [CONTRIBUTING.md](../../CONTRIBUTING.md) for development setup and guidelines.

### Development Commands

```bash
# Run tests
go test ./pkg/collectors/k8s/...

# Build locally
go build -o tapio-k8s-collector ./pkg/collectors/k8s/cmd/collector

# Run with local kubeconfig
./tapio-k8s-collector --kubeconfig=$HOME/.kube/config --log-level=debug
```