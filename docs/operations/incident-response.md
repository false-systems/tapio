# Incident Response Runbook

## Overview

This runbook provides comprehensive procedures for responding to incidents in Tapio production environments. It covers incident classification, escalation procedures, investigation steps, and post-incident activities.

## Incident Classification

### Severity Levels

#### P1 - Critical (Service Down)
- **Impact**: Complete service unavailability
- **Response Time**: 5 minutes
- **Escalation**: Immediate management notification
- **Examples**:
  - All Tapio services returning 5xx errors
  - Complete cluster failure
  - Data corruption or loss
  - Security breach with active threat

#### P2 - High (Service Degraded)
- **Impact**: Significant service degradation
- **Response Time**: 15 minutes
- **Escalation**: Engineering team notification
- **Examples**:
  - High error rate (>5%)
  - Slow response times (>5 seconds)
  - Partial functionality unavailable
  - Performance below SLA thresholds

#### P3 - Medium (Service Impaired)
- **Impact**: Minor service degradation
- **Response Time**: 1 hour
- **Escalation**: Standard on-call procedures
- **Examples**:
  - Moderate error rate (1-5%)
  - Some features intermittently unavailable
  - Resource usage warnings
  - Non-critical component failures

#### P4 - Low (Informational)
- **Impact**: No immediate service impact
- **Response Time**: Next business day
- **Escalation**: Standard ticket queue
- **Examples**:
  - Log volume increases
  - Minor configuration drift
  - Scheduled maintenance reminders
  - Documentation updates needed

## Incident Response Process

### 1. Detection and Initial Response

#### Automated Detection
- Monitoring alerts trigger PagerDuty
- Slack notifications to #tapio-alerts
- Email notifications to on-call engineer

#### Manual Detection
- User reports via support channels
- Engineering team observations
- Customer escalations

#### Initial Response (First 5 minutes)
1. **Acknowledge the incident**
   ```bash
   # Check service status immediately
   kubectl get pods -n tapio-system
   tapio check --timeout=30s
   ```

2. **Create incident ticket**
   - Use incident management system
   - Include initial symptoms
   - Set appropriate severity

3. **Notify stakeholders**
   - Post to #incident-response Slack channel
   - Page additional engineers if P1/P2
   - Update status page if customer-facing

### 2. Investigation and Diagnosis

#### Information Gathering
1. **Collect system status**
   ```bash
   # Pod status
   kubectl get pods -n tapio-system -o wide
   
   # Recent events
   kubectl get events -n tapio-system --sort-by='.lastTimestamp' | tail -20
   
   # Resource usage
   kubectl top nodes
   kubectl top pods -n tapio-system
   ```

2. **Review recent changes**
   ```bash
   # Deployment history
   kubectl rollout history deployment/tapio-server -n tapio-system
   
   # Git commits
   git log --oneline --since="2 hours ago"
   
   # Configuration changes
   kubectl diff -f deploy/kubernetes/
   ```

3. **Analyze logs**
   ```bash
   # Application logs
   kubectl logs -f deployment/tapio-server -n tapio-system --tail=100
   
   # System logs
   kubectl logs -f daemonset/tapio-agent -n tapio-system --tail=100
   
   # Error patterns
   kubectl logs --since=1h -l app=tapio-server -n tapio-system | grep ERROR
   ```

4. **Check metrics**
   - Review Grafana dashboards
   - Analyze Prometheus metrics
   - Check external monitoring tools

#### Root Cause Analysis
1. **Timeline reconstruction**
   - When did the incident start?
   - What changed around that time?
   - How did the issue propagate?

2. **Component analysis**
   - Which components are affected?
   - Are there dependency failures?
   - Is this a cascading failure?

3. **Hypothesis formation**
   - What are the likely causes?
   - How can each hypothesis be tested?
   - What evidence supports each theory?

### 3. Immediate Mitigation

#### Service Restoration Actions

**For Application Issues:**
```bash
# Restart problematic pods
kubectl delete pod <pod-name> -n tapio-system

# Scale up if performance issue
kubectl scale deployment tapio-server --replicas=10 -n tapio-system

# Rollback if recent deployment issue
kubectl rollout undo deployment/tapio-server -n tapio-system
```

