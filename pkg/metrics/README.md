# Enterprise Prometheus Metrics Package

## Overview

This package provides an enterprise-grade Prometheus integration with advanced Go patterns, featuring **factory pattern for different client types**, **observer pattern for real-time metric updates**, **memory-efficient streaming**, and **graceful shutdown with proper resource cleanup**.

## 🏗️ Architecture

The package demonstrates cutting-edge Go engineering patterns:

### Core Design Patterns

- **Factory Pattern**: `MetricFactory` creates different types of Prometheus clients (push, pull, stream, collector)
- **Observer Pattern**: `MetricPublisher` provides real-time metric event notifications with priority-based ordering
- **Strategy Pattern**: Different metric collection approaches with pluggable strategies
- **Circuit Breaker Pattern**: Resilient service discovery and metric collection
- **Object Pool Pattern**: Memory-efficient object reuse with `sync.Pool`

### Advanced Go Features

- **Go Generics**: Type-safe metrics with `Counter[T]`, `Gauge[T]`, `Histogram[T]`, `Summary[T]`
- **Context Propagation**: Proper cancellation and timeout handling throughout
- **Atomic Operations**: Lock-free performance-critical paths
- **Worker Pools**: Bounded concurrency with dynamic scaling
- **Rate Limiting**: Token bucket algorithm with backpressure management
- **Memory Management**: Zero-allocation paths and GC pressure reduction

## 📁 Package Structure

```
pkg/metrics/
├── interfaces.go          # Core interfaces with Go generics
├── factory.go            # Factory pattern for client creation
├── observer.go           # Observer pattern for real-time updates
├── collector.go          # Advanced metric collection with rate limiting
├── streamer.go           # Memory-efficient metric streaming
├── shutdown.go           # Graceful shutdown orchestration
├── types.go              # Type-safe metric implementations
├── benchmarks_test.go    # Comprehensive performance benchmarks
└── README.md            # This documentation
```

## 🚀 Key Features

### Type-Safe Metrics with Go Generics

```go
// Type-safe counter for different numeric types
counter := NewCounter[int64](
    "requests_total",
    "Total HTTP requests",
    Labels{"method": "GET"},
    CounterConstraints[int64]{
        MinValue: 0,
        MaxValue: 1000000000,
        ValidateFunc: func(v int64) error {
            if v < 0 {
                return fmt.Errorf("counter cannot be negative")
            }
            return nil
        },
    },
)

// Type-safe gauge with validation
gauge := NewGauge[float64](
    "cpu_usage_percent",
    "CPU usage percentage",
    Labels{"core": "0"},
    GaugeConstraints[float64]{
        MinValue: 0.0,
        MaxValue: 100.0,
    },
)
```

### Factory Pattern for Client Creation

```go
// Create factory with configuration
config := FactoryConfig{
    DefaultTimeout:         30 * time.Second,
    DefaultShutdownTimeout: 10 * time.Second,
    MaxClients:            100,
    EnableMetrics:         true,
}

factory := NewPrometheusMetricFactory(config, logger)

// Create different client types
pushClient, err := factory.CreatePushClient(PushClientConfig{
    GatewayURL: "http://prometheus-gateway:9091",
    JobName:    "my-service",
    Instance:   "instance-1",
    Timeout:    time.Second,
    RateLimiting: RateLimitConfig{
        RequestsPerSecond: 100.0,
        BurstSize:        10,
        Enabled:          true,
    },
})

pullClient, err := factory.CreatePullClient(PullClientConfig{
    ListenAddress:  "0.0.0.0",
    ListenPort:     8080,
    MetricsPath:    "/metrics",
    ScrapeInterval: 15 * time.Second,
})
```

### Observer Pattern for Real-Time Updates

```go
// Create publisher with buffering
config := PublisherConfig{
    DefaultBufferSize:     10000,
    DefaultFlushInterval:  time.Second,
    EnableBatching:        true,
    WorkerPoolSize:        5,
}

publisher := NewMetricEventPublisher[MetricType](config, logger)

// Create observer
type MetricObserver struct {
    id       string
    priority ObserverPriority
}

func (o *MetricObserver) OnMetricUpdated(ctx context.Context, metric MetricType, oldValue, newValue interface{}) error {
    log.Printf("Metric %s updated: %v -> %v", metric.GetName(), oldValue, newValue)
    return nil
}

// Subscribe observer
observer := &MetricObserver{
    id:       "alerting-observer",
    priority: ObserverPriorityHigh,
}
publisher.Subscribe(observer)

// Publish events (automatically triggered by metric updates)
event := MetricEvent[MetricType]{
    Type:      EventTypeUpdated,
    Metric:    counter,
    OldValue:  int64(10),
    NewValue:  int64(11),
    Timestamp: time.Now(),
}
publisher.Publish(ctx, event)
```

### Advanced Metric Collection

