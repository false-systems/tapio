# Tapio Operator

## Overview

The Tapio Operator manages the complete lifecycle of Tapio observability platform components in any Kubernetes cluster. It provides automated deployment, scaling, upgrades, and self-healing capabilities.

## Architecture

```
Tapio Operator
├── CRDs (Custom Resource Definitions)
│   ├── TapioCluster - Main cluster configuration
│   ├── TapioCollector - Collector configurations
│   └── TapioIntelligence - Intelligence engine settings
├── Controllers
│   ├── Cluster Controller - Manages overall deployment
│   ├── Collector Controller - Manages collector DaemonSets/Deployments
│   └── Intelligence Controller - Manages Neo4j and correlation services
└── Webhooks
    ├── Validation - Validates configurations
    └── Mutation - Applies defaults and security policies
```

## Components Managed

### Data Collection Layer
- **Node Collectors** (DaemonSets)
  - eBPF collector (kernel events)
  - Systemd collector (service events)
  - CNI collector (network events)
  
- **Cluster Collectors** (Deployments)
  - Kubernetes API collector
  - etcd collector (optional, requires access)

### Intelligence Layer
- **Neo4j** (StatefulSet)
  - Graph database for correlations
  - Persistent volume management
  - Backup/restore capabilities

- **Correlation Service** (Deployment)
  - Pattern detection
  - Event correlation
  - Root cause analysis

### Infrastructure Layer
- **NATS** (StatefulSet)
  - Event streaming backbone
  - JetStream for persistence
  - Cluster mode for HA

### API Layer
- **API Gateway** (Deployment)
  - External API access
  - Authentication/authorization
  - Rate limiting

## Installation

```bash
# Install CRDs
kubectl apply -f https://github.com/yairfalse/tapio/releases/latest/download/crds.yaml

# Install Operator
kubectl apply -f https://github.com/yairfalse/tapio/releases/latest/download/operator.yaml

# Deploy Tapio cluster
kubectl apply -f - <<EOF
apiVersion: tapio.io/v1alpha1
kind: TapioCluster
metadata:
  name: production
  namespace: tapio-system
spec:
  version: v0.1.0
  collectors:
    kubernetes:
      enabled: true
    ebpf:
      enabled: true
    systemd:
      enabled: true
  intelligence:
    neo4j:
      replicas: 3
      storage: 100Gi
    correlation:
      replicas: 2
  monitoring:
    prometheus:
      enabled: true
    grafana:
      enabled: true
EOF
```

## Features

### Auto-scaling
- Horizontal pod autoscaling based on metrics
- Vertical pod autoscaling for right-sizing
- Collector auto-discovery

### High Availability
- Multi-replica deployments
- Pod disruption budgets
- Anti-affinity rules
- Zone-aware placement

### Security
- RBAC generation
- Network policies
- Pod security policies
- Secret rotation

### Observability
- Prometheus metrics
- OpenTelemetry tracing
- Structured logging
- Health endpoints

### Lifecycle Management
- Rolling updates
- Canary deployments
- Automatic rollback
- Backup/restore

## Configuration Examples

### Minimal Development Setup
```yaml
apiVersion: tapio.io/v1alpha1
kind: TapioCluster
metadata:
  name: dev
spec:
  profile: development
  collectors:
    kubernetes:
      enabled: true
```

### Production HA Setup
```yaml
apiVersion: tapio.io/v1alpha1
kind: TapioCluster
metadata:
  name: production
spec:
  profile: production
  highAvailability: true
  collectors:
    all: true
  intelligence:
    neo4j:
      replicas: 3
      storage: 500Gi
      backup:
        enabled: true
        schedule: "0 2 * * *"
  resources:
    requests:
      memory: "16Gi"
      cpu: "4"
    limits:
      memory: "32Gi"
      cpu: "8"
```

### Edge Deployment
```yaml
apiVersion: tapio.io/v1alpha1
kind: TapioCluster
metadata:
  name: edge
spec:
  profile: edge
  lightweight: true
  collectors:
    kubernetes: true
    ebpf: false  # Might not be available
  intelligence:
    embedded: true  # Use embedded graph DB
```

## Operator Configuration

The operator itself can be configured via ConfigMap:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tapio-operator-config
  namespace: tapio-system
data:
  config.yaml: |
    reconcileInterval: 30s
    leaderElection: true
    webhooks:
      enabled: true
    metrics:
      port: 8080
    health:
      port: 8081
```

## Monitoring the Operator

```bash
# Check operator status
kubectl get pods -n tapio-system -l app=tapio-operator

# View operator logs
kubectl logs -n tapio-system deployment/tapio-operator

# Check managed resources
kubectl get tapioclusters -A

# View metrics
kubectl port-forward -n tapio-system svc/tapio-operator-metrics 8080:8080
curl localhost:8080/metrics
```

## Troubleshooting

### Common Issues

1. **Collectors not starting**
   - Check RBAC permissions
   - Verify node selectors
   - Check security contexts

2. **Neo4j not ready**
   - Check PVC provisioning
   - Verify memory limits
   - Check init containers

3. **NATS connection issues**
   - Verify network policies
   - Check service discovery
   - Validate credentials

### Debug Commands

```bash
# Get cluster status
kubectl describe tapiocluster production -n tapio-system

# Check events
kubectl get events -n tapio-system --sort-by='.lastTimestamp'

# Force reconciliation
kubectl annotate tapiocluster production -n tapio-system \
  tapio.io/force-reconcile="$(date +%s)" --overwrite
```

## Development

```bash
# Run operator locally
make run

# Run tests
make test

# Build operator image
make docker-build IMG=tapio-operator:dev

# Deploy to cluster
make deploy IMG=tapio-operator:dev
```

## Roadmap

- [ ] Multi-cluster support
- [ ] GitOps integration
- [ ] Cost optimization
- [ ] Automated tuning
- [ ] Disaster recovery
- [ ] Compliance reporting