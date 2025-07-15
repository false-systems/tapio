# Tapio Deployment Guide

This directory contains deployment configurations for all Tapio components in various environments.

## 🏗️ Architecture Overview

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   tapio-cli     │───▶│  tapio-engine    │◀───│ tapio-collector │
│   (CLI Tool)    │    │ (Correlation     │    │ (Data Agent)    │
│                 │    │  Engine)         │    │                 │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                              │                          │
                              ▼                          │
                       ┌──────────────────┐              │
                       │   tapio-gui      │              │
                       │ (Desktop App)    │              │
                       └──────────────────┘              │
                              │                          │
                              ▼                          ▼
                    ┌─────────────────────────────────────────────┐
                    │               Plugins                       │
                    │  ┌─────────────┐  ┌─────────────────────┐   │
                    │  │ OTEL Plugin │  │ Prometheus Plugin   │   │
                    │  └─────────────┘  └─────────────────────┘   │
                    └─────────────────────────────────────────────┘
```

## 📦 Deployment Options

### 1. Kubernetes (Recommended for Production)

**Prerequisites:**
- Kubernetes 1.21+
- Helm 3.0+
- kubectl configured

**Quick Install:**
```bash
# Add Tapio Helm repository
helm repo add tapio https://charts.tapio.sh
helm repo update

# Install Tapio
helm install tapio tapio/tapio -n tapio-system --create-namespace

# Verify installation
kubectl get pods -n tapio-system
```

**Custom Installation:**
```bash
# Create custom values file
cp deployments/helm/tapio/values.yaml my-values.yaml
# Edit my-values.yaml with your settings

# Install with custom values
helm install tapio tapio/tapio -n tapio-system --create-namespace -f my-values.yaml
```

### 2. Docker Compose (Development)

**Prerequisites:**
- Docker 20.0+
- Docker Compose 2.0+

**Quick Start:**
```bash
# Clone repository
git clone https://github.com/yairfalse/tapio.git
cd tapio

# Start all services
docker-compose -f deployments/docker-compose.yaml up -d

# Check services
docker-compose -f deployments/docker-compose.yaml ps

# View logs
docker-compose -f deployments/docker-compose.yaml logs -f tapio-engine
```

**Service URLs:**
- Tapio Engine API: http://localhost:8080
- Tapio GUI: http://localhost:3000
- Prometheus: http://localhost:9092
- Grafana: http://localhost:3001 (admin/admin)
- Jaeger: http://localhost:16686

### 3. Standalone Containers

**Engine:**
```bash
docker run -d \
  --name tapio-engine \
  -p 9090:9090 \
  -p 8080:8080 \
  -v $(pwd)/deployments/engine/config.yaml:/etc/tapio/config.yaml \
  tapio/engine:latest
```

**CLI:**
```bash
docker run --rm \
  --network host \
  tapio/cli:latest check --engine localhost:9090
