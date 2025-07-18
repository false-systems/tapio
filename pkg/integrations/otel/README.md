# OTEL Integration

This module provides OpenTelemetry integration for Tapio, featuring the revolutionary capabilities extracted during the Great Correlation Massacre:

## 🚀 Revolutionary Features

### 1. Semantic Trace Correlation
- Automatically correlates events across distributed systems
- Adds semantic context to traces for human understanding
- Links related spans based on Tapio's correlation engine

### 2. Predictive OTEL Metrics (First of its Kind!)
- Exports predictive analytics as OTEL metrics
- Tracks confidence levels and contributing factors
- Enables proactive monitoring and alerting

### 3. Human-Readable Context Generation
- Automatically generates descriptive span names
- Adds human-friendly attributes to traces
- Makes distributed tracing accessible to non-experts

## Architecture

This integration is at Level 3 of Tapio's architecture:

```
Dependencies:
- pkg/domain (Level 0) - Core types
- pkg/collectors/* (Level 1) - Event sources
- pkg/intelligence/* (Level 2) - Correlation and prediction
```

## Usage

```go
// Initialize OTEL exporter
config := &OTELConfig{
    Endpoint: "localhost:4317",
    EnablePredictiveMetrics: true,
    EnableSemanticEnrichment: true,
}

exporter, err := NewOTELExporter(ctx, config)
if err != nil {
    return err
}
defer exporter.Close()

// Export events with semantic enrichment
err = exporter.ExportEvent(ctx, event)

// Export correlations as linked traces
err = exporter.ExportCorrelation(ctx, correlation)

// Export predictive metrics
prediction := PredictiveMetrics{
    Type: "oom",
    PredictedTime: time.Now().Add(30*time.Minute).Unix(),
    Confidence: 0.85,
}
err = exporter.ExportPrediction(ctx, prediction)
```

## Implementation Status

- [ ] Core OTEL exporter
- [ ] Semantic trace enrichment
- [ ] Predictive metrics exporter
- [ ] Human-readable context generator
- [ ] Integration tests
- [ ] Performance benchmarks