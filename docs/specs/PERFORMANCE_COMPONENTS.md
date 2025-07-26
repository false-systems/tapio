# Tapio Performance Components Documentation

## Overview

The Tapio performance package provides high-performance, lock-free data structures extracted from our production systems. These components are designed to handle 165,000+ events per second with minimal CPU and memory overhead.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Application Layer                        │
├─────────────────────────────────────────────────────────────┤
│  gRPC Services │ Collectors │ Correlation │ Analytics       │
├─────────────────────────────────────────────────────────────┤
│                  Performance Components                      │
│  ┌────────────┐  ┌──────────────┐  ┌─────────────┐        │
│  │Ring Buffer │  │ Event Buffer │  │Object Pool  │        │
│  │  (MPMC)    │  │  (Type-safe) │  │ (Per-CPU)   │        │
│  └────────────┘  └──────────────┘  └─────────────┘        │
│  ┌─────────────────┐  ┌────────────────────────┐           │
│  │ Per-CPU Buffer  │  │ Per-CPU Event Buffer   │           │
│  │ (Cache-aligned) │  │ (Type-safe + Per-CPU)  │           │
│  └─────────────────┘  └────────────────────────┘           │
└─────────────────────────────────────────────────────────────┘
```

## Component Details

### 1. Ring Buffer

A lock-free, multi-producer multi-consumer (MPMC) ring buffer using atomic operations.

**Key Features:**
- Zero allocations during operation
- Cache-line padding prevents false sharing
- Power-of-2 sizing for efficient modulo operations
- Wait-free producers, lock-free consumers

**Performance Characteristics:**
- Throughput: 10M+ ops/sec per core
- Latency: ~90ns per operation
- Memory: Fixed allocation, no GC pressure

**Use Cases:**
- High-volume event buffering
- Cross-thread communication
- Backpressure handling

### 2. Event Buffer

Type-safe wrapper around Ring Buffer specifically for UnifiedEvent processing.

**Key Features:**
- No unsafe pointer handling required
- Batch operations for efficiency
- Non-blocking Try operations
- Built-in statistics and monitoring

**API:**
```go
// Basic operations
Put(event *UnifiedEvent) error
Get() (*UnifiedEvent, error)

// Non-blocking
TryPut(event *UnifiedEvent) bool
TryGet() (*UnifiedEvent, bool)

// Batch operations
PutBatch(events []*UnifiedEvent) (int, error)
GetBatch(maxCount int) ([]*UnifiedEvent, error)
DrainTo(events []*UnifiedEvent) int
```

### 3. Object Pool

Generic object pooling with per-CPU optimization to reduce GC pressure.

**Key Features:**
- Per-CPU pools minimize contention
- Generic implementation works with any type
- Automatic object reset before reuse
- Specialized pools for common types

**Specialized Pools:**
- `UnifiedEventPool`: Pre-initialized event objects
- `ByteSlicePool`: Common buffer sizes (64B, 512B, 4KB, 64KB)

**Benefits:**
- 90%+ reduction in allocations
- Sub-microsecond Get/Put operations
- Predictable memory usage

### 4. Per-CPU Buffer

CPU-local buffers that eliminate cache-line contention for ultra-high throughput scenarios.

**Key Features:**
- Each CPU gets its own buffer
- Zero contention between CPUs
- Automatic overflow handling
- Lock-free circular buffer per CPU
- 128-byte cache-line padding

**Performance Characteristics:**
- Throughput: 1M+ events/sec per CPU
- Zero false sharing between CPUs
- Predictable latency under load

**Use Cases:**
- eBPF event collection
- High-frequency metrics
- Network packet processing
- CPU-intensive collectors

### 5. Per-CPU Event Buffer

Type-safe wrapper combining per-CPU buffers with UnifiedEvent handling.

**Key Features:**
- All benefits of per-CPU buffers
- Direct UnifiedEvent support
- Optional object pooling
- Per-CPU metrics and monitoring

**API:**
```go
// Basic operations
Put(event *UnifiedEvent) error
PutBatch(events []*UnifiedEvent) (int, error)

// Read from all CPUs
Get() ([]*UnifiedEvent, error)

// Read from specific CPU
GetFromCPU(cpu int) ([]*UnifiedEvent, error)

// Process without removing
Process(fn func(*UnifiedEvent) error) error
```

## Integration Guide

### Basic Integration Pattern

```go
package main

import (
    "github.com/yairfalse/tapio/pkg/performance"
    "github.com/yairfalse/tapio/pkg/domain"
)

type Service struct {
    buffer    *performance.EventBuffer
    eventPool *performance.UnifiedEventPool
}

func NewService() (*Service, error) {
    // Create buffer with 64k capacity
    buffer, err := performance.NewEventBuffer(65536)
    if err != nil {
        return nil, err
    }
    
    // Create event pool
    eventPool := performance.NewUnifiedEventPool()
    
    return &Service{
        buffer:    buffer,
        eventPool: eventPool,
    }, nil
}