**For Resource Issues:**
```bash
# Increase resource limits
kubectl patch deployment tapio-server -n tapio-system -p '{"spec":{"template":{"spec":{"containers":[{"name":"server","resources":{"limits":{"cpu":"4","memory":"8Gi"}}}]}}}}'

# Clear disk space if needed
kubectl exec -it <pod-name> -n tapio-system -- df -h
kubectl exec -it <pod-name> -n tapio-system -- find /var/log -name "*.log" -mtime +7 -delete
```

**For Security Issues:**
```bash
# Block malicious IPs
kubectl apply -f security/network-policies/emergency-block.yaml

# Disable compromised accounts
kubectl patch secret tapio-auth -n tapio-system --type='json' -p='[{"op": "remove", "path": "/data/compromised-user"}]'

# Enable additional logging
kubectl patch configmap tapio-config -n tapio-system --type='json' -p='[{"op": "replace", "path": "/data/log-level", "value": "DEBUG"}]'
```

#### Load Shedding
```bash
# Enable emergency load shedding
kubectl patch configmap tapio-config -n tapio-system --type='json' -p='[{"op": "replace", "path": "/data/load-shedding-enabled", "value": "true"}]'

# Reduce processing complexity
kubectl patch configmap tapio-config -n tapio-system --type='json' -p='[{"op": "replace", "path": "/data/correlation-depth", "value": "basic"}]'
```

### 4. Communication

#### Internal Communication
- **Slack Updates**: Post to #incident-response every 15 minutes
- **Management Briefing**: For P1/P2 incidents within 30 minutes
- **Engineering Team**: Keep informed of investigation progress

#### External Communication
- **Status Page**: Update for customer-facing incidents
- **Customer Notifications**: Direct communication for high-impact customers
- **Support Team**: Brief on customer impact and expected resolution

#### Communication Templates

**Initial Notification:**
```
🚨 INCIDENT: [P1/P2/P3] - [Brief Description]
Started: [Timestamp]
Impact: [Customer impact description]
Status: Investigating
Lead: @[engineer-name]
Thread: [link to thread]
```

**Update Notification:**
```
📊 UPDATE: [Incident ID]
Status: [Current status]
Progress: [What we've learned/done]
Next Steps: [What we're doing next]
ETA: [Estimated resolution time]
```

**Resolution Notification:**
```
✅ RESOLVED: [Incident ID]
Duration: [Total time]
Cause: [Root cause summary]
Resolution: [What fixed it]
Follow-up: [Post-incident items]
```

### 5. Resolution and Recovery

#### Service Verification
1. **Health Checks**
   ```bash
   # Verify all pods healthy
   kubectl get pods -n tapio-system
   
   # Check health endpoints
   kubectl exec -it deployment/tapio-server -n tapio-system -- curl http://localhost:8080/health
   
   # Test functionality
   tapio check --all
   ```

2. **Performance Validation**
   - Monitor response times
   - Check error rates
   - Verify throughput metrics
   - Validate resource usage

3. **Data Integrity**
   - Check for data loss
   - Verify recent events processed
   - Validate correlation accuracy

#### Cleanup Actions
```bash
# Remove emergency configurations
kubectl delete -f security/network-policies/emergency-block.yaml

# Restore normal resource limits
kubectl apply -f deploy/kubernetes/production/

# Clear temporary debugging
kubectl patch configmap tapio-config -n tapio-system --type='json' -p='[{"op": "replace", "path": "/data/log-level", "value": "INFO"}]'
```

## Specific Incident Scenarios

### Scenario 1: Complete Service Outage

**Symptoms:**
- All Tapio endpoints returning 503
- Health checks failing
- No event processing

**Response Checklist:**
- [ ] Check Kubernetes cluster health
- [ ] Verify external dependencies
- [ ] Review recent deployments
- [ ] Check resource availability
- [ ] Examine security events

**Common Causes & Solutions:**
1. **Recent Deployment Issue**
   - Solution: Immediate rollback
   - Command: `kubectl rollout undo deployment/tapio-server -n tapio-system`

2. **Resource Exhaustion**
   - Solution: Scale up or clear resources
   - Commands: Scale deployment, clean up disk space

3. **External Dependency Failure**
   - Solution: Implement circuit breaker or failover
   - Action: Switch to backup services

### Scenario 2: High Error Rate

**Symptoms:**
- Error rate >5% for sustained period
- Increased 5xx responses
- User complaints

