# Performance Tuning Guide

## Overview

This guide provides comprehensive performance tuning procedures for Tapio production deployments. It covers system optimization, bottleneck identification, and performance monitoring strategies to achieve the target performance metrics.

## Performance Targets

### Primary Metrics

- **Event Processing Rate**: 500,000+ events/second per node
- **Processing Latency**: <500µs per event (99th percentile)
- **Memory Usage**: <100MB per node for eBPF buffers
- **CPU Overhead**: <1% system impact
- **Response Time**: `tapio check` in <2 seconds
- **Filter Efficiency**: 97% event reduction (165k → 5k relevant)

### Secondary Metrics

- **Correlation Accuracy**: >98%
- **False Positive Rate**: <2%
- **Signal-to-Noise Ratio**: >95%
- **Data Quality**: 100% semantic enrichment
- **Availability**: 99.9% uptime

## System Architecture Optimization

### eBPF Performance Tuning

#### Kernel Space Optimization

1. **Ring Buffer Configuration**
   ```yaml
   # eBPF collector configuration
   ebpf:
     ring_buffer:
       size: 32MB        # Per-CPU ring buffer size
       batch_size: 1024  # Events per batch
       timeout: 1ms      # Batch timeout
   ```

2. **Map Optimization**
   ```c
   // Optimize eBPF map sizes
   struct {
       __uint(type, BPF_MAP_TYPE_HASH);
       __uint(max_entries, 65536);  // Tune based on workload
       __type(key, u32);
       __type(value, struct event_data);
   } events_map SEC(".maps");
   ```

3. **Event Filtering**
   ```c
   // Aggressive kernel-side filtering
   SEC("tracepoint/syscalls/sys_enter_openat")
   int trace_openat(struct trace_event_raw_sys_enter* ctx) {
       // Filter by process, file patterns, etc.
       if (should_ignore_event(ctx)) {
           return 0;  // Drop event in kernel
       }
       // Process only relevant events
       return handle_event(ctx);
   }
   ```

#### User Space Optimization

1. **Event Processing Pipeline**
   ```go
   // High-performance event processor
   type EventProcessor struct {
       ringBuffer   *ebpf.RingBuffer
       batchSize    int
       workerPool   *WorkerPool
       eventCache   *sync.Pool
   }
   
   func (ep *EventProcessor) ProcessEvents() {
       // Use object pooling to reduce GC pressure
       batch := ep.eventCache.Get().([]*Event)
       defer ep.eventCache.Put(batch[:0])
       
       // Batch process events
       for len(batch) < ep.batchSize {
           event, err := ep.ringBuffer.Read()
           if err != nil {
               break
           }
           batch = append(batch, event)
       }
       
       // Parallel processing
       ep.workerPool.Process(batch)
   }
   ```

2. **Memory Management**
   ```go
   // Pre-allocated buffers
   var eventPool = sync.Pool{
       New: func() interface{} {
           return make([]*Event, 0, 1024)
       },
   }
   
   // Zero-copy event parsing
   func parseEvent(data []byte) *Event {
       // Use unsafe package for zero-copy parsing
       // when performance is critical
       event := (*Event)(unsafe.Pointer(&data[0]))
       return event
   }
   ```

### Application Performance Tuning

#### Go Runtime Optimization

1. **Garbage Collector Tuning**
   ```bash
   # Environment variables for production
   export GOGC=200                    # Reduce GC frequency
   export GOMAXPROCS=8               # Match CPU cores
   export GODEBUG=gctrace=1          # Monitor GC performance
   ```

2. **Memory Pool Configuration**
   ```go
   // Object pooling for high-frequency allocations
   var correlationPool = sync.Pool{
       New: func() interface{} {
           return &CorrelationContext{
               Events:     make([]*Event, 0, 100),
               Metadata:   make(map[string]interface{}, 10),
               Timestamps: make([]time.Time, 0, 100),
           }
       },
   }
   ```