func (s *Service) ProcessEvent(data []byte) error {
    // Get event from pool
    event := s.eventPool.Get()
    defer s.eventPool.Put(event)
    
    // Fill event
    event.ID = domain.GenerateEventID()
    event.Type = "example"
    event.RawData = data
    
    // Submit to buffer
    return s.buffer.Put(event)
}
```

### Integration with gRPC Services

```go
// In your EventService
func (s *EventService) StreamEvents(req *pb.StreamRequest, stream pb.EventService_StreamEventsServer) error {
    // Create performance components
    buffer, _ := performance.NewEventBuffer(65536)
    pool := performance.NewUnifiedEventPool()
    
    // Producer goroutine
    go func() {
        for {
            event := pool.Get()
            // ... fill event from source ...
            buffer.TryPut(event)
        }
    }()
    
    // Stream to client
    for {
        event, err := buffer.Get()
        if err != nil {
            continue
        }
        
        // Convert to proto
        pbEvent := convertToProto(event)
        
        // Send
        if err := stream.Send(pbEvent); err != nil {
            pool.Put(event)
            return err
        }
        
        pool.Put(event)
    }
}
```

### Integration with Per-CPU Buffers

```go
// Ultra-high performance collector
type HighSpeedCollector struct {
    pcBuffer  *performance.PerCPUEventBuffer
    fallback  *performance.EventBuffer
}

func NewHighSpeedCollector() (*HighSpeedCollector, error) {
    // Per-CPU buffer for normal operation
    pcBuffer, err := performance.NewPerCPUEventBuffer(
        performance.PerCPUEventBufferConfig{
            BufferSizePerCPU: 512 * 1024,  // 512KB per CPU
            OverflowSize:     8 * 1024 * 1024, // 8MB overflow
            EnablePooling:    true,
        })
    if err != nil {
        return nil, err
    }
    
    // Fallback buffer for extreme load
    fallback, _ := performance.NewEventBuffer(131072)
    
    return &HighSpeedCollector{
        pcBuffer: pcBuffer,
        fallback: fallback,
    }, nil
}

func (c *HighSpeedCollector) CollectEvent(event *domain.UnifiedEvent) {
    // Try per-CPU buffer first (no contention)
    if err := c.pcBuffer.Put(event); err != nil {
        // Overflow to fallback
        c.fallback.TryPut(event)
    }
}
```

### Integration with Collectors

```go
// eBPF Collector Example
type EBPFCollector struct {
    buffer    *performance.EventBuffer
    eventPool *performance.UnifiedEventPool
    bytePool  *performance.ByteSlicePool
}

func (c *EBPFCollector) handleKernelEvent(raw []byte) {
    // Get pooled resources
    event := c.eventPool.Get()
    
    // Parse kernel data
    event.Type = domain.EventType("syscall")
    event.Source = "ebpf"
    event.Kernel = &domain.KernelData{
        Syscall: parseSyscall(raw),
        PID:     parsePID(raw),
    }
    
    // Copy raw data using pooled buffer
    pooledData := c.bytePool.Get(len(raw))
    copy(pooledData, raw)
    event.RawData = pooledData
    
    // Submit event
    if err := c.buffer.Put(event); err != nil {
        // Buffer full, return resources
        c.bytePool.Put(pooledData)
        c.eventPool.Put(event)
    }
}
```

### Integration with Correlation Engine

```go
// High-performance correlation
type OptimizedCorrelation struct {
    criticalEvents *performance.EventBuffer
    normalEvents   *performance.EventBuffer
    eventPool      *performance.UnifiedEventPool
}

func (c *OptimizedCorrelation) ProcessEvent(event *domain.UnifiedEvent) error {
    // Route by priority
    if event.GetSeverity() == "critical" {
        return c.criticalEvents.Put(event)
    }
    return c.normalEvents.Put(event)
}

func (c *OptimizedCorrelation) correlateWorker() {
    for {
        // Process critical first
        if event, err := c.criticalEvents.Get(); err == nil {
            c.correlate(event)
            c.eventPool.Put(event)
            continue
        }
        
        // Then normal
        if event, err := c.normalEvents.Get(); err == nil {
            c.correlate(event)
            c.eventPool.Put(event)
        }
    }
}
```

## Configuration Guidelines

### Buffer Sizing

Choose buffer sizes based on:
- Event rate
- Processing latency
- Memory constraints

**Recommendations:**
```
eBPF Collector:     128k-256k events (high volume)
K8s Collector:      16k-32k events (moderate)
App Logs:           32k-64k events (variable)
Correlation:        64k-128k events (aggregated)
```

### Worker Count

```go
workers := runtime.NumCPU() / 2  // Start conservative
if highThroughput {
    workers = runtime.NumCPU()   // Use all cores
}
```

### Monitoring

```go
// Monitor buffer health
go func() {
    ticker := time.NewTicker(5 * time.Second)
    for range ticker.C {
        stats := buffer.GetStats()
        fillRate := float64(stats.Size) / float64(stats.Capacity)
        
        if fillRate > 0.8 {
            log.Warn("Buffer near capacity", "fill_rate", fillRate)
            // Scale up workers or apply backpressure
        }
        
        metrics.BufferSize.Set(float64(stats.Size))
        metrics.BufferCapacity.Set(float64(stats.Capacity))
    }
}()
```

## Performance Tuning

### CPU Affinity

For maximum performance, pin workers to CPUs:

```go
// In your service
func (s *Service) startWorker(cpuID int) {
    // Pin to CPU (requires runtime hacks)
    runtime.LockOSThread()
    
    // Process events on this CPU
    s.processEvents()
}
```

### Batch Processing

Always prefer batch operations:

```go
// Good - batch processing
events := make([]*UnifiedEvent, 100)
count := buffer.DrainTo(events)
processBatch(events[:count])