**Response Checklist:**
- [ ] Identify error patterns
- [ ] Check recent changes
- [ ] Analyze error logs
- [ ] Review performance metrics
- [ ] Validate external services

**Investigation Steps:**
1. **Error Analysis**
   ```bash
   # Get error breakdown
   kubectl logs --since=1h deployment/tapio-server -n tapio-system | grep ERROR | sort | uniq -c | sort -nr
   
   # Check specific error details
   kubectl logs --since=30m deployment/tapio-server -n tapio-system | grep "specific_error_pattern"
   ```

2. **Performance Analysis**
   - Review response time metrics
   - Check resource utilization
   - Analyze request patterns

### Scenario 3: Security Incident

**Symptoms:**
- Suspicious authentication attempts
- Unauthorized data access
- Unusual system behavior

**Response Checklist:**
- [ ] Preserve evidence
- [ ] Contain the threat
- [ ] Notify security team
- [ ] Document everything
- [ ] Coordinate with legal if needed

**Immediate Actions:**
1. **Containment**
   ```bash
   # Block suspicious IPs
   kubectl apply -f security/emergency-policies/
   
   # Disable compromised accounts
   kubectl patch secret user-credentials -n tapio-system --type='json' -p='[{"op": "remove", "path": "/data/compromised-user"}]'
   
   # Enable audit logging
   kubectl patch configmap tapio-config -n tapio-system --type='json' -p='[{"op": "replace", "path": "/data/audit-level", "value": "VERBOSE"}]'
   ```

2. **Evidence Preservation**
   ```bash
   # Backup current logs
   kubectl logs deployment/tapio-server -n tapio-system > incident-logs-$(date +%Y%m%d-%H%M).log
   
   # Export security events
   kubectl logs --since=24h -l component=security-auditor -n tapio-system > security-events-$(date +%Y%m%d).log
   ```

## Post-Incident Activities

### 1. Immediate Post-Resolution (Within 1 hour)

- [ ] Update status page to "Resolved"
- [ ] Notify all stakeholders
- [ ] Document final resolution steps
- [ ] Schedule post-mortem meeting
- [ ] Create follow-up tickets

### 2. Post-Mortem Process

#### Timeline Documentation
- Incident start time
- Detection time
- Response time
- Resolution time
- Communication timeline

#### Root Cause Analysis
- What was the root cause?
- Why wasn't it caught earlier?
- What made it worse?
- What made it better?

#### Action Items
- Immediate fixes needed
- Monitoring improvements
- Process improvements
- Documentation updates

### 3. Follow-Up Actions

#### Technical Improvements
- Code changes to prevent recurrence
- Monitoring enhancements
- Alerting improvements
- Documentation updates

#### Process Improvements
- Runbook updates
- Training needs
- Tool improvements
- Communication enhancements

## Incident Metrics

### Key Performance Indicators

- **Mean Time to Detection (MTTD)**: Time from incident start to detection
- **Mean Time to Acknowledgment (MTTA)**: Time from detection to response
- **Mean Time to Resolution (MTTR)**: Time from detection to resolution
- **Mean Time Between Failures (MTBF)**: Time between incidents

### Target Metrics

- P1 incidents: MTTA < 5 minutes, MTTR < 2 hours
- P2 incidents: MTTA < 15 minutes, MTTR < 8 hours
- P3 incidents: MTTA < 1 hour, MTTR < 24 hours

### Reporting

- Weekly incident summary
- Monthly trend analysis
- Quarterly improvement review
- Annual reliability report

## Tools and Resources

### Incident Management Tools
- **PagerDuty**: Alerting and escalation
- **Slack**: Real-time communication
- **Jira**: Incident tracking
- **Confluence**: Documentation

### Monitoring and Diagnostics
- **Grafana**: Metrics visualization
- **Kibana**: Log analysis
- **Jaeger**: Distributed tracing
- **kubectl**: Kubernetes management

### Communication Channels
- **#incident-response**: Primary incident channel
- **#tapio-alerts**: Automated alerts
- **#engineering**: Engineering team updates
- **#customer-success**: Customer impact updates

### External Resources
- [Kubernetes Troubleshooting Guide](https://kubernetes.io/docs/tasks/debug/)
- [Prometheus Query Examples](https://prometheus.io/docs/prometheus/latest/querying/examples/)
- [Go Debugging Guide](https://golang.org/doc/gdb)