# Monitoring and Alerting Runbook

## Overview

This runbook provides comprehensive guidance for monitoring Tapio in production environments, including dashboard usage, alert response procedures, and performance optimization.

## Dashboard Overview

### Primary Dashboard - Tapio Overview

**URL**: http://grafana.monitoring.svc.cluster.local:3000/d/tapio-overview

#### Key Panels

1. **Event Processing**
   - Events Processed/sec
   - Event Processing Latency
   - Event Queue Depth
   - Filter Efficiency Rate

2. **Data Quality**
   - Signal-to-Noise Ratio
   - Correlation Accuracy
   - False Positive Rate
   - Coverage Percentage

3. **System Health**
   - Pod Status
   - CPU Usage
   - Memory Usage
   - Network I/O

4. **Business Metrics**
   - Active Correlations
   - Insights Generated
   - User Interactions
   - API Response Times

### Secondary Dashboards

- **eBPF Collector Dashboard**: Kernel-level metrics
- **Correlation Engine Dashboard**: ML pipeline performance
- **Security Dashboard**: Audit logs and security events
- **Infrastructure Dashboard**: Kubernetes cluster health

## Alert Response Procedures

### Critical Alerts

#### ALERT: Service Down

**Severity**: P1 - Critical
**Response Time**: 5 minutes

**Symptoms**:
- Tapio service returns 5xx errors
- Health check endpoints failing
- No event processing

**Investigation Steps**:
1. Check pod status
   ```bash
   kubectl get pods -n tapio-system
   kubectl describe pod <failing-pod> -n tapio-system
   ```

2. Review recent deployments
   ```bash
   kubectl rollout history deployment/tapio-server -n tapio-system
   ```

3. Check system resources
   ```bash
   kubectl top nodes
   kubectl top pods -n tapio-system
   ```

**Resolution Steps**:
1. If recent deployment: Rollback immediately
   ```bash
   kubectl rollout undo deployment/tapio-server -n tapio-system
   ```

2. If resource exhaustion: Scale up or restart
   ```bash
   kubectl scale deployment tapio-server --replicas=5 -n tapio-system
   ```

3. If persistent issues: Contact on-call engineer

**Post-Incident**:
- Document root cause
- Update runbook if needed
- Schedule post-mortem meeting

#### ALERT: High Error Rate

**Severity**: P2 - High
**Response Time**: 15 minutes

**Symptoms**:
- Error rate > 5% for 5+ minutes
- Increased 4xx/5xx responses
- User complaints about functionality

**Investigation Steps**:
1. Check error logs
   ```bash
   kubectl logs -f deployment/tapio-server -n tapio-system | grep ERROR
   ```

2. Review Grafana error breakdown panel

3. Check external dependencies
   ```bash
   kubectl get endpoints -n tapio-system
   nslookup kubernetes.default.svc.cluster.local
   ```

**Resolution Steps**:
1. If transient issue: Monitor for auto-recovery
2. If persistent: Restart affected components
3. If dependency issue: Contact relevant team

#### ALERT: Event Processing Lag

**Severity**: P2 - High
**Response Time**: 15 minutes

**Symptoms**:
- Event queue depth increasing
- Processing latency > 5 seconds
- Real-time insights delayed

**Investigation Steps**:
1. Check eBPF collector performance
   ```bash
   kubectl logs -f daemonset/tapio-agent -n tapio-system | grep "events/sec"
   ```

2. Review correlation engine logs
   ```bash
   kubectl logs -f deployment/tapio-server -n tapio-system | grep correlation
   ```

3. Monitor resource usage patterns

**Resolution Steps**:
1. Scale up correlation engine
   ```bash
   kubectl scale deployment tapio-server --replicas=10 -n tapio-system
   ```

2. Increase resource limits if needed
3. Enable load shedding temporarily

### Warning Alerts

#### ALERT: High Resource Usage

**Severity**: P3 - Medium
**Response Time**: 1 hour

**Symptoms**:
- CPU usage > 80% for 15+ minutes
- Memory usage > 90% for 10+ minutes
- Disk usage > 85%

**Investigation Steps**:
1. Identify resource-intensive pods
   ```bash
   kubectl top pods -n tapio-system --sort-by=cpu
   kubectl top pods -n tapio-system --sort-by=memory
   ```

2. Review resource trends in Grafana

3. Check for memory leaks or CPU spikes

**Resolution Steps**:
1. Adjust resource requests/limits
2. Scale horizontally if needed
3. Investigate optimization opportunities

#### ALERT: Low Data Quality

**Severity**: P3 - Medium
**Response Time**: 2 hours

**Symptoms**:
- Signal-to-noise ratio < 95%
- False positive rate > 2%
- Correlation accuracy < 98%

**Investigation Steps**:
1. Review data quality metrics dashboard
2. Check recent configuration changes
3. Analyze event patterns and sources

**Resolution Steps**:
1. Tune filtering algorithms
2. Update correlation rules
3. Recalibrate ML models

## Performance Optimization

### CPU Optimization

1. **Profiling**
   ```bash
   # Enable CPU profiling
   kubectl port-forward deployment/tapio-server 6060:6060 -n tapio-system
   go tool pprof http://localhost:6060/debug/pprof/profile
   ```

2. **Optimization Actions**
   - Adjust goroutine pools
   - Optimize hot code paths
   - Reduce memory allocations

### Memory Optimization

1. **Memory Profiling**
   ```bash
   # Check memory usage
   go tool pprof http://localhost:6060/debug/pprof/heap
   ```

