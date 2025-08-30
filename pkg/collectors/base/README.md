# Base Collector Package

Common functionality for all Tapio collectors, reducing code duplication and ensuring consistent observability.

## Components

### 1. BaseCollector
Provides `Statistics()` and `Health()` methods for all collectors.

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

// In your event processing:
collector.RecordEvent()     // Track successful events
collector.RecordError(err)  // Track errors
collector.RecordDrop()      // Track dropped events
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

Every collector using BaseCollector automatically provides:

- `events_processed` - Total events successfully processed
- `error_count` - Total errors encountered
- `events_dropped` - Events dropped due to channel overflow
- `last_event_time` - Timestamp of last processed event
- `uptime` - How long the collector has been running

## Health Checks

BaseCollector provides automatic health monitoring:

- **Healthy**: Operating normally
- **Degraded**: No events for > timeout, or error rate > 10%
- **Unhealthy**: Explicitly marked unhealthy or critical errors

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