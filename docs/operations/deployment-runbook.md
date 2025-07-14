# Deployment Runbook

## Overview

This runbook provides step-by-step procedures for deploying Tapio in production environments. It covers pre-deployment validation, deployment execution, post-deployment verification, and rollback procedures.

## Pre-Deployment Checklist

### 1. Environment Validation

- [ ] Kubernetes cluster health verified
- [ ] Required namespaces exist
- [ ] RBAC permissions configured
- [ ] Storage classes available
- [ ] Network policies configured
- [ ] External dependencies accessible

### 2. Configuration Validation

- [ ] Configuration files reviewed and approved
- [ ] Secrets and ConfigMaps prepared
- [ ] Resource limits and requests set
- [ ] Security policies enabled
- [ ] Monitoring configuration validated

### 3. Backup Verification

- [ ] Current configuration backed up
- [ ] Database backup completed (if applicable)
- [ ] Previous deployment artifacts available
- [ ] Rollback plan documented

## Deployment Procedures

### Rolling Deployment

1. **Deploy New Version**
   ```bash
   # Apply new configuration
   kubectl apply -f deploy/kubernetes/

   # Monitor deployment progress
   kubectl rollout status deployment/tapio-agent -n tapio-system
   kubectl rollout status deployment/tapio-server -n tapio-system
   ```

2. **Verify Deployment**
   ```bash
   # Check pod status
   kubectl get pods -n tapio-system -o wide

   # Check service endpoints
   kubectl get endpoints -n tapio-system

   # Verify logs
   kubectl logs -f deployment/tapio-agent -n tapio-system
   ```

### Blue-Green Deployment

1. **Prepare Green Environment**
   ```bash
   # Create green deployment
   kubectl apply -f deploy/kubernetes/blue-green/green/

   # Wait for green pods to be ready
   kubectl wait --for=condition=Ready pods -l version=green -n tapio-system
   ```

2. **Switch Traffic**
   ```bash
   # Update service to point to green
   kubectl patch service tapio-server -n tapio-system -p '{"spec":{"selector":{"version":"green"}}}'

   # Verify traffic switch
   kubectl describe service tapio-server -n tapio-system
   ```

3. **Clean Up Blue**
   ```bash
   # After verification, remove blue deployment
   kubectl delete deployment tapio-agent-blue -n tapio-system
   kubectl delete deployment tapio-server-blue -n tapio-system
   ```

### Canary Deployment

1. **Deploy Canary**
   ```bash
   # Deploy canary version (10% traffic)
   kubectl apply -f deploy/kubernetes/canary/

   # Verify canary pods
   kubectl get pods -l version=canary -n tapio-system
   ```

2. **Monitor Metrics**
   ```bash
   # Check error rates
   kubectl logs -f deployment/tapio-server-canary -n tapio-system | grep ERROR

   # Monitor performance metrics in Grafana
   # URL: http://grafana.monitoring.svc.cluster.local:3000
   ```

3. **Scale Up or Rollback**
   ```bash
   # If successful, scale up canary
   kubectl scale deployment tapio-server-canary --replicas=5 -n tapio-system

   # If issues, rollback
   kubectl delete -f deploy/kubernetes/canary/
   ```

## Post-Deployment Verification

### 1. Health Checks

```bash
# Check all pods are running
kubectl get pods -n tapio-system

# Verify health endpoints
kubectl exec -it deployment/tapio-server -n tapio-system -- curl http://localhost:8080/health

# Check readiness probes
kubectl describe pods -l app=tapio-server -n tapio-system | grep Readiness
```

### 2. Functional Verification

```bash
# Test basic functionality
tapio check --all

# Verify eBPF collection
kubectl logs -f daemonset/tapio-agent -n tapio-system | grep "eBPF collector"

# Check correlation engine
kubectl logs -f deployment/tapio-server -n tapio-system | grep "correlation"
```

### 3. Performance Validation

