# OTEL Integration Test for Tapio CNI Collector

This test program demonstrates the OpenTelemetry (OTEL) integration in the Tapio CNI collector, showing how distributed tracing works across different events and collectors.

## Overview

The test program creates simulated CNI events with proper OTEL tracing to verify:
- Trace ID and Span ID creation
- Parent-child span relationships
- Cross-collector trace propagation
- Error handling with tracing
- Integration with UnifiedEvent structure

## Files

- `main.go` - Basic test showing trace IDs and relationships
- `main_with_console.go` - Detailed test with full console output of spans

## Running the Tests

### Basic Test
```bash
go run ./cmd/test-otel/main.go
```

This shows:
- Collector startup tracing
- CNI event processing with trace context
- Related events sharing the same trace ID
- Error scenarios with proper span status

### Detailed Console Output
```bash
go run ./cmd/test-otel/main_with_console.go
```

This outputs full span details including:
- All span attributes
- Event timestamps
- Parent-child relationships
- Span status and errors

## Key Features Demonstrated

### 1. Trace Context in UnifiedEvent
```go
event.TraceContext = &domain.TraceContext{
    TraceID: spanContext.TraceID().String(),
    SpanID:  spanContext.SpanID().String(),
}
```

### 2. CNI Event Instrumentation
The CNI collector automatically:
- Creates spans for event processing
- Adds CNI-specific attributes (pod name, namespace, IP, plugin)
- Records events (IP allocation, interface creation)
- Sets proper span status

### 3. Cross-Collector Correlation
Events from different collectors (CNI, K8s, SystemD) share the same trace ID, enabling:
- End-to-end request tracing
- Root cause analysis across components
- Performance bottleneck identification

### 4. Error Handling
Failed operations are properly traced with:
- Error recording in spans
- Error attributes (type, message)
- Span status set to Error
- Retry attempts tracked

## Integration with Production

In production, the CNI collector:
1. Initializes OTEL if enabled in config
2. Creates spans for all event processing
3. Enhances UnifiedEvents with trace context
4. Exports traces to Jaeger or other backends

## Example Output

```
=== Tapio OTEL Integration Test ===

Test 1: Collector Startup Tracing
---------------------------------
✓ Collector startup traced
  Trace ID: f113735e6a56aa7efaf08ba283122f67
  Span ID:  76513f83988d940c

Test 2: CNI Event Processing with Tracing
-----------------------------------------
✓ CNI event processed and traced
  Event ID: cni-1753059483335811000
  Trace ID: 413e03a16a3f41ad33b7fdeec7df65d9
  Span ID:  04c9f1c166f3269c
```

## Benefits

1. **Distributed Tracing**: Track requests across multiple Tapio collectors
2. **Performance Analysis**: Identify slow operations with span durations
3. **Error Correlation**: Link errors to specific traces for debugging
4. **Semantic Context**: Rich attributes for understanding event relationships
5. **Standards Compliance**: Uses OpenTelemetry standards for compatibility