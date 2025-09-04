# Base Collector Package

Common functionality for all Tapio collectors, reducing code duplication and ensuring consistent observability.

## Components

### 1. BaseCollector
Provides `Statistics()`, `Health()` methods, and built-in OTEL instrumentation.

```go
type MyCollector struct {
    *base.BaseCollector
    // your fields
}

func NewMyCollector() *MyCollector {
    return &MyCollector{
        BaseCollector: base.NewBaseCollector("my-collector", 5*time.Minute),
    }
}

// With custom configuration:
func NewMyCollectorAdvanced() *MyCollector {
    config := base.BaseCollectorConfig{
        Name:               "my-collector",
        HealthCheckTimeout: 5 * time.Minute,
        ErrorRateThreshold: 0.05, // 5% instead of default 10%
    }
    return &MyCollector{
        BaseCollector: base.NewBaseCollectorWithConfig(config),
    }
}

// In your event processing:
collector.RecordEvent()                    // Track successful events
collector.RecordEventWithContext(ctx)     // With trace context
collector.RecordError(err)                // Track errors
collector.RecordErrorWithContext(ctx, err) // With trace context + span error
collector.RecordDrop()                    // Track dropped events
collector.RecordDropWithReason(ctx, "buffer_full") // With reason

// Additional OTEL metrics:
collector.RecordProcessingDuration(ctx, duration)
collector.RecordEventSize(ctx, sizeBytes)

// Trace instrumentation:
ctx, span := collector.StartSpan(ctx, "process-event")
defer span.End()
```

### 2. EventChannelManager
Handles event channels with automatic drop counting.

```go
type MyCollector struct {
    *base.EventChannelManager
}

func NewMyCollector(channelSize int) *MyCollector {
    return &MyCollector{
        EventChannelManager: base.NewEventChannelManager(channelSize, "my-collector", logger),
    }
}

// Send events:
if !collector.SendEvent(event) {
    // Event was dropped
}

// Get channel for readers:
ch := collector.GetChannel()
```

### 3. LifecycleManager
Manages goroutines and graceful shutdown.

```go
type MyCollector struct {
    *base.LifecycleManager
}

func (c *MyCollector) Start() {
    c.LifecycleManager.Start("event-processor", func() {
        for {
            select {
            case <-c.StopChannel():
                return
            case event := <-c.eventCh:
                c.processEvent(event)
            }
        }
    })
}

func (c *MyCollector) Stop() error {
    return c.LifecycleManager.Stop(30 * time.Second)
}
```

## Benefits

- **Consistent Metrics**: All collectors report the same base metrics
- **Thread-Safe**: Uses atomic operations throughout
- **Zero Allocation**: Hot paths avoid allocations
- **Reduced Code**: ~200 lines saved per collector
- **Better Testing**: Test base functionality once

## Usage Example

Complete collector using all base components:

```go
package mycollector

import (
    "github.com/yairfalse/tapio/pkg/collectors/base"
    "github.com/yairfalse/tapio/pkg/domain"
)

type Collector struct {
    *base.BaseCollector
    *base.EventChannelManager
    *base.LifecycleManager
    
    config *Config
    logger *zap.Logger
}

func NewCollector(config *Config, logger *zap.Logger) (*Collector, error) {
    return &Collector{
        BaseCollector:       base.NewBaseCollector("my-collector", 5*time.Minute),
        EventChannelManager: base.NewEventChannelManager(1000, "my-collector", logger),
        LifecycleManager:    base.NewLifecycleManager(context.Background(), logger),
        config:              config,
        logger:              logger,
    }, nil
}

// Automatically get Statistics() and Health() methods from BaseCollector

func (c *Collector) Start(ctx context.Context) error {
    c.Start("processor", func() {
        // Your processing logic
        c.RecordEvent() // Track events
    })
    return nil
}

func (c *Collector) Stop() error {
    return c.Stop(30 * time.Second)
}

// Events returns the channel (implements Collector interface)
func (c *Collector) Events() <-chan *domain.CollectorEvent {
    return c.GetChannel()
}
```

## Metrics Provided

Every collector using BaseCollector automatically provides OpenTelemetry metrics:

### Standard Metrics
- `{collector}_events_processed_total` - Total events successfully processed
- `{collector}_events_dropped_total` - Events dropped due to channel overflow (with reason labels)
- `{collector}_errors_total` - Total errors encountered (with error_type labels)
- `{collector}_processing_duration_seconds` - Event processing time histogram
- `{collector}_event_size_bytes` - Event size distribution histogram
- `{collector}_health_status` - Health status gauge (0=unhealthy, 1=degraded, 2=healthy)

### Legacy Statistics (for backward compatibility)
- `events_processed` - Total events successfully processed
- `error_count` - Total errors encountered  
- `events_dropped` - Events dropped due to channel overflow
- `last_event_time` - Timestamp of last processed event
- `uptime` - How long the collector has been running

## Health Checks

BaseCollector provides automatic health monitoring:

- **Healthy**: Operating normally
- **Degraded**: No events for > timeout, or error rate > threshold (default 10%, configurable)
- **Unhealthy**: Explicitly marked unhealthy or critical errors

Health status is automatically exported as an OTEL gauge metric with reasons for degraded states.

## Distributed Tracing

BaseCollector provides built-in tracing support:

```go
// Start a span for event processing
ctx, span := collector.StartSpan(ctx, "process-kafka-event")
defer span.End()

// Record errors automatically in spans
collector.RecordErrorWithContext(ctx, err) // Error recorded in span + metrics

// Custom instrumentation
tracer := collector.GetTracer()
ctx, customSpan := tracer.Start(ctx, "custom-operation")
defer customSpan.End()

// All OTEL metrics are automatically correlated with trace context
collector.RecordEventWithContext(ctx) // Metrics linked to current span
```

## Custom Metrics

Extend with collector-specific metrics:

```go
type MyCollector struct {
    *base.BaseCollector
    
    // Custom metrics using the same meter
    customCounter metric.Int64Counter
}

func NewMyCollector() (*MyCollector, error) {
    bc := base.NewBaseCollector("my-collector", 5*time.Minute)
    
    // Use the base collector's meter for consistency
    meter := bc.GetMeter()
    customCounter, err := meter.Int64Counter(
        "my_collector_custom_events_total",
        metric.WithDescription("Custom events processed"),
    )
    
    return &MyCollector{
        BaseCollector: bc,
        customCounter: customCounter,
    }, err
}
```

## Thread Safety

All base components are thread-safe:
- Atomic counters for statistics
- Atomic values for complex types
- No locks in hot paths
- Safe concurrent access from multiple goroutines

## Testing

```bash
# Run tests
go test ./pkg/collectors/base/

# Check for race conditions
go test -race ./pkg/collectors/base/

# Benchmark performance
go test -bench=. -benchmem ./pkg/collectors/base/
```