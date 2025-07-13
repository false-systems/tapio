# Tapio Deployment Guide

This directory contains Kubernetes manifests for deploying the Tapio observability platform with separate collector and server binaries.

## Architecture

The Tapio platform consists of two main components:

- **tapio-collector**: Lightweight DaemonSet that runs on every node, collecting eBPF, Kubernetes, and systemd data
- **tapio-server**: Central processing server that receives streamed data, performs correlation analysis, and provides insights

## Quick Start

1. **Create the namespace:**
   ```bash
   kubectl apply -f namespace.yaml
   ```

2. **Deploy the server:**
   ```bash
   kubectl apply -f server-deployment.yaml
   ```

3. **Deploy the collectors:**
   ```bash
   kubectl apply -f collector-daemonset.yaml
   ```

4. **Verify deployment:**
   ```bash
   kubectl get pods -n tapio-system
   kubectl logs -n tapio-system -l app.kubernetes.io/component=collector
   kubectl logs -n tapio-system -l app.kubernetes.io/component=server
   ```

## Components

### Namespace (namespace.yaml)
- Creates `tapio-system` namespace with appropriate security policies
- Sets up resource quotas and network policies
- Enables privileged pod security for eBPF requirements

### Collector DaemonSet (collector-daemonset.yaml)
- **Image**: `tapio/collector:1.0.0`
- **Resources**: 100MB memory, 1% CPU per node
- **Privileges**: Requires privileged access for eBPF and system monitoring
- **Configuration**: Via ConfigMap with comprehensive collector settings
- **Features**:
  - eBPF kernel-level monitoring
  - Kubernetes API event collection
  - systemd service monitoring
  - Automatic server discovery and connection
  - Health checks and metrics endpoint

### Server Deployment (server-deployment.yaml)
- **Image**: `tapio/server:1.0.0`
- **Replicas**: 2 (with HPA scaling 2-10)
- **Resources**: 500MB memory, 50% CPU
- **Features**:
  - High-performance gRPC streaming (165k+ events/sec)
  - Real-time correlation engine
  - Prometheus metrics integration
  - Horizontal auto-scaling
  - Pod disruption budget for HA

## Configuration

### Collector Configuration

The collector is configured via ConfigMap. Key settings:

```yaml
collector:
  enabled_collectors: ["ebpf", "k8s", "systemd"]
  sampling_rate: 1.0
  max_events_per_sec: 10000

grpc:
  server_endpoints: ["tapio-server:9090"]
  max_batch_size: 100
  compression: "lz4"

resources:
  max_memory_mb: 100
  max_cpu_milli: 10
```

### Server Configuration

The server is configured via ConfigMap. Key settings:

```yaml
server:
  max_events_per_sec: 165000
  max_concurrent_streams: 1000

correlation:
  enabled: true
  analysis_window: "5m"

metrics:
  prometheus_enabled: true
  prometheus_port: 9091
```

## Security

### Collector Security
- Runs as root (required for eBPF)
- Privileged container with specific capabilities
- Host PID and network access for monitoring
- RBAC with cluster-wide read permissions

### Server Security
- Runs as non-root user (10001)
- Read-only filesystem
- Minimal RBAC permissions
- Network policies for traffic isolation

## Monitoring

### Health Checks
- **Collector**: `http://collector-pod:8081/healthz`
- **Server**: `http://server-pod:9091/healthz`

### Metrics
- **Collector**: `http://collector-pod:8081/metrics`
- **Server**: `http://server-pod:9091/metrics`

### Prometheus Integration

Both components expose Prometheus metrics:

```yaml
# ServiceMonitor for Prometheus Operator
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: tapio
  namespace: tapio-system
spec:
  selector:
    matchLabels:
      app.kubernetes.io/part-of: tapio
  endpoints:
    - port: metrics
      interval: 30s
      path: /metrics
```

## Troubleshooting

### Common Issues

1. **Collector fails to start**:
   ```bash
   # Check eBPF availability
   kubectl exec -n tapio-system collector-pod -- ls /sys/kernel/debug/tracing
   
   # Check permissions
   kubectl get psp,scc  # Pod security policies
   ```

2. **Server connection issues**:
   ```bash
   # Check service resolution
   kubectl exec -n tapio-system collector-pod -- nslookup tapio-server
   
   # Check network policies
   kubectl get networkpolicy -n tapio-system
   ```

3. **High resource usage**:
   ```bash
   # Check metrics
   kubectl top pods -n tapio-system
   
   # Adjust sampling rate
   kubectl patch configmap tapio-collector-config -n tapio-system --patch='{"data":{"collector.yaml":"...sampling_rate: 0.5..."}}'
   ```

### Log Analysis

```bash
# Collector logs
kubectl logs -n tapio-system -l app.kubernetes.io/component=collector -f

# Server logs  
kubectl logs -n tapio-system -l app.kubernetes.io/component=server -f

# Filter for errors
kubectl logs -n tapio-system -l app.kubernetes.io/part-of=tapio | grep ERROR
```

## Performance Tuning

### Collector Optimization

1. **Adjust sampling rate**:
   ```yaml
   collector:
     sampling_rate: 0.1  # Collect 10% of events
   ```

2. **Filter by severity**:
   ```yaml
   pipeline:
     filter_config:
       min_severity: "medium"  # Only medium+ severity
   ```

3. **Disable collectors**:
   ```yaml
   collectors:
     systemd:
       enabled: false  # Disable if not needed
   ```

### Server Optimization

1. **Increase resources**:
   ```yaml
   resources:
     limits:
       memory: "1Gi"
       cpu: "1000m"
   ```

2. **Tune correlation window**:
   ```yaml
   correlation:
     analysis_window: "1m"  # Shorter window for less memory
   ```

3. **Enable HPA**:
   ```bash
   kubectl autoscale deployment tapio-server -n tapio-system --cpu-percent=50 --min=2 --max=10
   ```

## Upgrading

### Rolling Upgrade

1. **Update server first**:
   ```bash
   kubectl set image deployment/tapio-server tapio-server=tapio/server:1.1.0 -n tapio-system
   kubectl rollout status deployment/tapio-server -n tapio-system
   ```

2. **Update collectors**:
   ```bash
   kubectl set image daemonset/tapio-collector tapio-collector=tapio/collector:1.1.0 -n tapio-system
   kubectl rollout status daemonset/tapio-collector -n tapio-system
   ```

### Configuration Updates

```bash
# Update collector config
kubectl patch configmap tapio-collector-config -n tapio-system --patch-file=new-collector-config.yaml

# Restart collectors to pick up changes
kubectl rollout restart daemonset/tapio-collector -n tapio-system
```

## Production Considerations

### High Availability
- Deploy server across multiple availability zones
- Use persistent storage for correlation state
- Implement proper backup and disaster recovery

### Security Hardening
- Enable TLS for gRPC communication
- Use Pod Security Policies/Standards
- Implement network segmentation
- Regular security scanning

### Scalability
- Monitor resource usage and scale accordingly
- Consider sharding for very large clusters
- Implement proper load balancing

### Compliance
- Ensure data retention policies are met
- Implement audit logging
- Consider data sovereignty requirements

## Support

For issues and questions:
- Check logs and metrics first
- Review this troubleshooting guide
- Open an issue in the Tapio repository
- Join the Tapio community discussions