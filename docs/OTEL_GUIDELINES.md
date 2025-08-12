# OpenTelemetry (OTEL) Implementation Guidelines for Tapio

## 🎯 Core Principles

### 1. Type Safety First
**NEVER use `map[string]interface{}` or `interface{}` in public APIs**

```go
// ❌ BAD - Avoid untyped maps
func ProcessMetrics(data map[string]interface{}) error

// ✅ GOOD - Use strongly typed structs
type MetricData struct {
    Name      string        `json:"name"`
    Value     float64       `json:"value"`
    Unit      string        `json:"unit"`
    Timestamp time.Time     `json:"timestamp"`
    Labels    MetricLabels  `json:"labels"`
}

func ProcessMetrics(data MetricData) error
```

### 2. Structured Telemetry Types

#### Span Attributes
```go
// ✅ GOOD - Typed span attributes
type SpanAttributes struct {
    Service      string    `json:"service"`
    Operation    string    `json:"operation"`
    TraceID      string    `json:"trace_id"`
    SpanID       string    `json:"span_id"`
    ParentID     string    `json:"parent_id,omitempty"`
    StartTime    time.Time `json:"start_time"`
    EndTime      time.Time `json:"end_time"`
    Status       SpanStatus `json:"status"`
    Attributes   map[string]string `json:"attributes"` // String-only map for custom attrs
}

type SpanStatus struct {
    Code        StatusCode `json:"code"`
    Description string     `json:"description,omitempty"`
}

type StatusCode int

const (
    StatusCodeUnset StatusCode = iota
    StatusCodeOK
    StatusCodeError
)
```

#### Metric Types
```go
// ✅ GOOD - Typed metrics
type MetricPoint struct {
    Name       string           `json:"name"`
    Value      float64          `json:"value"`
    Unit       string           `json:"unit"`
    Timestamp  time.Time        `json:"timestamp"`
    Labels     map[string]string `json:"labels"`     // String-only for labels
    Attributes MetricAttributes `json:"attributes"` // Typed for known fields
}

type MetricAttributes struct {
    Service   string `json:"service"`
    Namespace string `json:"namespace"`
    Pod       string `json:"pod,omitempty"`
    Container string `json:"container,omitempty"`
    Node      string `json:"node,omitempty"`
}
```

### 3. Correlation Data Types

Following the GOOD pattern from `pkg/intelligence/correlation/types.go`:

```go
// ✅ GOOD - Fully typed correlation structures
type CorrelationDetails struct {
    Pattern          string             `json:"pattern"`
    Algorithm        string             `json:"algorithm"`
    ProcessingTime   time.Duration      `json:"processing_time"`
    DataPoints       int                `json:"data_points"`
    SourceEvents     []EventReference   `json:"source_events"`
    ImpactedServices []ServiceReference `json:"impacted_services"`
}

type EvidenceData struct {
    EventIDs      []string               `json:"event_ids"`
    ResourceIDs   []string               `json:"resource_ids"`
    Timestamps    []time.Time            `json:"timestamps"`
    Metrics       map[string]MetricValue `json:"metrics"`
    Relationships []ResourceRelationship `json:"relationships"`
    Attributes    map[string]string      `json:"attributes"`
}

// Never store as strings - always use structured types
```

### 4. Context Propagation

```go
// ✅ GOOD - Always propagate context with tracing
func (c *Correlator) Process(ctx context.Context, event *domain.UnifiedEvent) error {
    // Create span from context
    ctx, span := tracer.Start(ctx, "correlator.process",
        trace.WithAttributes(
            attribute.String("correlator.name", c.Name()),
            attribute.String("event.id", event.ID),
            attribute.String("event.type", string(event.Type)),
        ))
    defer span.End()
    
    // Pass context through all calls
    if err := c.validate(ctx, event); err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
        return err
    }
    
    span.SetStatus(codes.Ok, "")
    return nil
}
```

### 5. Metric Instrumentation