3. **Goroutine Pool Management**
   ```go
   // Fixed-size worker pool
   type WorkerPool struct {
       workers    chan chan Job
       jobQueue   chan Job
       maxWorkers int
   }
   
   func NewWorkerPool(maxWorkers int) *WorkerPool {
       pool := &WorkerPool{
           workers:    make(chan chan Job, maxWorkers),
           jobQueue:   make(chan Job, maxWorkers*2),
           maxWorkers: maxWorkers,
       }
       
       // Pre-create workers
       for i := 0; i < maxWorkers; i++ {
           worker := NewWorker(pool.workers)
           worker.Start()
       }
       
       return pool
   }
   ```

#### Database Optimization

1. **Connection Pool Tuning**
   ```yaml
   database:
     max_open_conns: 25      # Maximum open connections
     max_idle_conns: 5       # Idle connections to maintain
     conn_max_lifetime: 1h   # Connection lifetime
     conn_max_idle_time: 15m # Idle timeout
   ```

2. **Query Optimization**
   ```sql
   -- Create efficient indexes
   CREATE INDEX CONCURRENTLY idx_events_timestamp_filtered 
   ON events (timestamp DESC) 
   WHERE event_type IN ('security', 'performance', 'error');
   
   -- Partition large tables
   CREATE TABLE events_2024_01 PARTITION OF events
   FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
   ```

3. **Batch Operations**
   ```go
   // Batch inserts for better performance
   func (db *Database) BatchInsertEvents(events []*Event) error {
       const batchSize = 1000
       
       for i := 0; i < len(events); i += batchSize {
           end := i + batchSize
           if end > len(events) {
               end = len(events)
           }
           
           if err := db.insertBatch(events[i:end]); err != nil {
               return err
           }
       }
       
       return nil
   }
   ```

### Kubernetes Resource Optimization

#### Pod Resource Configuration

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tapio-server
spec:
  template:
    spec:
      containers:
      - name: server
        resources:
          requests:
            cpu: "2"           # Guaranteed CPU
            memory: "4Gi"      # Guaranteed memory
          limits:
            cpu: "4"           # Maximum CPU burst
            memory: "8Gi"      # Memory limit
        env:
        - name: GOMAXPROCS
          valueFrom:
            resourceFieldRef:
              resource: limits.cpu
```

#### Node Affinity and Scheduling

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tapio-agent
spec:
  template:
    spec:
      # Ensure agents run on performance-optimized nodes
      nodeSelector:
        node-type: "high-performance"
      
      # Prefer nodes with faster storage
      affinity:
        nodeAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            preference:
              matchExpressions:
              - key: "storage-type"
                operator: In
                values: ["nvme"]
```

#### Priority Classes

```yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: tapio-critical
value: 1000
globalDefault: false
description: "Critical Tapio components"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tapio-server
spec:
  template:
    spec:
      priorityClassName: tapio-critical
```

## Performance Monitoring and Profiling

### Built-in Profiling

1. **CPU Profiling**
   ```go
   import _ "net/http/pprof"
   
   // Enable profiling endpoint
   go func() {
       log.Println(http.ListenAndServe("localhost:6060", nil))
   }()
   ```

2. **Memory Profiling**
   ```bash
   # Capture heap profile
   curl http://localhost:6060/debug/pprof/heap > heap.prof
   go tool pprof heap.prof
   
   # Analyze memory allocations
   curl http://localhost:6060/debug/pprof/allocs > allocs.prof
   go tool pprof allocs.prof
   ```

3. **Goroutine Analysis**
   ```bash
   # Check goroutine count
   curl http://localhost:6060/debug/pprof/goroutine?debug=1
   
   # Analyze goroutine profiles
   curl http://localhost:6060/debug/pprof/goroutine > goroutine.prof
   go tool pprof goroutine.prof
   ```

### Metrics Collection