```go
// Create collector with rate limiting and backpressure
config := CollectorConfig[CustomMetric]{
    CollectorName:      "system-metrics",
    CollectionInterval: 30 * time.Second,
    Timeout:           10 * time.Second,
    
    // Rate limiting
    RateLimit: RateLimitSettings{
        RequestsPerSecond: 50.0,
        BurstSize:        10,
        Enabled:          true,
    },
    
    // Backpressure management
    Backpressure: BackpressureSettings{
        Strategy:      BackpressureStrategyAdaptive,
        BufferSize:    5000,
        DropThreshold: 0.8,
        AlertThreshold: 0.9,
    },
    
    // Circuit breaker
    CircuitBreaker: CircuitBreakerSettings{
        FailureThreshold: 5,
        Timeout:         30 * time.Second,
        Enabled:         true,
    },
    
    // Collection function
    CollectionFunc: func(ctx context.Context, opts CollectionOptions) ([]CustomMetric, error) {
        // Implement your metric collection logic
        return collectSystemMetrics(ctx)
    },
}

collector, err := NewPrometheusMetricCollector(config, logger)

// Start collection
resultCh, err := collector.Collect(ctx, CollectionOptions{
    Timeout:    30 * time.Second,
    MaxMetrics: 1000,
})

// Process results
for result := range resultCh {
    if result.Error != nil {
        log.Printf("Collection error: %v", result.Error)
        continue
    }
    
    log.Printf("Collected %d metrics in %v", 
        len(result.Metrics), result.Duration)
}
```

### Memory-Efficient Streaming

```go
// Create streamer with compression and buffering
config := StreamerConfig{
    WorkerCount:         4,
    DefaultBufferSize:   5000,
    DefaultFlushInterval: time.Second,
    MaxStreams:          50,
    EnableDiskSpillover: true,
    SpilloverThreshold:  100 * 1024 * 1024, // 100MB
}

streamer := NewPrometheusMetricStreamer[MetricType](config, logger)

// Start stream with compression
streamCh, err := streamer.StartStream(ctx, StreamOptions{
    BufferSize:    10000,
    FlushInterval: 500 * time.Millisecond,
    Compression:   true,
    EnableBatching: true,
    BatchSize:     100,
    
    // Transform function for metric processing
    TransformFunc: func(metric MetricType) MetricType {
        // Apply transformations
        return metric
    },
    
    // Filter function for selective streaming
    FilterFunc: func(metric MetricType) bool {
        return metric.GetName() != "debug_metric"
    },
})

// Process stream results
for result := range streamCh {
    if result.Error != nil {
        log.Printf("Stream error: %v", result.Error)
        continue
    }
    
    log.Printf("Streamed %d metrics (sequence: %d)", 
        len(result.Metrics), result.Sequence)
}
```

### Graceful Shutdown

```go
// Create metric manager for coordinated shutdown
config := ShutdownConfig{
    GlobalShutdownTimeout: 30 * time.Second,
    ComponentTimeout:      10 * time.Second,
    EnableMetricFlushing:  true,
    EnableResourceTracking: true,
    EnablePhases:          true,
}

manager := NewMetricManager(factory, config, logger)

// Register components for shutdown
manager.RegisterComponent("collector", collector)
manager.RegisterComponent("publisher", publisher)
manager.RegisterComponent("streamer", streamer)

// Add cleanup tasks
manager.AddCleanupTask(func(ctx context.Context) error {
    // Custom cleanup logic
    return cleanupTempFiles(ctx)
})

manager.AddFlushTask(func(ctx context.Context) error {
    // Custom metric flushing
    return flushPendingMetrics(ctx)
})

// Graceful shutdown (automatically handles signals)
// Or manually trigger:
err := manager.Shutdown(ctx)
if err != nil {
    log.Printf("Shutdown error: %v", err)
}

// Get shutdown statistics
stats := manager.GetShutdownStats()
log.Printf("Shutdown completed in %v, %d components, %d errors",
    stats.TotalDuration, stats.ComponentCount, stats.ErrorCount)
```

## 🎯 Performance Characteristics

### Benchmarks

Run comprehensive benchmarks:

```bash
go test -bench=. -benchmem -count=5 ./pkg/metrics/
```

Expected performance on modern hardware:

- **Counter operations**: ~10ns per `Add()` operation
- **Gauge operations**: ~15ns per `Set()` operation  
- **Observer notifications**: ~100ns per event (including buffering)
- **Stream throughput**: >100K metrics/second with compression
- **Memory efficiency**: <1KB per metric instance
- **GC pressure**: Minimal allocations in hot paths

### Memory Optimization

- **Object Pooling**: `sync.Pool` for frequent allocations
- **Atomic Operations**: Lock-free critical paths
- **Ring Buffers**: Efficient circular buffering
- **Zero-Copy**: Minimal data copying in stream processing
- **Compression**: Optional compression for space efficiency

