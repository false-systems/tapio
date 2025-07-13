# Production E-Commerce Cluster Opinions
Author: DevOps Team
Date: 2024-01-20
Cluster Type: production
Workload Type: stateless

## Overview

We run a high-traffic e-commerce platform with the following characteristics:
- Java-based microservices using Spring Boot
- PostgreSQL for persistent data
- Redis for caching and sessions
- Typical load: 10k requests/minute, peaks at 50k during sales
- Graceful shutdown is critical for payment processing

## 🧠 Memory Management

### General Guidelines
- **Acceptable memory usage**: 85%
  - Why: Our JVM apps with G1GC run stable at 80-82% under normal load
  - Buffer needed for garbage collection cycles
  
- **OOM prediction window**: 5 minutes
  - Why: Gives our on-call team enough time to respond
  - Allows horizontal autoscaling to kick in

### Service-Specific Memory Limits
- `payment-processor` pods can use up to **90% memory**
  - These are memory-optimized with careful heap sizing
- `batch-reports` pods can use up to **95% memory**
  - Run during off-hours, can use more resources
- `redis-cache` pods should never exceed **70% memory**
  - Redis needs headroom for copy-on-write during persistence

## 🔗 Correlation Windows

### Pod Lifecycle Events
- **OOM → Pod Restart**: 45 seconds
  - Why: Our pods need ~30s for graceful shutdown
  - Payment transactions must complete cleanly
  - Additional 15s buffer for Kubernetes operations

### Service Dependencies
When **postgres-primary** has issues, I expect to see:
- **api-gateway** errors within **10 seconds**
- **payment-service** errors within **15 seconds**
- **order-service** errors within **15 seconds**
- **notification-service** errors within **30 seconds**
- **analytics-service** errors within **60 seconds**

When **redis-cache** fails:
- **session-service** errors within **5 seconds**
- **api-gateway** degradation within **10 seconds**

## 🚨 Anomaly Detection

### Time-based Sensitivity

| Time Period | Sensitivity | Description |
|-------------|-------------|-------------|
| Business Hours (9-17 PST) | High (0.7) | Peak customer traffic, any anomaly matters |
| Evening (17-22 PST) | Medium (0.8) | Moderate traffic, some flexibility |
| Night (22-06 PST) | Low (0.9) | Batch processing, expect variations |
| Weekends | Medium (0.8) | Lower traffic but still customer-facing |
| Black Friday Week | Very High (0.6) | Zero tolerance for issues |
| Maintenance Windows | Very Low (0.95) | Expect unusual patterns |

### Anomaly Thresholds by Metric
- **Response time**: Alert when p95 > 500ms (normally ~200ms)
- **Error rate**: Alert when > 1% (normally < 0.1%)
- **CPU throttling**: Alert when > 10% of samples throttled

## ⚖️ Service Importance

```yaml
service_weights:
  # Critical - Customer facing, revenue impact
  payment-processor: 1.0      # Handles all payments
  api-gateway: 0.95          # All traffic flows through
  order-service: 0.95        # Core business function
  
  # Important - Degraded experience
  search-service: 0.8        # Customers can browse without search
  recommendation-api: 0.7    # Nice to have, not critical
  session-service: 0.8       # Can fallback to stateless
  
  # Standard - Internal services
  inventory-sync: 0.6        # Can lag a few minutes
  email-service: 0.5         # Queued, eventual delivery
  
  # Low Priority - Best effort
  analytics-worker: 0.3      # Batch processing
  log-aggregator: 0.2        # Debugging tool
  metrics-collector: 0.3     # Observability
```

## 📊 Behavioral Learning

### Learning Configuration
- **Learning window**: 14 days
  - Why: Captures weekly patterns and pay cycles
  - Includes at least one full business cycle
  
- **Minimum samples required**: 200
  - Why: Statistical significance for our traffic patterns
  
- **Behavioral deviation sensitivity**: 0.8
  - Why: Fairly sensitive but allows for promotional variations
  
- **Trend detection window**: 2 hours
  - Why: Long enough to identify real trends vs noise

### Known Patterns to Learn
Please learn and consider these normal patterns:
- **Monday morning spike**: 30% traffic increase 9-10 AM
- **Lunch time dip**: 20% decrease 12-1 PM
- **End of month**: 50% increase in batch job duration
- **Hourly cron jobs**: CPU spike at :00 and :30
- **Database backup**: High disk I/O at 2 AM daily
- **Cache warmup**: Memory spike after deployments

## 🔮 Prediction Settings

### What to Predict
- **OOM Events**: Yes, with 5-minute horizon
- **Cascade Failures**: Yes, identify spreading issues
- **Traffic Anomalies**: Yes, detect DDoS or flash sales

### Prediction Confidence
- **Minimum confidence for alerts**: 75%
- **Minimum confidence for auto-remediation**: 90%

## 🚀 Special Rules

### Deployment Awareness
- Ignore anomalies for 1 hour after deployment
- Expect 2x normal memory during rolling updates
- Pod startup takes up to 2 minutes (JVM warmup)

### Seasonal Adjustments
- **Black Friday**: Use special high-sensitivity profile
- **Christmas Week**: Expect 3x normal traffic
- **Super Bowl Sunday**: Traffic spike during commercials

### Integration Points
- **PagerDuty severity mapping**:
  - Critical (1.0 weight) → P1 incident
  - High (>0.8 weight) → P2 incident
  - Medium (>0.5 weight) → P3 incident
  - Low (<0.5 weight) → Ticket only

## 📝 Notes

This configuration is optimized for our e-commerce platform running on EKS with:
- Kubernetes 1.28
- Java 17 with G1GC
- 99.9% uptime SLA
- Zero-downtime deployment requirement

Last reviewed: 2024-01-20
Next review: 2024-04-20