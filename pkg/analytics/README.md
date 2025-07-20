# Tapio Analytics Engine

High-performance analytics engine for processing UnifiedEvents with semantic correlation and real-time analysis.

## Overview

The analytics engine processes up to 165k events/second using a multi-stage pipeline architecture with OTEL trace context propagation and semantic correlation.

## Architecture

```
UnifiedEvent → Validation → Enrichment → Correlation → Analytics → Result
                   ↓           ↓            ↓            ↓
                Validate    Add Context  Group Events  Score & Assess
```

## Components

### Core Engine (`engine/analytics_engine.go`)
- Manages event pipeline with configurable workers
- Integrates with correlation engine and semantic tracer
- Provides real-time and batch processing APIs
- Tracks comprehensive metrics

### Real-Time Processor (`engine/realtime_processor.go`)
- Extracts OTEL trace context
- Processes semantic information
- Handles layer-specific data (kernel, network, app, k8s)
- Tracks processing rate

### Confidence Scorer (`engine/confidence_scorer.go`)
- Calculates event confidence based on:
  - Trace context completeness
  - Semantic information quality
  - Entity context
  - Impact assessment
  - Correlation patterns

### Impact Assessment (`engine/impact_assessment.go`)
- Evaluates business and technical impact
- Calculates cascade risk
- Generates actionable recommendations
- Layer-specific impact analysis

### Processing Stages (`engine/stages.go`)

1. **ValidationStage**: Validates required fields and data integrity
2. **EnrichmentStage**: Adds missing context (trace, semantic, severity)
3. **CorrelationStage**: Groups related events and detects patterns
4. **AnalyticsStage**: Scores events and detects anomalies

## Usage

```go
// Create engine with default config
config := engine.DefaultConfig()
logger := zap.NewProduction()
analyticsEngine, err := engine.NewAnalyticsEngine(config, logger)

// Start the engine
err = analyticsEngine.Start()

// Process a UnifiedEvent
result, err := analyticsEngine.ProcessEvent(ctx, unifiedEvent)

// Process batch
results, err := analyticsEngine.ProcessBatch(ctx, events)

// Get real-time results stream
resultStream := analyticsEngine.GetAnalyticsStream()
for result := range resultStream {
    // Handle analytics results
}

// Get metrics
metrics := analyticsEngine.GetMetrics()
```

## Configuration

```go
type Config struct {
    MaxEventsPerSecond       int           // Target: 165000
    BatchSize                int           // Default: 100
    FlushInterval            time.Duration // Default: 100ms
    WorkerCount              int           // Default: 8
    EnableSemanticGrouping   bool          // Default: true
    ConfidenceThreshold      float64       // Default: 0.7
    EnableRealTimeAnalysis   bool          // Default: true
    EnableImpactAssessment   bool          // Default: true
}
```

## Analytics Result

```go
type AnalyticsResult struct {
    EventID          string
    Timestamp        time.Time
    CorrelationID    string
    SemanticGroupID  string
    ConfidenceScore  float64
    ImpactAssessment *ImpactResult
    PredictedOutcome *PredictionResult
    RelatedEvents    []string
    AnalysisLatency  time.Duration
    Metadata         map[string]interface{}
}
```

## Performance

- **Throughput**: 165k+ events/second
- **Latency**: < 1ms per event (p99)
- **Memory**: Ring buffers with zero-copy optimization
- **CPU**: Worker affinity for cache locality

## Integration

The analytics engine integrates with:
- OTEL trace propagation
- Semantic correlation engine
- Event pipeline with configurable stages
- gRPC streaming services

## Metrics

Available metrics:
- Events processed
- Correlations found
- Semantic groups created
- Analysis latency
- Pipeline throughput
- Queue depth
- Output backlog