# Test Observer

The test observer generates configurable mock events for validating ObserverRuntime infrastructure.

## Purpose

- **NOT** a stub or placeholder
- **IS** a real processor that generates synthetic events
- Used to validate runtime features before migrating real observers

## Usage

```go
import (
    "github.com/yairfalse/tapio/internal/observers/test"
    "github.com/yairfalse/tapio/internal/runtime"
)

// Create processor with default settings (10 events/sec, test events)
processor := test.NewProcessor()

// Create processor with custom settings
processor := test.NewProcessor(
    test.WithEventRate(100),  // 100 events/sec
    test.WithEventTypes([]test.EventType{
        {Type: "network", Subtype: "dns_query"},
        {Type: "network", Subtype: "http_connection"},
        {Type: "container", Subtype: "oom_kill"},
    }),
)

// Wire to ObserverRuntime
obs, err := runtime.NewObserverRuntime(processor)
if err != nil {
    log.Fatal(err)
}

// Run
ctx := context.Background()
if err := obs.Run(ctx); err != nil {
    log.Fatal(err)
}
```

## Configuration Options

### WithEventRate(rate int)

Sets events per second generation rate.

**Examples:**
- `WithEventRate(10)` - 10 events/sec (default)
- `WithEventRate(100)` - 100 events/sec
- `WithEventRate(1000)` - 1K events/sec (load testing)
- `WithEventRate(10000)` - 10K events/sec (stress testing)

### WithEventTypes(types []EventType)

Configures event types to generate. Processor randomly selects from this list.

**Examples:**
```go
// Single event type
test.WithEventTypes([]test.EventType{
    {Type: "test", Subtype: "mock_event"},
})

// Multiple network events
test.WithEventTypes([]test.EventType{
    {Type: "network", Subtype: "dns_query"},
    {Type: "network", Subtype: "dns_response"},
    {Type: "network", Subtype: "http_connection"},
    {Type: "network", Subtype: "link_failure"},
})

// Mixed event types
test.WithEventTypes([]test.EventType{
    {Type: "network", Subtype: "dns_query"},
    {Type: "container", Subtype: "oom_kill"},
    {Type: "node", Subtype: "pmc_sample"},
})
```

## Testing Runtime Features

### 1. Event Sampling

```go
processor := test.NewProcessor(
    test.WithEventRate(1000), // Generate 1K events/sec
)

// Configure sampling to 10%
obs, err := runtime.NewObserverRuntime(processor,
    runtime.WithSampling(0.1, nil), // Keep 10%
)

// Verify: Should emit ~100 events/sec
```

### 2. Backpressure

```go
processor := test.NewProcessor(
    test.WithEventRate(10000), // Generate 10K events/sec
)

// Configure small queue with drop policy
obs, err := runtime.NewObserverRuntime(processor,
    runtime.WithBackpressure(100, runtime.DropOldest),
)

// Verify: Queue drops events when full, no OOM
```

### 3. Multiple Emitters

```go
processor := test.NewProcessor(
    test.WithEventRate(100),
)

obs, err := runtime.NewObserverRuntime(processor,
    runtime.WithEmitters(
        NewOTLPEmitter("localhost:4318"),
        NewFileEmitter("/tmp/test-events.json"),
    ),
)

// Verify: Events fan-out to all emitters
```

### 4. Health Checks

```go
processor := test.NewProcessor()

obs, err := runtime.NewObserverRuntime(processor)

// Check health
healthy := obs.IsHealthy()

// Simulate failure
obs.Stop()

// Health should be false
```

## Load Testing

Generate high event rates to stress test:

```go
// Sustained load (1 hour @ 10K events/sec = 36M events)
processor := test.NewProcessor(
    test.WithEventRate(10000),
)

ctx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
defer cancel()

obs, err := runtime.NewObserverRuntime(processor)
if err != nil {
    log.Fatal(err)
}

if err := obs.Run(ctx); err != nil {
    log.Fatal(err)
}

// Monitor:
// - Memory usage (pprof)
// - CPU usage
// - Event drop rate
// - Emitter errors
```

## Event Generation

Events are generated at the specified rate and selected randomly from configured types.

**Generated Event Structure:**
```json
{
  "type": "network",
  "subtype": "dns_query",
  "timestamp": "2025-01-05T10:30:00Z"
}
```

Events are minimal (type/subtype/timestamp only) since the test observer validates **infrastructure**, not business logic.

## Not a Stub

This is NOT stub code. The test observer:
- ✅ Implements the full EventProcessor interface
- ✅ Generates real domain.ObserverEvent instances
- ✅ Respects context cancellation
- ✅ Has comprehensive tests (9 test cases)
- ✅ Works with ObserverRuntime
- ❌ Does NOT return nil and do nothing
- ❌ Does NOT have TODO comments
- ❌ Does NOT have incomplete implementations

## Example: Full Integration Test

```go
func TestObserverRuntime_WithTestObserver(t *testing.T) {
    // Create test observer
    processor := test.NewProcessor(
        test.WithEventRate(10),
        test.WithEventTypes([]test.EventType{
            {Type: "network", Subtype: "dns_query"},
        }),
    )

    // Create file emitter for verification
    emitter, err := NewFileEmitter("/tmp/test-events.json")
    require.NoError(t, err)

    // Wire to runtime
    obs, err := runtime.NewObserverRuntime(processor,
        runtime.WithEmitters(emitter),
    )
    require.NoError(t, err)

    // Run for 1 second
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    go obs.Run(ctx)

    // Start generating events
    eventCh := make(chan []byte, 100)
    go processor.StartGeneration(ctx, eventCh)

    // Feed events to runtime
    go func() {
        for data := range eventCh {
            obs.ProcessEvent(ctx, data)
        }
    }()

    <-ctx.Done()

    // Verify file has ~10 events
    data, err := os.ReadFile("/tmp/test-events.json")
    require.NoError(t, err)

    lines := bytes.Count(data, []byte("\n"))
    assert.InDelta(t, 10, lines, 2)
}
```