```

## ⚙️ Configuration

### Environment Variables

**Engine:**
- `TAPIO_ENGINE_CONFIG`: Configuration file path
- `TAPIO_ENGINE_LOG_LEVEL`: Log level (debug, info, warn, error)
- `TAPIO_ENGINE_DATA_DIR`: Data directory path

**Collector:**
- `TAPIO_COLLECTOR_SERVER`: Engine endpoint
- `TAPIO_COLLECTOR_CONFIG`: Configuration file path
- `TAPIO_COLLECTOR_NODE_NAME`: Kubernetes node name

**CLI:**
- `TAPIO_ENGINE_ENDPOINT`: Engine endpoint
- `TAPIO_CLI_CONFIG`: Configuration file path

### Resource Requirements

**Minimum (Development):**
```yaml
Engine:    256Mi memory, 250m CPU
Collector: 64Mi memory, 50m CPU  
GUI:       128Mi memory, 100m CPU
Plugins:   64Mi memory, 50m CPU each
```

**Recommended (Production):**
```yaml
Engine:    512Mi memory, 500m CPU
Collector: 128Mi memory, 100m CPU
GUI:       256Mi memory, 200m CPU  
Plugins:   128Mi memory, 100m CPU each
```

## 🔒 Security

### RBAC Configuration

The Helm chart automatically creates:
- ServiceAccount for each component
- ClusterRole with minimal required permissions
- ClusterRoleBinding for service accounts

### Network Policies

Network policies are included to:
- Restrict inter-pod communication
- Allow only required ingress/egress traffic
- Isolate components by namespace

### Pod Security

All pods run with:
- Non-root user (UID 1000)
- Read-only root filesystem
- Dropped capabilities
- Security context constraints

## 📊 Monitoring

### Prometheus Integration

**Metrics Endpoints:**
- Engine: `:9091/metrics`
- Collector: `:9091/metrics`
- Plugins: `:8080/metrics`

**Service Monitors:**
Automatic ServiceMonitor creation for Prometheus scraping.

### Grafana Dashboards

Pre-built dashboards for:
- Tapio Engine performance
- Collector health and metrics
- Plugin status and throughput
- Kubernetes cluster overview

### Jaeger Tracing

Distributed tracing integration via OTEL plugin:
- Request tracing across components
- Performance analysis
- Error tracking

## 🔧 Troubleshooting

### Common Issues

**Engine not starting:**
```bash
# Check logs
kubectl logs -n tapio-system deployment/tapio-engine

# Check configuration
kubectl get configmap -n tapio-system tapio-engine-config -o yaml
```

**Collector connection issues:**
```bash
# Check network connectivity
kubectl exec -it -n tapio-system daemonset/tapio-collector -- \
  nslookup tapio-engine-grpc.tapio-system.svc.cluster.local

# Check RBAC permissions
kubectl auth can-i get pods --as=system:serviceaccount:tapio-system:tapio-collector
```

**Plugin failures:**
```bash
# Check plugin logs
kubectl logs -n tapio-system deployment/tapio-otel-plugin

# Verify plugin configuration
kubectl get configmap -n tapio-system tapio-otel-plugin-config -o yaml
```

### Health Checks

**Engine Health:**
```bash
curl http://localhost:8080/health
```

**Collector Health:**
```bash
kubectl exec -it -n tapio-system daemonset/tapio-collector -- \
  wget -qO- http://localhost:8080/health
```

## 🚀 Scaling

### Horizontal Scaling

**Engine (Stateless):**
```bash
kubectl scale deployment tapio-engine --replicas=3 -n tapio-system
```

**Plugins:**
```bash
kubectl scale deployment tapio-otel-plugin --replicas=2 -n tapio-system
```

### Vertical Scaling

**Update resource requests/limits:**
```bash
kubectl patch deployment tapio-engine -n tapio-system -p '
{
  "spec": {
    "template": {
      "spec": {
        "containers": [{
          "name": "tapio-engine",
          "resources": {
            "requests": {"memory": "512Mi", "cpu": "500m"},
            "limits": {"memory": "1Gi", "cpu": "1000m"}
          }
        }]
      }
    }
  }
}'
```

## 📋 Maintenance

### Backup

**Configuration:**
```bash
kubectl get configmaps -n tapio-system -o yaml > tapio-config-backup.yaml
```

**Data (if persistent storage used):**
```bash
kubectl exec -it -n tapio-system deployment/tapio-engine -- \
  tar czf - /var/lib/tapio/data > tapio-data-backup.tar.gz
```

### Updates

**Rolling Update:**
```bash
helm upgrade tapio tapio/tapio -n tapio-system -f my-values.yaml
```

**Manual Image Update:**
```bash
kubectl set image deployment/tapio-engine tapio-engine=tapio/engine:1.1.0 -n tapio-system
```

## 📚 Additional Resources

- [Configuration Reference](../docs/configuration.md)
- [Performance Tuning](../docs/performance.md)
- [Security Guide](../docs/security.md)
- [Troubleshooting Guide](../docs/troubleshooting.md)
- [API Documentation](../docs/api.md)