1. **Custom Metrics**
   ```go
   var (
       eventsProcessed = prometheus.NewCounterVec(
           prometheus.CounterOpts{
               Name: "tapio_events_processed_total",
               Help: "Total number of events processed",
           },
           []string{"node", "type"},
       )
       
       processingDuration = prometheus.NewHistogramVec(
           prometheus.HistogramOpts{
               Name:    "tapio_event_processing_duration_seconds",
               Help:    "Event processing duration",
               Buckets: prometheus.ExponentialBuckets(0.000001, 2, 20),
           },
           []string{"stage"},
       )
   )
   ```

2. **Performance Dashboards**
   ```json
   {
     "dashboard": {
       "title": "Tapio Performance",
       "panels": [
         {
           "title": "Event Processing Rate",
           "targets": [
             {
               "expr": "rate(tapio_events_processed_total[5m])",
               "legendFormat": "{{node}}"
             }
           ]
         },
         {
           "title": "Processing Latency P99",
           "targets": [
             {
               "expr": "histogram_quantile(0.99, rate(tapio_event_processing_duration_seconds_bucket[5m]))",
               "legendFormat": "P99 Latency"
             }
           ]
         }
       ]
     }
   }
   ```

### Benchmark Testing

1. **Load Testing**
   ```go
   func BenchmarkEventProcessing(b *testing.B) {
       processor := NewEventProcessor()
       events := generateTestEvents(1000)
       
       b.ResetTimer()
       b.RunParallel(func(pb *testing.PB) {
           for pb.Next() {
               processor.ProcessBatch(events)
           }
       })
   }
   ```

2. **Stress Testing**
   ```bash
   # Generate high event load
   kubectl run load-generator \
     --image=tapio/load-generator \
     --env="EVENTS_PER_SECOND=100000" \
     --env="DURATION=300s"
   ```

## Bottleneck Identification

### Common Performance Bottlenecks

#### 1. eBPF Ring Buffer Overflow

**Symptoms:**
- Dropped events in kernel logs
- High ring buffer utilization
- Event loss metrics increasing

**Diagnosis:**
```bash
# Check ring buffer stats
cat /sys/kernel/debug/tracing/ring_buffer_stats

# Monitor eBPF map usage
bpftool map dump name events_map | wc -l
```

**Solution:**
```yaml
# Increase buffer sizes
ebpf:
  ring_buffer:
    size: 64MB  # Increase from 32MB
    pages: 16384  # Increase page count
```

#### 2. CPU Saturation

**Symptoms:**
- High CPU usage (>80%)
- Increased processing latency
- Context switching overhead

**Diagnosis:**
```bash
# Check CPU usage patterns
kubectl top pods -n tapio-system --sort-by=cpu

# Analyze CPU profiling
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
```

**Solution:**
```yaml
# Scale horizontally
replicas: 10  # Increase from 5

# Or scale vertically
resources:
  limits:
    cpu: "8"  # Increase CPU allocation
```

#### 3. Memory Pressure

**Symptoms:**
- High memory usage
- Frequent garbage collection
- Memory allocation errors

**Diagnosis:**
```bash
# Check memory usage
kubectl top pods -n tapio-system --sort-by=memory

# Analyze heap profile
go tool pprof http://localhost:6060/debug/pprof/heap
```

**Solution:**
```go
// Implement object pooling
var eventPool = sync.Pool{
    New: func() interface{} {
        return &Event{}
    },
}

// Use pool in hot paths
event := eventPool.Get().(*Event)
defer eventPool.Put(event)
```

#### 4. Database Contention

**Symptoms:**
- Slow query performance
- High database CPU
- Lock wait timeouts

**Diagnosis:**
```sql
-- Check slow queries
SELECT query, mean_time, calls 
FROM pg_stat_statements 
ORDER BY mean_time DESC 
LIMIT 10;

-- Check lock contention
SELECT * FROM pg_stat_activity 
WHERE state = 'active' AND waiting;
```