- Monitor CPU and memory usage
- Check event processing rates
- Verify response times
- Validate resource utilization

### 4. Security Verification

- Confirm TLS certificates are valid
- Verify authentication is working
- Check audit logs are being generated
- Validate RBAC permissions

## Rollback Procedures

### Automatic Rollback

```bash
# Rollback to previous version
kubectl rollout undo deployment/tapio-agent -n tapio-system
kubectl rollout undo deployment/tapio-server -n tapio-system

# Check rollback status
kubectl rollout status deployment/tapio-agent -n tapio-system
```

### Manual Rollback

```bash
# List deployment history
kubectl rollout history deployment/tapio-server -n tapio-system

# Rollback to specific revision
kubectl rollout undo deployment/tapio-server --to-revision=2 -n tapio-system

# Verify rollback
kubectl get deployment tapio-server -n tapio-system -o yaml | grep image:
```

## Emergency Procedures

### Complete Service Outage

1. **Immediate Response**
   ```bash
   # Scale down problematic deployment
   kubectl scale deployment tapio-server --replicas=0 -n tapio-system

   # Check cluster status
   kubectl get nodes
   kubectl top nodes
   ```

2. **Restore Service**
   ```bash
   # Restore from backup
   kubectl apply -f backup/last-known-good/

   # Verify restoration
   tapio check --timeout=30s
   ```

### Partial Service Degradation

1. **Isolate Problem**
   ```bash
   # Check specific pods
   kubectl describe pod <problematic-pod> -n tapio-system

   # Check events
   kubectl get events -n tapio-system --sort-by='.lastTimestamp'
   ```

2. **Mitigation**
   ```bash
   # Restart problematic pods
   kubectl delete pod <problematic-pod> -n tapio-system

   # Adjust resource limits if needed
   kubectl patch deployment tapio-agent -n tapio-system -p '{"spec":{"template":{"spec":{"containers":[{"name":"agent","resources":{"limits":{"memory":"512Mi"}}}]}}}}'
   ```

## Monitoring During Deployment

### Key Metrics to Watch

- **Pod Status**: All pods should be Running and Ready
- **Event Processing Rate**: Should maintain normal levels
- **Error Rate**: Should remain below 1%
- **Response Time**: Should stay under 2 seconds
- **Resource Usage**: CPU < 80%, Memory < 90%

### Alerting Thresholds

- Critical: Pod crash loops, service unavailable
- Warning: High error rate, slow response times
- Info: Deployment started, rollback initiated

## Troubleshooting

### Common Issues

1. **Pods Not Starting**
   ```bash
   kubectl describe pod <pod-name> -n tapio-system
   kubectl logs <pod-name> -n tapio-system --previous
   ```

2. **Service Discovery Issues**
   ```bash
   kubectl get endpoints -n tapio-system
   kubectl describe service tapio-server -n tapio-system
   ```

3. **eBPF Permission Issues**
   ```bash
   # Check privileged mode
   kubectl get pod <agent-pod> -n tapio-system -o yaml | grep privileged

   # Verify security context
   kubectl describe pod <agent-pod> -n tapio-system | grep "Security Context"
   ```

### Log Analysis

```bash
# Application logs
kubectl logs -f deployment/tapio-server -n tapio-system

# System logs
kubectl logs -f daemonset/tapio-agent -n tapio-system

# Audit logs
kubectl logs -f deployment/tapio-server -n tapio-system | grep AUDIT
```

## Contact Information

- **On-Call Engineer**: [Phone/Slack]
- **Platform Team**: [Email/Slack]
- **Security Team**: [Email] (for security incidents)
- **Management**: [Email] (for major outages)

## Documentation Links

- [Monitoring Dashboard](http://grafana.monitoring.svc.cluster.local:3000)
- [Log Aggregation](http://kibana.logging.svc.cluster.local:5601)
- [Incident Management](http://pagerduty.com/incidents)
- [Architecture Documentation](../architecture/README.md)