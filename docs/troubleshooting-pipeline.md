# Intelligence Pipeline Troubleshooting Guide

This guide helps diagnose and resolve common issues with the Intelligence Pipeline.

## Table of Contents

1. [Common Issues](#common-issues)
2. [Performance Problems](#performance-problems)
3. [Memory Issues](#memory-issues)
4. [Error Diagnostics](#error-diagnostics)
5. [Configuration Problems](#configuration-problems)
6. [Integration Issues](#integration-issues)
7. [Debug Tools](#debug-tools)
8. [Getting Help](#getting-help)

## Common Issues

### Pipeline Won't Start

**Symptom**: `pipeline.Start()` returns error or hangs

**Possible Causes & Solutions**:

1. **Already Running**
   ```go
   // Error: "orchestrator is already running"
   // Solution: Check if pipeline is already started
   if pipeline.IsRunning() {
       log.Println("Pipeline already running")
       return
   }
   ```

2. **Invalid Configuration**
   ```go
   // Check configuration before building
   config := pipeline.DefaultPipelineConfig()
   if err := config.Validate(); err != nil {
       log.Printf("Invalid config: %v", err)
   }
   ```

3. **Resource Constraints**
   ```bash
   # Check system resources
   free -h  # Memory
   nproc    # CPU cores
   ulimit -n # File descriptors
   
   # Increase limits if needed
   ulimit -n 65536
   ```

### Events Not Processing

**Symptom**: Events sent but metrics show no processing

**Diagnosis**:
```go
// Check pipeline state
metrics := pipeline.GetMetrics()
log.Printf("Pipeline running: %v", pipeline.IsRunning())
log.Printf("Events received: %d", metrics.EventsReceived)
log.Printf("Events processed: %d", metrics.EventsProcessed)
log.Printf("Queue depth: %d", metrics.QueueDepth)
```

**Common Fixes**:

1. **Event Validation Failures**
   ```go
   // Ensure required fields
   event := &domain.UnifiedEvent{
       ID:        domain.GenerateEventID(), // Required
       Type:      domain.EventTypeSystem,   // Required
       Timestamp: time.Now(),               // Required
       Source:    "my-collector",           // Required
   }
   ```

2. **Circuit Breaker Open**
   ```go
   metrics := pipeline.GetMetrics()
   if metrics.CircuitBreakerState == "open" {
       log.Println("Circuit breaker is open due to errors")
       // Wait for recovery or fix underlying issues
   }
   ```

3. **Buffer Full**
   ```go
   // Increase buffer size
   pipeline, err := pipeline.NewPipelineBuilder().
       WithBufferSize(100000). // Increase from default
       Build()
   ```

### High Error Rate

**Symptom**: `metrics.ErrorRate > 0.05` (5%)

**Investigation Steps**:

1. **Check Error Distribution**
   ```go
   metrics := pipeline.GetMetrics()
   log.Printf("Validation errors: %d", metrics.ValidationErrors)
   log.Printf("Context errors: %d", metrics.ContextErrors)
   log.Printf("Correlation errors: %d", metrics.CorrelationErrors)
   ```

2. **Enable Debug Logging**
   ```go
   pipeline, _ := pipeline.NewPipelineBuilder().
       WithMode(pipeline.PipelineModeDebug).
       Build()
   ```

3. **Common Error Patterns**
   - Missing source field → Add source to events
   - Old timestamps → Check event age (max 24h)
   - Invalid event types → Use defined EventType constants

## Performance Problems

### Low Throughput

**Symptom**: Processing < 100k events/sec when expecting more

**Diagnosis Checklist**:

1. **Check Pipeline Mode**
   ```go
   config := pipeline.GetConfig()
   if config.Mode != pipeline.PipelineModeHighPerformance {
       // Switch to high-performance mode
       pipeline, _ = pipeline.NewHighPerformancePipeline()
   }
   ```

2. **Verify Concurrency Settings**
   ```go
   // Optimal settings for throughput
   pipeline, _ := pipeline.NewPipelineBuilder().
       WithMaxConcurrency(runtime.NumCPU() * 2).
       WithBatchSize(5000).
       WithBufferSize(100000).
       Build()
   ```

3. **CPU Profiling**
   ```bash
   # Run with profiling
   go test -cpuprofile=cpu.prof -bench=.
   go tool pprof cpu.prof
   
   # In pprof:
   top10
   list ProcessEvent
   ```

4. **System Bottlenecks**
   ```bash
   # Monitor during load
   htop  # CPU usage
   iotop # Disk I/O
   iftop # Network I/O
   ```

### High Latency

**Symptom**: P99 latency > 20ms

**Common Causes**:

1. **Large Batch Sizes**
   ```go
   // Reduce batch size for lower latency
   pipeline, _ := pipeline.NewPipelineBuilder().
       WithBatchSize(100).  // Smaller batches
       Build()
   ```

2. **Correlation Window Too Large**
   ```go
   // Reduce correlation window
   config.OrchestratorConfig.CorrelationWindow = 2 * time.Minute
   ```

3. **GC Pressure**
   ```bash
   # Monitor GC
   GODEBUG=gctrace=1 ./your-app
   
   # Tune GC
   export GOGC=200  # Less frequent GC
   ```

### Throughput Degradation

**Symptom**: Performance decreases over time

**Solutions**:

1. **Memory Leak Check**
   ```go
   // Monitor memory growth
   ticker := time.NewTicker(1 * time.Minute)
   for range ticker.C {
       var m runtime.MemStats
       runtime.ReadMemStats(&m)
       log.Printf("Alloc = %v MB", m.Alloc / 1024 / 1024)
   }
   ```

2. **Pattern Cache Growth**
   ```go
   // Limit pattern cache
   correlationConfig := &correlation.ProcessorConfig{
       MaxPatterns: 1000,  // Limit pattern count
   }
   ```

## Memory Issues

### High Memory Usage

**Symptom**: Memory usage > 2GB for moderate load

**Diagnosis**:
```go
// Memory profiling
import _ "net/http/pprof"

go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()

// Then: go tool pprof http://localhost:6060/debug/pprof/heap
```

**Common Fixes**:

1. **Reduce Buffer Sizes**
   ```go
   pipeline, _ := pipeline.NewPipelineBuilder().
       WithBufferSize(10000).  // Smaller buffer
       Build()
   ```

2. **Enable Event Pooling**
   ```go
   var eventPool = sync.Pool{
       New: func() interface{} {
           return &domain.UnifiedEvent{}
       },
   }
   
   // Reuse events
   event := eventPool.Get().(*domain.UnifiedEvent)
   defer eventPool.Put(event)
   ```

### Memory Leaks

**Detection**:
```bash
# Compare heap profiles
go tool pprof -base heap1.prof heap2.prof

# Look for growing allocations
(pprof) top -cum
(pprof) list functionName
```

**Common Leak Sources**:
- Unbounded correlation buffers
- Growing metrics without reset
- Channel goroutine leaks

## Error Diagnostics

### Validation Errors

**Common Validation Failures**:

```go
// Event too old
event.Timestamp = time.Now().Add(-25 * time.Hour) // Will fail

// Missing required fields
event.Source = "" // Will fail

// Fix: Ensure all required fields
event := &domain.UnifiedEvent{
    ID:        domain.GenerateEventID(),
    Type:      domain.EventTypeSystem,
    Timestamp: time.Now(),
    Source:    "my-service",
}
```

### Context Building Errors

**Common Issues**:
- Invalid entity data
- Malformed semantic context
- Missing enrichment data

**Debug**:
```go
// Test context building separately
validator := context.NewEventValidator()
if err := validator.Validate(event); err != nil {
    log.Printf("Validation error: %v", err)
}
```

### Correlation Errors

**Symptoms**: Patterns not detected

**Debug Steps**:

1. **Check Time Windows**
   ```go
   // Events must be within correlation window
   config.CorrelationWindow = 10 * time.Minute
   ```

2. **Verify Pattern Configuration**
   ```go
   // Add debug logging
   log.Printf("Event entity: %+v", event.Entity)
   log.Printf("Event semantic: %+v", event.Semantic)
   ```

## Configuration Problems

### Invalid Configuration

**Validation Helper**:
```go
func validateConfig(config *pipeline.PipelineConfig) error {
    if config.BatchSize > config.BufferSize {
        return fmt.Errorf("batch size cannot exceed buffer size")
    }
    if config.MaxConcurrency == 0 {
        config.MaxConcurrency = runtime.NumCPU()
    }
    return config.Validate()
}
```

### Configuration Best Practices

```go
// Development
devConfig := pipeline.DebugPipelineConfig()

// Testing
testConfig := pipeline.StandardPipelineConfig()

// Production
prodConfig := &pipeline.PipelineConfig{
    Mode:               pipeline.PipelineModeHighPerformance,
    MaxConcurrency:     runtime.NumCPU() * 2,
    BatchSize:          1000,
    BufferSize:         50000,
    EnableCircuitBreaker: true,
    ErrorThreshold:     0.01, // 1% error rate
}
```

## Integration Issues

### gRPC Connection Problems

**Debug Connection**:
```go
// Enable gRPC logging
import "google.golang.org/grpc/grpclog"

grpclog.SetLoggerV2(grpclog.NewLoggerV2(
    os.Stdout, os.Stdout, os.Stderr,
))
```

### Event Format Mismatch

**Validation**:
```go
// Validate JSON marshaling
data, err := json.Marshal(event)
if err != nil {
    log.Printf("Marshal error: %v", err)
}

// Validate unmarshaling
var decoded domain.UnifiedEvent
if err := json.Unmarshal(data, &decoded); err != nil {
    log.Printf("Unmarshal error: %v", err)
}
```

## Debug Tools

### Built-in Metrics

```go
// Comprehensive metrics logging
func logDetailedMetrics(p pipeline.IntelligencePipeline) {
    metrics := p.GetMetrics()
    
    log.Printf("=== Pipeline Metrics ===")
    log.Printf("State: Running=%v", p.IsRunning())
    log.Printf("Events: Received=%d, Processed=%d, Failed=%d", 
        metrics.EventsReceived, 
        metrics.EventsProcessed, 
        metrics.EventsFailed)
    log.Printf("Throughput: %.2f events/sec", metrics.ThroughputPerSecond)
    log.Printf("Latency: Avg=%v, P99=%v", metrics.AverageLatency, metrics.P99Latency)
    log.Printf("Errors: Rate=%.2f%%", metrics.ErrorRate * 100)
    log.Printf("Circuit Breaker: State=%s, Trips=%d", 
        metrics.CircuitBreakerState, 
        metrics.CircuitBreakerTrips)
    log.Printf("Resources: Workers=%d, Queue=%d/%d", 
        metrics.ActiveWorkers, 
        metrics.QueueDepth, 
        metrics.QueueCapacity)
}
```

### Debug Mode Pipeline

```go
// Create debug pipeline with extensive logging
debugPipeline, _ := pipeline.NewPipelineBuilder().
    WithMode(pipeline.PipelineModeDebug).
    EnableTracing(true).
    EnableProfiling(true).
    WithMetricsInterval(1 * time.Second).
    Build()
```

### Custom Debug Stage

```go
type DebugStage struct {
    name string
    log  *log.Logger
}

func (d *DebugStage) Process(ctx context.Context, event *domain.UnifiedEvent) error {
    d.log.Printf("[%s] Processing event: ID=%s, Type=%s, Source=%s", 
        d.name, event.ID, event.Type, event.Source)
    return nil
}

// Add to pipeline
builder.AddStage(&DebugStage{name: "debug", log: logger})
```

## Getting Help

### Collect Diagnostics

```bash
# System info
uname -a
go version
free -h
nproc

# Pipeline info
curl http://localhost:8080/metrics  # If metrics endpoint exposed

# Logs (last 1000 lines)
journalctl -u tapio -n 1000 > tapio.log
```

### Report Issues

When reporting issues, include:

1. **Environment Details**
   - OS and version
   - Go version
   - Hardware specs
   - Pipeline configuration

2. **Reproduction Steps**
   - Minimal code example
   - Event examples that trigger issue
   - Expected vs actual behavior

3. **Metrics and Logs**
   - Pipeline metrics output
   - Error messages
   - Stack traces if available

### Support Channels

- GitHub Issues: https://github.com/yairfalse/tapio/issues
- Documentation: https://docs.tapio.io
- Community Slack: #tapio-help

### Emergency Fixes

**Pipeline Hanging**:
```go
// Force shutdown with timeout
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
pipeline.Shutdown()
```

**Memory Exhaustion**:
```go
// Emergency memory reduction
debug.FreeOSMemory()
runtime.GC()
```

**Complete Reset**:
```go
// Restart pipeline
pipeline.Shutdown()
time.Sleep(1 * time.Second)
pipeline, _ = pipeline.NewHighPerformancePipeline()
pipeline.Start(ctx)
```