**Solution:**
```sql
-- Optimize queries
CREATE INDEX CONCURRENTLY idx_events_compound 
ON events (timestamp, event_type, node_id);

-- Partition tables
CREATE TABLE events_partition_2024_01 
PARTITION OF events 
FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
```

#### 5. Network Bottlenecks

**Symptoms:**
- High network latency
- Packet drops
- Service mesh overhead

**Diagnosis:**
```bash
# Check network metrics
kubectl top nodes | grep -E "(CPU|MEMORY|NETWORK)"

# Test network performance
kubectl run netperf --image=networkstatic/netperf -- sleep 3600
kubectl exec -it netperf -- netperf -H tapio-server.tapio-system.svc.cluster.local
```

**Solution:**
```yaml
# Optimize service mesh
spec:
  proxy:
    config:
      concurrency: 4  # Match CPU cores
      resources:
        requests:
          cpu: 100m
          memory: 128Mi
```

## Advanced Optimization Techniques

### Lock-Free Data Structures

```go
// Lock-free ring buffer for high-throughput scenarios
type LockFreeRingBuffer struct {
    buffer []unsafe.Pointer
    mask   uint64
    head   uint64
    tail   uint64
}

func (rb *LockFreeRingBuffer) Push(item unsafe.Pointer) bool {
    head := atomic.LoadUint64(&rb.head)
    next := (head + 1) & rb.mask
    
    if next == atomic.LoadUint64(&rb.tail) {
        return false // Buffer full
    }
    
    rb.buffer[head] = item
    atomic.StoreUint64(&rb.head, next)
    return true
}
```

### NUMA Awareness

```go
// Pin goroutines to specific CPU cores
func pinToCPU(cpuID int) {
    mask := unix.CPUSet{}
    mask.Set(cpuID)
    unix.SchedSetaffinity(0, &mask)
}

// Allocate memory on specific NUMA nodes
func allocateOnNode(size int, node int) []byte {
    // Use numa library or mmap with NUMA policies
    return make([]byte, size)
}
```

### Zero-Copy Optimization

```go
// Zero-copy event parsing using unsafe
func parseEventZeroCopy(data []byte) *Event {
    if len(data) < eventHeaderSize {
        return nil
    }
    
    // Cast bytes directly to struct (unsafe but fast)
    header := (*EventHeader)(unsafe.Pointer(&data[0]))
    
    // Validate header before proceeding
    if header.Magic != eventMagic {
        return nil
    }
    
    return &Event{
        Header:  header,
        Payload: data[eventHeaderSize:],
    }
}
```

### Batch Processing Optimization

```go
// Adaptive batch sizing based on load
type AdaptiveBatcher struct {
    minBatchSize int
    maxBatchSize int
    currentSize  int
    lastLatency  time.Duration
}

func (ab *AdaptiveBatcher) adjustBatchSize() {
    if ab.lastLatency > targetLatency {
        // Reduce batch size to improve latency
        ab.currentSize = max(ab.minBatchSize, ab.currentSize/2)
    } else {
        // Increase batch size to improve throughput
        ab.currentSize = min(ab.maxBatchSize, ab.currentSize*2)
    }
}
```

## Performance Testing Framework

### Automated Performance Tests

```go
// Performance test suite
func TestEventProcessingPerformance(t *testing.T) {
    tests := []struct {
        name           string
        eventsPerSec   int
        duration       time.Duration
        maxLatencyP99  time.Duration
    }{
        {"Low Load", 10000, 60 * time.Second, 1 * time.Millisecond},
        {"Medium Load", 50000, 60 * time.Second, 5 * time.Millisecond},
        {"High Load", 100000, 60 * time.Second, 10 * time.Millisecond},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := runPerformanceTest(tt.eventsPerSec, tt.duration)
            
            if result.LatencyP99 > tt.maxLatencyP99 {
                t.Errorf("P99 latency %v exceeds limit %v", 
                    result.LatencyP99, tt.maxLatencyP99)
            }
        })
    }
}
```

