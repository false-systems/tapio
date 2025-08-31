# Tapio Collector Development Guide

## Base Collector Package

All collectors should use the base package for common functionality:

### Location
`pkg/collectors/base/`

### Components

1. **BaseCollector** - Provides Statistics() and Health() methods
2. **EventChannelManager** - Handles event channels with drop counting  
3. **LifecycleManager** - Manages goroutines and shutdown

### Usage Pattern

```go
type MyCollector struct {
    *base.BaseCollector      // Embed for stats/health
    *base.EventChannelManager // Embed for events
    *base.LifecycleManager    // Embed for lifecycle
    
    // Your specific fields
    config *Config
}
```

### Benefits
- Consistent metrics across all collectors
- Thread-safe operations with atomics
- ~200 lines of code saved per collector
- Automatic health monitoring

## Collector Registration

Each collector keeps its own `init.go` for registration because:
- Config mapping from YAML is unique
- Factory functions need specific setup
- Registration happens once at startup

## Testing Collectors

```bash
# Test base functionality
go test ./pkg/collectors/base/

# Test with race detection
go test -race ./pkg/collectors/base/

# Benchmark for performance
go test -bench=. -benchmem ./pkg/collectors/base/
```

## Adding a New Collector

1. Create directory: `pkg/collectors/your-collector/`
2. Embed base components you need
3. Implement Collector interface (Name, Start, Stop)
4. Add init.go for orchestrator registration
5. Write tests including integration tests

## Performance Guidelines

- Use atomic operations for counters
- Avoid allocations in hot paths
- Batch operations when possible
- Use ring buffers for kernel communication
- Sample events if volume is high

## Common Patterns

### Event Processing Loop
```go
func (c *Collector) processEvents() {
    for {
        select {
        case <-c.StopChannel():
            return
        case event := <-c.eventCh:
            c.RecordEvent()  // Track in base
            // Process event
        }
    }
}
```

### Error Handling
```go
if err != nil {
    c.RecordError(err)  // Track in base
    c.logger.Warn("Failed to process", zap.Error(err))
}
```

### Health Checks
The base collector automatically provides health status based on:
- Event flow (degraded if no events for timeout)
- Error rate (degraded if >10% errors)  
- Manual status (SetHealthy(false) for critical issues)