```go
// ✅ GOOD - Structured metrics with typed attributes
type EngineMetrics struct {
    EventsProcessed       int64         `json:"events_processed"`
    CorrelationsFound     int64         `json:"correlations_found"`
    AverageProcessingTime time.Duration `json:"average_processing_time"`
    ErrorCount            int64         `json:"error_count"`
    LastErrorReason       string        `json:"last_error_reason,omitempty"`
}

func (e *Engine) recordMetrics(ctx context.Context, metrics EngineMetrics) {
    meter := otel.Meter("tapio.correlation")
    
    // Use typed fields, not map[string]interface{}
    counter, _ := meter.Int64Counter("events.processed")
    counter.Add(ctx, metrics.EventsProcessed,
        metric.WithAttributes(
            attribute.String("engine", e.name),
            attribute.Int64("correlations", metrics.CorrelationsFound),
        ))
}
```

## 🔧 Implementation Patterns

### 1. Storage Layer Pattern

```go
// ✅ GOOD - Type-safe storage
func (s *Storage) Store(ctx context.Context, result *CorrelationResult) error {
    // Marshal typed structs, not raw strings
    evidenceJSON, err := json.Marshal(result.Evidence) // Evidence is EvidenceData struct
    if err != nil {
        return fmt.Errorf("failed to marshal evidence: %w", err)
    }
    
    detailsJSON, err := json.Marshal(result.Details) // Details is CorrelationDetails struct
    if err != nil {
        return fmt.Errorf("failed to marshal details: %w", err)
    }
    
    // Store as JSON strings in database
    params := map[string]interface{}{
        "evidence": string(evidenceJSON),
        "details":  string(detailsJSON),
    }
    
    return s.executeQuery(ctx, query, params)
}

// ✅ GOOD - Type-safe retrieval
func (s *Storage) parseResults(records []neo4j.Record) []*CorrelationResult {
    for _, record := range records {
        result := &CorrelationResult{}
        
        // Unmarshal into typed structs
        if evidenceStr, ok := props["evidence"].(string); ok {
            var evidence EvidenceData
            json.Unmarshal([]byte(evidenceStr), &evidence)
            result.Evidence = evidence // Assign struct, not string
        }
        
        if detailsStr, ok := props["details"].(string); ok {
            var details CorrelationDetails
            json.Unmarshal([]byte(detailsStr), &details)
            result.Details = details // Assign struct, not string
        }
    }
}
```

### 2. Error Handling with Tracing

```go
// ✅ GOOD - Structured error handling
type CorrelatorError struct {
    Type    CorrelatorErrorType
    Message string
    Cause   error
    TraceID string // Include trace ID for debugging
}

func (c *Correlator) Process(ctx context.Context, event *domain.UnifiedEvent) error {
    span := trace.SpanFromContext(ctx)
    
    if err := c.validate(event); err != nil {
        corrErr := &CorrelatorError{
            Type:    ErrorTypeValidation,
            Message: "event validation failed",
            Cause:   err,
            TraceID: span.SpanContext().TraceID().String(),
        }
        
        span.RecordError(corrErr)
        span.SetStatus(codes.Error, corrErr.Error())
        
        return corrErr
    }
    
    return nil
}
```

### 3. Batch Operations

```go
// ✅ GOOD - Typed batch operations
type BatchResult struct {
    Successful int           `json:"successful"`
    Failed     int           `json:"failed"`
    Errors     []BatchError  `json:"errors"`
    Duration   time.Duration `json:"duration"`
}

type BatchError struct {
    Index   int    `json:"index"`
    ID      string `json:"id"`
    Error   string `json:"error"`
    TraceID string `json:"trace_id,omitempty"`
}

func (s *Storage) BatchStore(ctx context.Context, results []*CorrelationResult) BatchResult {
    ctx, span := tracer.Start(ctx, "storage.batch_store")
    defer span.End()
    
    batch := BatchResult{}
    start := time.Now()
    
    for i, result := range results {
        if err := s.Store(ctx, result); err != nil {
            batch.Failed++
            batch.Errors = append(batch.Errors, BatchError{
                Index:   i,
                ID:      result.ID,
                Error:   err.Error(),
                TraceID: span.SpanContext().TraceID().String(),
            })
        } else {
            batch.Successful++
        }
    }
    
    batch.Duration = time.Since(start)
    return batch
}
```

## 📊 Monitoring Best Practices

