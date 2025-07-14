# Updated Correlation Rules

## Memory Leak Detection Pattern

When memory usage > 90% and keeps increasing for 15 minutes,
then this indicates a severe memory leak requiring immediate attention.

Root cause: Check for heap allocation patterns and memory retention issues.
Recommend: Enable memory profiling, check for unclosed resources, and consider memory dumps.
Predict: System will become unresponsive within 5 minutes without intervention.

Severity: critical
Confidence: 95%
Category: memory

## New Database Performance Pattern

When database query time > 2000ms for 1 minute,
then database performance is severely degraded.

This indicates potential index issues or query optimization problems.
Fix: Check slow query logs and analyze execution plans.
Recommend: Add missing indexes or optimize problematic queries.

```yaml
severity: high
confidence: 88
category: database_performance
custom_field: production_critical
```