## 🔧 Configuration Examples

### Production Configuration

```go
// High-performance production setup
factoryConfig := FactoryConfig{
    DefaultTimeout:         30 * time.Second,
    DefaultShutdownTimeout: 10 * time.Second,
    DefaultRateLimit:       1000.0,
    DefaultBurstSize:       100,
    MaxClients:            500,
    HealthCheckInterval:   30 * time.Second,
    EnableMetrics:         true,
    EnableTracing:         true,
}

publisherConfig := PublisherConfig{
    DefaultBufferSize:     50000,
    DefaultFlushInterval:  500 * time.Millisecond,
    DefaultFlushThreshold: 1000,
    MaxObservers:          100,
    EnableBatching:        true,
    WorkerPoolSize:        runtime.NumCPU(),
    ErrorStrategy:         "drop", // Drop events under load
}

collectorConfig := CollectorConfig[MetricType]{
    CollectionInterval: 15 * time.Second,
    MaxConcurrency:     20,
    BufferSize:        20000,
    BatchSize:         500,
    
    RateLimit: RateLimitSettings{
        RequestsPerSecond: 500.0,
        BurstSize:        50,
        Enabled:          true,
    },
    
    Backpressure: BackpressureSettings{
        Strategy:      BackpressureStrategyAdaptive,
        BufferSize:    10000,
        DropThreshold: 0.85,
        AlertThreshold: 0.95,
    },
}
```

### Development Configuration

```go
// Development setup with verbose logging
factoryConfig := FactoryConfig{
    DefaultTimeout:       10 * time.Second,
    MaxClients:          10,
    EnableMetrics:       true,
    EnableProfiling:     true,
}

publisherConfig := PublisherConfig{
    DefaultBufferSize:     1000,
    DefaultFlushInterval:  time.Second,
    WorkerPoolSize:        2,
    ErrorStrategy:         "log", // Log all errors
}
```

## 🧪 Testing

### Unit Tests

```bash
# Run all tests
go test ./pkg/metrics/

# Run with race detection
go test -race ./pkg/metrics/

# Run with coverage
go test -cover ./pkg/metrics/
```

### Benchmark Tests

```bash
# Run all benchmarks
go test -bench=. ./pkg/metrics/

# Memory profiling
go test -bench=BenchmarkMemoryEfficiency -memprofile=mem.prof ./pkg/metrics/

# CPU profiling  
go test -bench=BenchmarkCounter -cpuprofile=cpu.prof ./pkg/metrics/

# Concurrent stress testing
go test -bench=BenchmarkConcurrentAccess -cpu=1,2,4,8 ./pkg/metrics/
```

## 🚨 Production Considerations

### Resource Management

- **Memory Limits**: Configure appropriate buffer sizes
- **Worker Pools**: Size pools based on workload characteristics  
- **Rate Limiting**: Prevent system overload
- **Circuit Breakers**: Handle downstream failures gracefully

### Monitoring

- **Internal Metrics**: The package exposes its own performance metrics
- **Health Checks**: Monitor component health continuously
- **Alerts**: Configure alerts for errors and performance degradation

### Scalability

- **Horizontal Scaling**: Factory supports multiple client instances
- **Vertical Scaling**: Worker pools auto-scale based on load
- **Resource Isolation**: Components are independently manageable

## 🔗 Integration Examples

### With Kubernetes

```go
// Service discovery integration
discoveryClient := discovery.NewKubernetesDiscovery(...)
metricsClient := metrics.NewPrometheusFactory(...)

// Automatic service metric collection
collector := NewServiceMetricsCollector(discoveryClient, metricsClient)
```

### With OTEL (Phase 3)

```go
// OTEL trace integration (next phase)
tracingConfig := OTELConfig{
    ServiceName: "tapio-metrics",
    Endpoint:   "http://jaeger:14268/api/traces",
}

// Instrument metrics with tracing
instrumentedFactory := WithOTELTracing(factory, tracingConfig)
```

## 📊 Monitoring Dashboard

Monitor the metrics package itself:

```yaml
# Prometheus queries for monitoring
- name: metrics_package_performance
  queries:
    - metric_events_published_total
    - metric_collection_duration_seconds
    - metric_stream_throughput_ops_per_sec
    - metric_factory_clients_created_total
    - metric_observer_errors_total
    - metric_backpressure_drops_total
```

## 🎓 Learning Outcomes

This package demonstrates enterprise-grade Go patterns:

1. **Type Safety**: Go generics for compile-time guarantees
2. **Performance**: Atomic operations and memory optimization  
3. **Concurrency**: Worker pools and lock-free data structures
4. **Resilience**: Circuit breakers and graceful degradation
5. **Observability**: Built-in metrics and comprehensive logging
6. **Maintainability**: Clean interfaces and dependency injection

The implementation showcases production-ready Go code suitable for high-scale environments with proper error handling, resource management, and performance optimization.