### Continuous Performance Monitoring

```yaml
# Performance test job
apiVersion: batch/v1
kind: CronJob
metadata:
  name: performance-test
spec:
  schedule: "0 */6 * * *"  # Every 6 hours
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: perf-test
            image: tapio/performance-tester
            env:
            - name: TARGET_RPS
              value: "50000"
            - name: TEST_DURATION
              value: "300s"
```

## Performance Regression Detection

### Automated Performance Regression Tests

```go
// Performance regression detector
type PerformanceRegression struct {
    baseline    PerformanceMetrics
    threshold   float64  // 10% regression threshold
    history     []PerformanceMetrics
}

func (pr *PerformanceRegression) checkRegression(current PerformanceMetrics) bool {
    latencyIncrease := (current.LatencyP99 - pr.baseline.LatencyP99) / pr.baseline.LatencyP99
    throughputDecrease := (pr.baseline.Throughput - current.Throughput) / pr.baseline.Throughput
    
    return latencyIncrease > pr.threshold || throughputDecrease > pr.threshold
}
```

### Performance Alerts

```yaml
# Prometheus alerting rules
groups:
- name: performance
  rules:
  - alert: HighLatency
    expr: histogram_quantile(0.99, rate(tapio_event_processing_duration_seconds_bucket[5m])) > 0.005
    for: 2m
    labels:
      severity: warning
    annotations:
      summary: "Event processing latency is high"
      description: "P99 latency is {{ $value }}s, exceeding 5ms threshold"
      
  - alert: LowThroughput
    expr: rate(tapio_events_processed_total[5m]) < 10000
    for: 5m
    labels:
      severity: critical
    annotations:
      summary: "Event processing throughput is low"
      description: "Processing only {{ $value }} events/sec, below 10k threshold"
```

## Performance Troubleshooting Playbook

### Performance Issue Investigation Process

1. **Initial Assessment**
   ```bash
   # Quick health check
   kubectl get pods -n tapio-system
   kubectl top nodes
   kubectl top pods -n tapio-system
   ```

2. **Metrics Analysis**
   ```bash
   # Check key performance metrics
   curl http://prometheus:9090/api/v1/query?query=rate(tapio_events_processed_total[5m])
   curl http://prometheus:9090/api/v1/query?query=histogram_quantile(0.99,rate(tapio_event_processing_duration_seconds_bucket[5m]))
   ```

3. **Profiling Analysis**
   ```bash
   # CPU profiling
   kubectl port-forward deployment/tapio-server 6060:6060 -n tapio-system
   go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
   
   # Memory profiling
   go tool pprof http://localhost:6060/debug/pprof/heap
   ```

4. **System Resource Analysis**
   ```bash
   # Check system resources on nodes
   kubectl exec -it daemonset/node-exporter -- cat /proc/loadavg
   kubectl exec -it daemonset/node-exporter -- free -h
   kubectl exec -it daemonset/node-exporter -- iostat -x 1 5
   ```

### Common Performance Issues and Solutions

| Issue | Symptoms | Solution |
|-------|----------|----------|
| Ring Buffer Overflow | Event drops, high kernel CPU | Increase buffer size, optimize filtering |
| Memory Pressure | High GC time, OOM kills | Add memory, implement pooling |
| CPU Saturation | High CPU usage, slow processing | Scale out, optimize algorithms |
| Database Contention | Slow queries, lock waits | Optimize indexes, use partitioning |
| Network Latency | Slow service calls | Optimize service mesh, use local caching |

This performance tuning guide provides a comprehensive framework for optimizing Tapio's performance in production environments. Regular application of these techniques and continuous monitoring will help maintain the target performance metrics.