### 1. Metrics to Track

```go
const (
    // Counter metrics
    MetricEventsProcessed     = "tapio.events.processed"
    MetricCorrelationsFound   = "tapio.correlations.found"
    MetricErrorsTotal         = "tapio.errors.total"
    
    // Histogram metrics
    MetricProcessingDuration  = "tapio.processing.duration"
    MetricQueueSize          = "tapio.queue.size"
    
    // Gauge metrics
    MetricActiveCorrelators  = "tapio.correlators.active"
    MetricMemoryUsage        = "tapio.memory.usage"
)
```

### 2. Span Naming Convention

```
service.component.operation
```

Examples:
- `tapio.correlation.process`
- `tapio.storage.neo4j.store`
- `tapio.correlator.temporal.analyze`

### 3. Attribute Standards

```go
// Standard attributes for all spans
const (
    AttrServiceName    = "service.name"
    AttrServiceVersion = "service.version"
    AttrCorrelatorName = "correlator.name"
    AttrEventID        = "event.id"
    AttrEventType      = "event.type"
    AttrTraceID        = "trace.id"
    AttrCluster        = "k8s.cluster"
    AttrNamespace      = "k8s.namespace"
    AttrPod            = "k8s.pod"
)
```

## 🚫 Anti-Patterns to Avoid

### 1. Never Use Raw Interface Types
```go
// ❌ BAD
func Store(data interface{}) error

// ❌ BAD  
func Process(attrs map[string]interface{}) error

// ✅ GOOD
func Store(result *CorrelationResult) error
func Process(event *domain.UnifiedEvent) error
```

### 2. Never Store Complex Types as Strings
```go
// ❌ BAD
result.Details = "pattern: temporal, algorithm: sequence"
result.Evidence = strings.Join(eventIDs, ",")

// ✅ GOOD
result.Details = CorrelationDetails{
    Pattern:   "temporal",
    Algorithm: "sequence",
}
result.Evidence = EvidenceData{
    EventIDs: eventIDs,
}
```

### 3. Never Ignore Context
```go
// ❌ BAD
func Process(event *Event) error

// ✅ GOOD
func Process(ctx context.Context, event *Event) error
```

## 🔍 Testing Guidelines

### 1. Test Telemetry
```go
func TestCorrelatorWithTracing(t *testing.T) {
    // Setup test tracer
    exp := tracetest.NewInMemoryExporter()
    tp := trace.NewTracerProvider(
        trace.WithSyncer(exp),
    )
    
    ctx := context.Background()
    ctx = trace.ContextWithTracerProvider(ctx, tp)
    
    // Run operation
    correlator := NewCorrelator()
    err := correlator.Process(ctx, testEvent)
    
    // Verify spans
    spans := exp.GetSpans()
    assert.Len(t, spans, 1)
    assert.Equal(t, "correlator.process", spans[0].Name)
    assert.Equal(t, codes.Ok, spans[0].Status.Code)
}
```

### 2. Test Metrics
```go
func TestEngineMetrics(t *testing.T) {
    engine := NewEngine()
    
    // Process events
    engine.Process(ctx, event1)
    engine.Process(ctx, event2)
    
    // Get metrics
    metrics := engine.GetMetrics()
    
    // Verify typed fields
    assert.Equal(t, int64(2), metrics.EventsProcessed)
    assert.Greater(t, metrics.AverageProcessingTime, time.Duration(0))
}
```

## 📚 References

- [OpenTelemetry Go Documentation](https://opentelemetry.io/docs/instrumentation/go/)
- [Correlation Types](pkg/intelligence/correlation/types.go) - Reference implementation
- [CLAUDE.md](CLAUDE.md) - Project standards and requirements

## ✅ Checklist for New Implementations

- [ ] All public APIs use typed structs, not `map[string]interface{}`
- [ ] Context is propagated through all function calls
- [ ] Spans are created for significant operations
- [ ] Errors are recorded in spans with proper status codes
- [ ] Metrics use typed attributes, not raw maps
- [ ] Storage serializes/deserializes typed structs
- [ ] Tests verify telemetry output
- [ ] No TODO or stub implementations
- [ ] Code is properly formatted with `make fmt`