2. **Optimization Actions**
   - Implement object pooling
   - Reduce GC pressure
   - Optimize data structures

### eBPF Performance

1. **Kernel Space Optimization**
   - Optimize eBPF programs
   - Reduce map lookups
   - Implement efficient filtering

2. **User Space Optimization**
   - Batch event processing
   - Optimize ring buffer usage
   - Reduce context switches

## Capacity Planning

### Growth Metrics

Monitor these metrics for capacity planning:

- Events processed per second (trend over time)
- Storage growth rate
- CPU/memory usage trends
- Network bandwidth utilization

### Scaling Triggers

**Scale Up When**:
- CPU usage > 70% for 1+ hours
- Memory usage > 80% for 30+ minutes
- Event queue depth consistently > 1000

**Scale Down When**:
- CPU usage < 30% for 4+ hours
- Memory usage < 50% for 2+ hours
- Event queue depth consistently < 100

### Scaling Commands

```bash
# Scale server components
kubectl scale deployment tapio-server --replicas=10 -n tapio-system

# Scale agent components (if using deployment instead of daemonset)
kubectl scale deployment tapio-agent --replicas=20 -n tapio-system

# Update resource limits
kubectl patch deployment tapio-server -n tapio-system -p '{"spec":{"template":{"spec":{"containers":[{"name":"server","resources":{"limits":{"cpu":"2","memory":"4Gi"}}}]}}}}'
```

## Log Analysis

### Log Levels

- **ERROR**: Service failures, critical issues
- **WARN**: Performance degradation, configuration issues
- **INFO**: Normal operations, significant events
- **DEBUG**: Detailed troubleshooting information

### Log Queries

#### ELK Stack Queries

```json
// High error rate
{
  "query": {
    "bool": {
      "must": [
        {"match": {"kubernetes.namespace": "tapio-system"}},
        {"match": {"level": "ERROR"}},
        {"range": {"@timestamp": {"gte": "now-1h"}}}
      ]
    }
  }
}

// Performance issues
{
  "query": {
    "bool": {
      "must": [
        {"match": {"kubernetes.namespace": "tapio-system"}},
        {"range": {"response_time": {"gte": 5000}}},
        {"range": {"@timestamp": {"gte": "now-30m"}}}
      ]
    }
  }
}
```

#### Command Line Log Analysis

```bash
# Check for errors in last hour
kubectl logs --since=1h -l app=tapio-server -n tapio-system | grep ERROR

# Monitor real-time events
kubectl logs -f deployment/tapio-server -n tapio-system | grep -E "(ERROR|WARN)"

# Analyze performance patterns
kubectl logs --since=1h deployment/tapio-server -n tapio-system | grep "response_time" | awk '{print $5}' | sort -n
```

## Security Monitoring

### Security Events to Monitor

1. **Authentication Failures**
   - Failed login attempts
   - Invalid tokens
   - Brute force attacks

2. **Authorization Violations**
   - Access denied events
   - Privilege escalation attempts
   - Unusual access patterns

3. **Data Access**
   - Large data exports
   - Unusual query patterns
   - Off-hours access

### Security Dashboards

- **Authentication Dashboard**: Login patterns, failures
- **Authorization Dashboard**: Access control events
- **Audit Dashboard**: Complete audit trail
- **Threat Detection Dashboard**: Security anomalies

### Incident Response

#### Security Incident Detected

1. **Immediate Actions**
   - Document the incident
   - Preserve logs and evidence
   - Notify security team

2. **Investigation**
   - Analyze audit logs
   - Check for data exfiltration
   - Identify attack vectors

3. **Containment**
   - Block malicious IPs
   - Disable compromised accounts
   - Isolate affected systems

## Maintenance Windows

### Weekly Maintenance

**Schedule**: Every Sunday 2:00-4:00 AM UTC

**Activities**:
- Log rotation and cleanup
- Certificate renewal checks
- Security patch assessment
- Performance review

### Monthly Maintenance

**Schedule**: First Saturday of each month 6:00-10:00 AM UTC

**Activities**:
- Full backup verification
- Disaster recovery testing
- Capacity planning review
- Security audit

### Quarterly Maintenance

**Schedule**: End of each quarter, scheduled separately

**Activities**:
- Major version upgrades
- Architecture reviews
- Business continuity testing
- Full security assessment

## Emergency Contacts

### Escalation Matrix

**Level 1**: Operations Team
- Slack: #tapio-ops
- Phone: +1-xxx-xxx-xxxx

**Level 2**: Engineering Team
- Slack: #tapio-engineering
- Email: engineering@company.com

**Level 3**: Management
- Phone: +1-xxx-xxx-xxxx
- Email: management@company.com

### External Contacts

- **Cloud Provider**: [Support phone/portal]
- **Security Vendor**: [Security team contact]
- **Network Provider**: [Network operations center]

## Tools and Resources

### Monitoring Tools

- **Grafana**: Metrics visualization
- **Prometheus**: Metrics collection
- **ELK Stack**: Log aggregation and analysis
- **Jaeger**: Distributed tracing

### CLI Tools

- **kubectl**: Kubernetes management
- **tapio**: Application-specific commands
- **curl**: HTTP testing
- **jq**: JSON processing

### Documentation

- [Grafana Dashboard Guide](./grafana-guide.md)
- [Alert Configuration](./alert-config.md)
- [Troubleshooting Guide](./troubleshooting.md)
- [Architecture Documentation](../architecture/)