// Less efficient - individual
for i := 0; i < 100; i++ {
    event, _ := buffer.Get()
    process(event)
}
```

### Memory Optimization

```go
// Pre-allocate pools at startup
func initPools() {
    pool := performance.NewUnifiedEventPool()
    
    // Pre-warm the pool
    events := make([]*domain.UnifiedEvent, 1000)
    for i := range events {
        events[i] = pool.Get()
    }
    for _, e := range events {
        pool.Put(e)
    }
}
```

## Common Patterns

### 1. Multi-Stage Pipeline

```go
type Pipeline struct {
    stage1 *performance.EventBuffer
    stage2 *performance.EventBuffer
    pool   *performance.UnifiedEventPool
}

func (p *Pipeline) Run() {
    // Stage 1: Ingestion
    go p.ingest()
    
    // Stage 2: Enrichment
    go p.enrich()
    
    // Stage 3: Output
    go p.output()
}
```

### 2. Priority Queues

```go
type PriorityProcessor struct {
    critical  *performance.EventBuffer
    high      *performance.EventBuffer
    normal    *performance.EventBuffer
    low       *performance.EventBuffer
}

func (p *PriorityProcessor) Route(event *UnifiedEvent) {
    switch event.GetSeverity() {
    case "critical":
        p.critical.TryPut(event)
    case "high":
        p.high.TryPut(event)
    case "medium":
        p.normal.TryPut(event)
    default:
        p.low.TryPut(event)
    }
}
```

### 3. Fan-out Processing

```go
type FanOut struct {
    input   *performance.EventBuffer
    outputs []*performance.EventBuffer
}

func (f *FanOut) Distribute() {
    for {
        event, _ := f.input.Get()
        
        // Round-robin distribution
        for _, output := range f.outputs {
            if output.TryPut(event) {
                break
            }
        }
    }
}
```

## Troubleshooting

### High Memory Usage

**Symptoms:** Growing memory despite pooling

**Solutions:**
1. Check pool return discipline
2. Verify buffer sizes are appropriate
3. Monitor pool statistics

```go
stats := pool.GetStats()
if stats.InUse > stats.Allocated * 0.9 {
    // Pool exhaustion - events not being returned
}
```

### Buffer Overflow

**Symptoms:** Events dropped, Put() errors

**Solutions:**
1. Increase buffer size
2. Add more workers
3. Implement backpressure

```go
if err := buffer.Put(event); err != nil {
    // Apply backpressure
    metrics.DroppedEvents.Inc()
    time.Sleep(time.Millisecond)
}
```

### Poor Performance

**Symptoms:** Lower than expected throughput

**Solutions:**
1. Check for lock contention
2. Verify power-of-2 buffer sizes
3. Use batch operations
4. Profile with pprof

## Benchmarks

Run benchmarks to verify performance:

```bash
go test -bench=. -benchmem ./pkg/performance/...
```

Expected results:
```
BenchmarkRingBuffer/Put-8         12,000,000    89 ns/op     0 B/op    0 allocs/op
BenchmarkRingBuffer/Get-8         10,000,000   103 ns/op     0 B/op    0 allocs/op
BenchmarkEventPool/GetPut-8       40,000,000    25 ns/op     0 B/op    0 allocs/op
BenchmarkBatch/Process100-8        2,000,000   612 ns/op     0 B/op    0 allocs/op
```

## Migration Guide

### From Channels

```go
// Before: channels
ch := make(chan *Event, 1000)
ch <- event
event := <-ch

// After: ring buffer
buffer, _ := performance.NewEventBuffer(1024)
buffer.Put(event)
event, _ := buffer.Get()
```

### From sync.Pool

```go
// Before: sync.Pool
pool := &sync.Pool{New: func() interface{} { return &Event{} }}
event := pool.Get().(*Event)
pool.Put(event)

// After: typed pool
pool := performance.NewUnifiedEventPool()
event := pool.Get()
pool.Put(event)
```

## Best Practices Summary

1. **Always return objects to pools**
2. **Use batch operations when possible**
3. **Monitor buffer fill rates**
4. **Size buffers appropriately**
5. **Handle backpressure gracefully**
6. **Pre-warm pools at startup**
7. **Use non-blocking operations in hot paths**
8. **Profile regularly**