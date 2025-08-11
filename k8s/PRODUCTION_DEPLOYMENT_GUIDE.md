# Tapio Production Deployment Guide

## Overview

This guide provides comprehensive instructions for deploying Tapio eBPF collectors with OTEL integration in production Kubernetes environments. The deployment strategy focuses on security, performance, and operational excellence.

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Pre-Deployment Checklist](#pre-deployment-checklist)
3. [Deployment Procedures](#deployment-procedures)
4. [Post-Deployment Validation](#post-deployment-validation)
5. [Operational Procedures](#operational-procedures)
6. [Troubleshooting](#troubleshooting)
7. [Security Considerations](#security-considerations)
8. [Performance Tuning](#performance-tuning)

## Prerequisites

### Cluster Requirements

- **Kubernetes Version**: 1.20+ (recommended: 1.24+)
- **Node Operating System**: Linux only
- **Kernel Version**: 4.18+ (recommended: 5.4+)
- **Architecture**: amd64, arm64
- **Container Runtime**: containerd, CRI-O, or Docker

### Resource Requirements

| Component | CPU Request | CPU Limit | Memory Request | Memory Limit |
|-----------|-------------|-----------|----------------|--------------|
| Collector (per node) | 100m | 500m | 256Mi | 1Gi |
| OTEL Collector | 500m | 2000m | 1Gi | 4Gi |
| Dependencies (NATS) | 200m | 1000m | 512Mi | 2Gi |
| Dependencies (Neo4j) | 500m | 2000m | 2Gi | 6Gi |

### Storage Requirements

- **Fast SSD storage** recommended for persistent data
- **NATS JetStream**: 50Gi minimum
- **Neo4j**: 100Gi minimum
- **Logs**: 10Gi minimum (with rotation)

### Network Requirements

- **Bandwidth**: 100Mbps+ per node for metric export
- **Latency**: <10ms between components
- **DNS**: Internal cluster DNS resolution
- **External Access**: HTTPS/443 for external monitoring backends

### External Dependencies

- **Prometheus**: For metrics collection (optional)
- **Jaeger**: For trace storage (optional)
- **Elasticsearch**: For log storage (optional)
- **Grafana**: For dashboards (optional)

## Pre-Deployment Checklist

### ✅ Infrastructure Validation

- [ ] Kubernetes cluster is healthy and accessible
- [ ] All nodes meet minimum kernel version requirements (4.18+)
- [ ] Container runtime is properly configured
- [ ] Storage classes are available and performant
- [ ] Network policies are supported (if using CNI with NetworkPolicy support)
- [ ] Resource quotas allow sufficient resources for Tapio components

### ✅ Security Preparation

- [ ] Pod Security Standards policy reviewed and approved
- [ ] RBAC permissions reviewed and minimal permissions granted
- [ ] Network policies designed for zero-trust architecture
- [ ] TLS certificates prepared for secure communication
- [ ] Image pull secrets configured for private registries
- [ ] Service accounts created with appropriate permissions

### ✅ Monitoring Preparation

- [ ] Prometheus operator installed (if using ServiceMonitors)
- [ ] Grafana available for dashboard installation
- [ ] Alert manager configured for critical alerts
- [ ] Log aggregation system available (optional)
- [ ] External monitoring backends configured

### ✅ Configuration Preparation

- [ ] Cluster-specific values prepared (cluster name, region, environment)
- [ ] Resource limits tuned for workload requirements
- [ ] Sampling rates configured for expected event volume
- [ ] Export endpoints configured for observability backends
- [ ] Retention policies defined for different data types

## Deployment Procedures

### Method 1: Script-Based Deployment (Recommended)

```bash
# 1. Clone repository and navigate to k8s directory
git clone https://github.com/yairfalse/tapio.git
cd tapio/k8s

# 2. Set environment variables
export CLUSTER_NAME="production-cluster"
export ENVIRONMENT="production"
export NAMESPACE="tapio-system"

# 3. Review and customize configurations
vim configmaps.yaml
vim secrets.yaml

# 4. Run deployment script
./scripts/deploy.sh \
    --cluster "$CLUSTER_NAME" \
    --environment "$ENVIRONMENT" \
    --namespace "$NAMESPACE" \
    --verbose

# 5. Verify deployment
./scripts/health-check.sh --namespace "$NAMESPACE" --verbose
```

### Method 2: Helm-Based Deployment

```bash
# 1. Add Tapio Helm repository (when available)
helm repo add tapio https://charts.tapio.io
helm repo update

# 2. Create values file
cat > production-values.yaml << EOF
global:
  clusterName: "production-cluster"
  environment: "production"
  region: "us-west-2"

collector:
  resources:
    limits:
      cpu: "500m"
      memory: "1Gi"
    requests:
      cpu: "100m"
      memory: "256Mi"

otelCollector:
  centralized:
    replicas: 3
    autoscaling:
      enabled: true
      maxReplicas: 10

monitoring:
  enabled: true

security:
  networkPolicies:
    enabled: true
EOF

# 3. Install Tapio
helm install tapio tapio/tapio \
    --namespace tapio-system \
    --create-namespace \
    --values production-values.yaml \
    --timeout 10m

# 4. Verify installation
helm test tapio --namespace tapio-system
```

### Method 3: Manual Deployment

```bash
# 1. Create namespace
kubectl create namespace tapio-system

# 2. Apply components in order
kubectl apply -f secrets.yaml
kubectl apply -f rbac.yaml
kubectl apply -f configmaps.yaml
kubectl apply -f network-policies.yaml
kubectl apply -f otel-collector.yaml
kubectl apply -f collector-daemonset.yaml
kubectl apply -f monitoring.yaml

# 3. Wait for deployment
kubectl rollout status daemonset/tapio-collector -n tapio-system
kubectl rollout status deployment/tapio-otel-collector -n tapio-system
```

## Post-Deployment Validation

### ✅ Component Health Check

```bash
# Run comprehensive health check
./scripts/health-check.sh --namespace tapio-system --verbose

# Expected output should show all components as healthy (✅)
```

### ✅ Functional Validation

```bash
# 1. Check collector pods are running on all nodes
kubectl get daemonset tapio-collector -n tapio-system
kubectl get pods -l app.kubernetes.io/name=tapio-collector -n tapio-system -o wide

# 2. Verify eBPF programs are loaded
kubectl exec -n tapio-system daemonset/tapio-collector -- ls -la /sys/fs/bpf/

# 3. Check metrics endpoint
kubectl port-forward -n tapio-system daemonset/tapio-collector 9090:9090 &
curl http://localhost:9090/metrics | grep tapio_

# 4. Verify OTEL pipeline
kubectl port-forward -n tapio-system deployment/tapio-otel-collector 55679:55679 &
curl http://localhost:55679/debug/tracez

# 5. Check logs for errors
kubectl logs -n tapio-system daemonset/tapio-collector --tail=100
kubectl logs -n tapio-system deployment/tapio-otel-collector --tail=100
```

### ✅ Performance Validation

```bash
# 1. Check resource usage
kubectl top pods -n tapio-system --sort-by=cpu
kubectl top pods -n tapio-system --sort-by=memory

# 2. Monitor event processing rate
kubectl exec -n tapio-system daemonset/tapio-collector -- \
    curl -s http://localhost:9090/metrics | grep tapio_events_processed_total

# 3. Check for dropped events
kubectl exec -n tapio-system daemonset/tapio-collector -- \
    curl -s http://localhost:9090/metrics | grep tapio_events_dropped_total
```

### ✅ Security Validation

```bash
# 1. Check Pod Security Standards compliance
kubectl get pods -n tapio-system -o jsonpath='{.items[*].metadata.annotations}' | grep security

# 2. Verify network policies
kubectl get networkpolicies -n tapio-system
kubectl describe networkpolicy tapio-collector-policy -n tapio-system

# 3. Test RBAC permissions
kubectl auth can-i get nodes --as=system:serviceaccount:tapio-system:tapio-collector
kubectl auth can-i create pods --as=system:serviceaccount:tapio-system:tapio-collector
```

## Operational Procedures

### Scaling Operations

#### Scale OTEL Collector

```bash
# Manual scaling
kubectl scale deployment tapio-otel-collector --replicas=5 -n tapio-system

# Configure HPA
kubectl patch hpa tapio-otel-collector-hpa -n tapio-system -p '{"spec":{"maxReplicas":15}}'
```

#### Resource Adjustment

```bash
# Update resource limits
kubectl patch daemonset tapio-collector -n tapio-system -p '{
  "spec": {
    "template": {
      "spec": {
        "containers": [
          {
            "name": "tapio-collector",
            "resources": {
              "limits": {"cpu": "1", "memory": "2Gi"},
              "requests": {"cpu": "200m", "memory": "512Mi"}
            }
          }
        ]
      }
    }
  }
}'
```

### Configuration Updates

#### Update Collector Configuration

```bash
# Edit ConfigMap
kubectl edit configmap tapio-collector-config -n tapio-system

# Restart collectors to pick up changes
kubectl rollout restart daemonset/tapio-collector -n tapio-system
```

#### Update OTEL Configuration

```bash
# Edit OTEL ConfigMap
kubectl edit configmap tapio-otel-collector-config -n tapio-system

# Restart OTEL collectors
kubectl rollout restart deployment/tapio-otel-collector -n tapio-system
```

### Maintenance Procedures

#### Rolling Updates

```bash
# Update collector image
kubectl set image daemonset/tapio-collector \
    tapio-collector=tapio/collector:v1.1.0 \
    -n tapio-system

# Monitor rollout
kubectl rollout status daemonset/tapio-collector -n tapio-system
```

#### Backup Procedures

```bash
# Backup configurations
kubectl get configmaps -n tapio-system -o yaml > tapio-configmaps-backup.yaml
kubectl get secrets -n tapio-system -o yaml > tapio-secrets-backup.yaml

# Backup RBAC
kubectl get clusterroles,clusterrolebindings -l app.kubernetes.io/part-of=tapio -o yaml > tapio-rbac-backup.yaml
```

#### Log Rotation

```bash
# Set up log rotation (example for systemd)
cat > /etc/logrotate.d/tapio << EOF
/var/log/tapio/*.log {
    daily
    rotate 7
    compress
    missingok
    notifempty
    postrotate
        kubectl rollout restart daemonset/tapio-collector -n tapio-system
    endscript
}
EOF
```

## Troubleshooting

### Common Issues and Solutions

#### eBPF Program Loading Failures

**Symptoms**: Collectors fail to start, logs show eBPF program loading errors

**Diagnosis**:
```bash
# Check kernel version
kubectl get nodes -o jsonpath='{.items[*].status.nodeInfo.kernelVersion}'

# Check BTF support
kubectl exec -n tapio-system daemonset/tapio-collector -- ls -la /sys/kernel/btf/vmlinux

# Check BPF filesystem
kubectl exec -n tapio-system daemonset/tapio-collector -- mount | grep bpf
```

**Solutions**:
1. Ensure kernel version ≥ 4.18
2. Enable BTF in kernel configuration
3. Mount BPF filesystem: `mount -t bpf bpf /sys/fs/bpf`
4. Set fallback mode in configuration

#### High Memory Usage

**Symptoms**: Collectors consuming excessive memory, potential OOM kills

**Diagnosis**:
```bash
# Check memory usage
kubectl top pods -n tapio-system --sort-by=memory

# Check for OOM kills
kubectl get events -n tapio-system --field-selector reason=OOMKilling

# Check ring buffer sizes
kubectl logs -n tapio-system daemonset/tapio-collector | grep "ring buffer"
```

**Solutions**:
1. Reduce ring buffer sizes in configuration
2. Increase memory limits
3. Reduce sampling rates
4. Enable circuit breaker protection

#### Network Connectivity Issues

**Symptoms**: OTEL export failures, service discovery problems

**Diagnosis**:
```bash
# Test DNS resolution
kubectl exec -n tapio-system daemonset/tapio-collector -- nslookup tapio-otel-collector

# Check network policies
kubectl describe networkpolicy -n tapio-system

# Test connectivity
kubectl exec -n tapio-system daemonset/tapio-collector -- \
    curl -v telnet://tapio-otel-collector:4317
```

**Solutions**:
1. Verify NetworkPolicy configurations
2. Check service selectors
3. Ensure correct port mappings
4. Validate DNS configuration

#### Performance Degradation

**Symptoms**: High latency, dropped events, CPU throttling

**Diagnosis**:
```bash
# Check event drop rates
kubectl exec -n tapio-system daemonset/tapio-collector -- \
    curl -s http://localhost:9090/metrics | grep dropped

# Check CPU throttling
kubectl top pods -n tapio-system
kubectl describe pods -l app.kubernetes.io/name=tapio-collector -n tapio-system | grep -A5 -B5 throttl
```

**Solutions**:
1. Increase CPU limits
2. Tune sampling rates
3. Optimize ring buffer sizes
4. Scale OTEL collector replicas

## Security Considerations

### Least Privilege Access

- Grant minimal required permissions to service accounts
- Use dedicated service accounts for different components
- Regularly audit and review RBAC permissions
- Implement network micro-segmentation with NetworkPolicies

### Data Protection

- Encrypt data in transit using TLS
- Encrypt persistent data at rest
- Implement proper key management
- Regular security updates and patches

### Container Security

- Use non-root users where possible (except for eBPF collectors)
- Implement Pod Security Standards
- Regular container image scanning
- Use read-only root filesystems where possible

### Network Security

- Implement NetworkPolicies for zero-trust networking
- Use service mesh for additional security layers
- Monitor network traffic for anomalies
- Implement ingress/egress filtering

## Performance Tuning

### Resource Optimization

#### CPU Tuning
```yaml
# Optimized CPU settings
resources:
  requests:
    cpu: "100m"      # Baseline for scheduling
  limits:
    cpu: "500m"      # Prevent CPU hogging
```

#### Memory Tuning
```yaml
# Optimized memory settings
resources:
  requests:
    memory: "256Mi"   # Baseline for scheduling
  limits:
    memory: "1Gi"     # Prevent OOM
```

### Sampling Configuration

```yaml
# Production sampling rates
ebpf:
  programs:
    network:
      samplingRate: 0.1    # 10% sampling
    process:
      samplingRate: 0.05   # 5% sampling
    security:
      samplingRate: 1.0    # 100% for security events
    dns:
      samplingRate: 0.5    # 50% sampling
```

### Ring Buffer Optimization

```yaml
# Optimized ring buffer sizes
ebpf:
  programs:
    network:
      ringBufferSize: 4194304  # 4MB for high-volume
    process:
      ringBufferSize: 2097152  # 2MB for medium-volume
    security:
      ringBufferSize: 8388608  # 8MB for critical events
```

### Export Batching

```yaml
# Optimized export settings
export:
  otel:
    batch:
      timeout: "5s"
      sendBatchSize: 1000
      sendBatchMaxSize: 1500
```

## Monitoring and Alerting

### Key Metrics to Monitor

1. **Collector Health**
   - Pod availability: `up{job="tapio-collector"}`
   - Event processing rate: `rate(tapio_events_processed_total[5m])`
   - Event drop rate: `rate(tapio_events_dropped_total[5m])`

2. **Resource Usage**
   - CPU utilization: `rate(container_cpu_usage_seconds_total[5m])`
   - Memory utilization: `container_memory_usage_bytes`
   - Ring buffer usage: `tapio_ring_buffer_usage_percent`

3. **eBPF Programs**
   - Program load status: `tapio_ebpf_program_loaded`
   - Load failures: `rate(tapio_ebpf_load_failures_total[5m])`

4. **OTEL Pipeline**
   - Span processing rate: `rate(otelcol_receiver_accepted_spans_total[5m])`
   - Export failures: `rate(otelcol_exporter_send_failed_spans_total[5m])`
   - Queue size: `otelcol_exporter_queue_size`

### Critical Alerts

Configure alerts for:
- Collector pods down for >2 minutes
- High event drop rate >1000 events/sec for >5 minutes
- Memory usage >90% for >5 minutes
- eBPF program load failures
- OTEL export failures >10/sec for >5 minutes

## Cleanup Procedures

### Safe Removal

```bash
# Graceful cleanup with data preservation
./scripts/cleanup.sh --namespace tapio-system --preserve-data

# Complete removal
./scripts/cleanup.sh --namespace tapio-system --force
```

### Emergency Shutdown

```bash
# Emergency stop all collectors
kubectl scale daemonset tapio-collector --replicas=0 -n tapio-system
kubectl scale deployment tapio-otel-collector --replicas=0 -n tapio-system
```

## Support and Documentation

- **Documentation**: https://docs.tapio.io
- **Issues**: https://github.com/yairfalse/tapio/issues
- **Community**: https://tapio.io/community
- **Commercial Support**: https://tapio.io/support

## Version Compatibility Matrix

| Tapio Version | Kubernetes | Kernel | Container Runtime |
|---------------|------------|--------|-------------------|
| 1.0.x         | 1.20+      | 4.18+  | containerd 1.4+   |
| 1.1.x         | 1.22+      | 5.4+   | containerd 1.5+   |

---

**Last Updated**: 2024-01-10
**Document Version**: 1.0
**Reviewed By**: Tapio Platform Team