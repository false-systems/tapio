# K8sGrapher

K8sGrapher is a Kubernetes relationship graph builder that creates and maintains a real-time graph of Kubernetes resources and their relationships in Neo4j. It's a core component of Tapio's semantic correlation platform.

## Overview

K8sGrapher watches Kubernetes resources and builds a comprehensive relationship graph that includes:

- Service → Pod selections
- Pod → ConfigMap/Secret dependencies
- Pod → PersistentVolumeClaim bindings
- Ownership chains (Deployment → ReplicaSet → Pod)
- Cross-namespace resource relationships

This graph enables semantic correlation - understanding how changes in one resource (like a ConfigMap) impact other resources (like Pods that mount it).

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        K8sGrapher                           │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────┐     ┌──────────────┐    ┌──────────────┐ │
│  │   K8s API   │────▶│  Informers   │───▶│ Graph Update │ │
│  │   Watchers  │     │   (Cache)    │    │    Queue     │ │
│  └─────────────┘     └──────────────┘    └──────────────┘ │
│                                                    │        │
│                                                    ▼        │
│  ┌─────────────┐     ┌──────────────┐    ┌──────────────┐ │
│  │    OTEL     │◀────│   Update     │◀───│   Neo4j      │ │
│  │   Metrics   │     │  Processor   │    │   Driver     │ │
│  └─────────────┘     └──────────────┘    └──────────────┘ │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## Features

- **Real-time Updates**: Uses K8s watch API for instant graph updates
- **Comprehensive Relationships**: Tracks all major K8s resource relationships
- **Performance Optimized**: Batched updates, efficient Neo4j queries
- **Full Observability**: OpenTelemetry metrics and traces
- **Fault Tolerant**: Handles API disconnections, retries failed updates
- **Multi-namespace**: Can watch specific namespace or all namespaces

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NEO4J_URI` | Neo4j connection URI | `neo4j://localhost:7687` |
| `NEO4J_USERNAME` | Neo4j username | `neo4j` |
| `NEO4J_PASSWORD` | Neo4j password (required) | - |
| `K8S_NAMESPACE` | Namespace to watch (empty for all) | `""` |
| `KUBECONFIG` | Path to kubeconfig file | `~/.kube/config` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTEL collector endpoint | `localhost:4317` |

### Kubernetes Permissions

K8sGrapher requires read access to the following resources:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: k8sgrapher
rules:
- apiGroups: [""]
  resources: ["services", "pods", "configmaps", "secrets", "persistentvolumeclaims"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["apps"]
  resources: ["deployments", "replicasets"]
  verbs: ["get", "list", "watch"]
```

## Neo4j Schema

### Nodes

- **Service**: `{namespace, name, uid, selector, type, clusterIP}`
- **Pod**: `{namespace, name, uid, labels, phase, ready, nodeName}`
- **ConfigMap**: `{namespace, name, uid, dataKeys}`
- **Secret**: `{namespace, name, uid, type, dataKeys}`
- **Deployment**: `{namespace, name, uid, replicas, selector}`
- **ReplicaSet**: `{namespace, name, uid, replicas, selector}`
- **PVC**: `{namespace, name, uid, storageClass, phase, capacity}`

### Relationships

- `(Service)-[:SELECTS]->(Pod)` - Service selects pods by labels
- `(Pod)-[:MOUNTS]->(ConfigMap)` - Pod mounts ConfigMap as volume or env
- `(Pod)-[:USES_SECRET]->(Secret)` - Pod uses Secret
- `(Pod)-[:CLAIMS]->(PVC)` - Pod claims persistent volume
- `(Deployment)-[:OWNS]->(ReplicaSet)` - Ownership chain
- `(ReplicaSet)-[:OWNS]->(Pod)` - Ownership chain

## Metrics

K8sGrapher exposes the following OpenTelemetry metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `tapio.grapher.relationships.discovered` | Counter | Total relationships discovered |
| `tapio.grapher.graph.update.duration` | Histogram | Time to update Neo4j graph |
| `tapio.grapher.k8s.watch.events` | Counter | K8s API watch events by type |
| `tapio.grapher.relationships.active` | UpDownCounter | Current active relationships |
| `tapio.grapher.mappings.service_pod` | UpDownCounter | Service→Pod mappings |
| `tapio.grapher.mounts.configmap` | UpDownCounter | ConfigMap mounts |

## Building

```bash
# Build locally
go build -o k8sgrapher ./pkg/integrations/k8sgrapher/cmd/main.go

# Build Docker image
docker build -f pkg/integrations/k8sgrapher/Dockerfile -t tapio/k8sgrapher:latest .

# Run tests
go test ./pkg/integrations/k8sgrapher/...
```

## Running

### Local Development

```bash
# Set environment
export NEO4J_PASSWORD=your-password
export NEO4J_URI=neo4j://localhost:7687

# Run with local kubeconfig
./k8sgrapher

# Watch specific namespace
K8S_NAMESPACE=production ./k8sgrapher
```

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: k8sgrapher
spec:
  replicas: 1
  selector:
    matchLabels:
      app: k8sgrapher
  template:
    metadata:
      labels:
        app: k8sgrapher
    spec:
      serviceAccountName: k8sgrapher
      containers:
      - name: k8sgrapher
        image: tapio/k8sgrapher:latest
        env:
        - name: NEO4J_URI
          value: "neo4j://neo4j:7687"
        - name: NEO4J_PASSWORD
          valueFrom:
            secretKeyRef:
              name: neo4j-credentials
              key: password
        - name: OTEL_EXPORTER_OTLP_ENDPOINT
          value: "otel-collector:4317"
```

## Query Examples

### Find all pods selected by a service

```cypher
MATCH (s:Service {name: "frontend", namespace: "default"})-[:SELECTS]->(p:Pod)
RETURN p.name, p.ready, p.phase
```

### Find all resources affected by a ConfigMap

```cypher
MATCH (cm:ConfigMap {name: "app-config"})<-[:MOUNTS]-(p:Pod)
MATCH (p)<-[:SELECTS]-(s:Service)
RETURN cm.name, collect(DISTINCT p.name) as pods, collect(DISTINCT s.name) as services
```

### Trace ownership chain

```cypher
MATCH path = (d:Deployment)-[:OWNS*]->(p:Pod)
WHERE d.name = "frontend"
RETURN path
```

## Integration with Tapio

K8sGrapher provides the foundation for Tapio's semantic correlation by:

1. **Real-time Graph**: Correlators can query current K8s state
2. **Historical Context**: Graph changes are tracked over time
3. **Relationship Traversal**: Find impact chains across resources
4. **Configuration Dependencies**: Understand config→pod relationships

Correlators use this graph to answer questions like:
- "Which pods will be affected by this ConfigMap change?"
- "What services depend on this failing pod?"
- "What's the deployment history of this ReplicaSet?"

## Troubleshooting

### Common Issues

1. **Connection to Neo4j failed**
   - Check NEO4J_URI and credentials
   - Verify Neo4j is running and accessible
   - Check network policies if in K8s

2. **No resources discovered**
   - Verify RBAC permissions
   - Check namespace configuration
   - Look for errors in logs

3. **High memory usage**
   - Adjust resync period
   - Check for namespace with many resources
   - Monitor graph update queue size

### Debug Logging

Enable debug logging by setting log level:

```go
logger, _ := zap.NewDevelopment()
```

## Contributing

See [CONTRIBUTING.md](../../../CONTRIBUTING.md) for development guidelines.

## License

Part of the Tapio observability platform.