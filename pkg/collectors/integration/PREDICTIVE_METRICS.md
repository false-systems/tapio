# Predictive OTEL Metrics for Tapio

## Overview

The predictive metrics implementation extracts the core prediction functionality from the complex correlation system and integrates it with your production semantic correlation engine. It provides **future-looking metrics** that predict system states before they occur.

## Key Features

### 1. Resource Exhaustion Prediction
- **Memory Exhaustion ETA**: Predicts minutes until memory runs out
- **CPU Exhaustion ETA**: Predicts minutes until CPU saturation
- **Disk Exhaustion ETA**: Predicts minutes until disk space runs out

### 2. Cascade Failure Risk Assessment
- Combines multiple stress indicators
- Calculates probability of system-wide failure
- Updates in real-time based on trends

### 3. Capacity Remaining Predictions
- Shows predicted capacity across multiple time horizons:
  - 5 minutes
  - 1 hour
  - 1 day
- Separate predictions for memory, CPU, and disk

### 4. Performance Degradation Risk
- Composite risk score based on resource pressure
- Early warning for performance issues

## Architecture

```
SemanticCorrelationEngine
         |
         v
PredictiveMetrics
         |
    +---------+
    |         |
TrendTracker  OTEL Metrics
    |         |
    v         v
Predictions   Prometheus
```

## Core Components

### PredictiveMetrics
- Main engine for predictive analysis
- Manages trend trackers for different resources
- Exposes OTEL metrics for Prometheus

### TrendTracker
- Tracks metrics over time with sliding window
- Performs linear regression for trend analysis
- Calculates confidence scores

### Prediction Model
```go
type Prediction struct {
    Type           string        // Type of prediction
    Metric         string        // Metric being predicted
    CurrentValue   float64       // Current metric value
    PredictedValue float64       // Predicted future value
    TimeHorizon    time.Duration // How far in future
    Confidence     float64       // Confidence score (0-1)
    ETAMinutes     float64       // Minutes to threshold
    Probability    float64       // Probability of occurrence
}
```

## Integration with Correlation Engine

### Option 1: Direct Integration
```go
// In your SimpleManager
correlation := NewSemanticCorrelationEngine(batchSize, batchTimeout)

// Add predictive metrics
predictiveMetrics, _ := NewPredictiveMetrics()

// Process events through both
go func() {
    for event := range eventChan {
        // Normal correlation
        correlation.ProcessEvent(ctx, event)
        
        // Update predictions
        domainEvent := convertToDomainEvent(event)
        predictiveMetrics.ProcessEvent(ctx, domainEvent)
    }
}()

// Periodic prediction updates
ticker := time.NewTicker(30 * time.Second)
go func() {
    for range ticker.C {
        predictiveMetrics.UpdatePredictions()
    }
}()
```

### Option 2: Extended Engine (Recommended)
```go
// Replace your correlation engine creation
correlation, err := NewPredictiveCorrelationEngine(
    config.CorrelationBatchSize,
    config.CorrelationBatchTimeout,
)
```

## Exposed Metrics

### Memory Metrics
```promql
# Memory exhaustion ETA in minutes (0 = no exhaustion predicted)
tapio_memory_exhaustion_eta_minutes{confidence="0.85"}

# Memory capacity remaining at different time horizons
tapio_capacity_remaining_percent{resource="memory", time_horizon="5min"}
tapio_capacity_remaining_percent{resource="memory", time_horizon="1hr"}
tapio_capacity_remaining_percent{resource="memory", time_horizon="1day"}
```

### CPU Metrics
```promql
# CPU exhaustion ETA
tapio_cpu_exhaustion_eta_minutes

# CPU capacity remaining
tapio_capacity_remaining_percent{resource="cpu", time_horizon="5min"}
```

### Risk Metrics
```promql
# Cascade failure risk (0-1)
tapio_cascade_failure_risk{confidence="0.70"}

# Performance degradation risk (0-1)
tapio_performance_degradation_risk{calculation="composite"}

# Error rate prediction (errors per minute)
tapio_error_rate_prediction{time_horizon="15min"}
```

## Prometheus Alert Examples

```yaml
groups:
  - name: predictive_alerts
    rules:
      - alert: MemoryExhaustionImminent
        expr: tapio_memory_exhaustion_eta_minutes > 0 AND tapio_memory_exhaustion_eta_minutes < 30
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Memory exhaustion in {{ $value }} minutes"
          description: "{{ $labels.instance }} will run out of memory"
          
      - alert: HighCascadeFailureRisk
        expr: tapio_cascade_failure_risk > 0.7
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "Cascade failure risk: {{ $value | humanizePercentage }}"
          
      - alert: CapacityDepleting
        expr: tapio_capacity_remaining_percent{time_horizon="1hr"} < 20
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "{{ $labels.resource }} capacity < 20% in 1 hour"
```

## Grafana Dashboard Queries

### Resource Exhaustion Panel
```promql
# Show all exhaustion predictions
max by (resource) (
  label_replace(tapio_memory_exhaustion_eta_minutes, "resource", "memory", "", "") or
  label_replace(tapio_cpu_exhaustion_eta_minutes, "resource", "cpu", "", "") or
  label_replace(tapio_disk_exhaustion_eta_minutes, "resource", "disk", "", "")
)
```

### Capacity Forecast Panel
```promql
# Capacity remaining over time horizons
tapio_capacity_remaining_percent
```

### Risk Overview Panel
```promql
# All risk metrics
tapio_cascade_failure_risk or
tapio_performance_degradation_risk or
(tapio_error_rate_prediction / 100)  # Normalized to 0-1
```

## Configuration

### Update Intervals
- Prediction updates: 30 seconds (configurable)
- Trend window: 5 minutes for short-term, up to 24 hours for long-term
- Metric observation: Real-time via OTEL callbacks

### Thresholds
- Memory exhaustion: 95% usage
- CPU exhaustion: 90% usage
- Disk exhaustion: 90% usage
- High confidence: 0.7+

## Benefits

1. **Proactive Monitoring**: See problems before they happen
2. **Automated Insights**: Predictive insights trigger automatically
3. **Standard Prometheus**: Works with existing monitoring stack
4. **Low Overhead**: Efficient trend tracking and calculation
5. **Configurable**: Adjust thresholds and windows as needed

## Future Enhancements

1. **Machine Learning Models**: Replace linear regression with ML
2. **Seasonal Patterns**: Detect daily/weekly patterns
3. **Multi-Resource Correlation**: Predict based on resource interactions
4. **Custom Predictions**: User-defined prediction rules
5. **Historical Accuracy**: Track and improve prediction accuracy

## Example Output

When memory usage is trending toward exhaustion:
```
Metric: tapio_memory_exhaustion_eta_minutes
Value: 42.5
Labels: {confidence="0.85", time_horizon="predicted"}
```

This means:
- Memory will be exhausted in ~42.5 minutes
- Prediction confidence is 85%
- Based on current trend analysis