# My Custom Kubernetes Correlations

These are custom correlation patterns I've observed in my production cluster.

## Memory Leak Detection Pattern

When memory usage > 85% and keeps increasing for 10 minutes,
then this indicates a memory leak.

Root cause: Check for heap allocation patterns in the application.
Recommend: Enable memory profiling and check for unclosed resources.

Severity: high
Confidence: 85%
Category: memory

## Database Connection Cascade

When database latency > 500ms for 30 seconds,
then predict API errors will start appearing within 2 minutes.

I expect to see:
- Connection pool exhaustion
- Request timeouts increasing
- 5xx errors spiking

Root cause: Database is overwhelmed or network issues.
Fix: Scale database replicas or check for slow queries.

```yaml
severity: critical
confidence: 90
category: cascade_failure
```

## CPU Throttling Warning

If CPU usage > 90% and throttling is detected,
then the service is undersized for the workload.

Predict: Response times will degrade by 50% or more.
Recommendation: Increase CPU limits or add horizontal scaling.

Severity: medium

## Midnight Batch Job Impact

When time is between 00:00 and 02:00 and batch-processor CPU > 80%,
then other services may experience latency spikes.

This is expected behavior during nightly batch processing.
Action: Consider scheduling less critical batch jobs at different times.

```yaml
category: batch_processing
time_sensitive: true
severity: low
```

## Network Saturation Pattern

When network bytes out > 100MB/s and packet drops > 1%,
then network saturation is occurring.

Symptoms include:
- Increased retransmissions
- Connection timeouts
- Service mesh communication failures

Root cause: Network bandwidth limits reached.
Recommend: Enable network policies to limit non-critical traffic.

## Container Restart Loop

If container restarts > 5 in 10 minutes,
then the application is in a crash loop.

Common causes:
- Liveness probe failing
- Application startup issues
- Configuration errors

Recommendation: Check container logs for startup errors.
Severity: high

## Disk Space Crisis

When disk usage > 90% and increasing > 1GB per minute,
then predict disk full within 10 minutes.

This is a critical situation requiring immediate action.
Fix: Clean up logs, temp files, or add disk space.

```yaml
severity: critical
confidence: 95
auto_remediate: true
```

## Service Dependency Failure

When auth-service errors > 10 per second,
then I expect api-gateway errors within 30 seconds.

This indicates a service dependency cascade.
Root cause: Authentication service is down or overloaded.

Action: Check auth-service health and scale if needed.

## OOM Killer Prediction

If memory usage > 95% and memory request/limit ratio > 0.9,
then predict OOM kill within 5 minutes.

This pattern indicates imminent memory exhaustion.
Recommend: Immediate pod restart or memory limit increase.

Severity: critical
Category: memory