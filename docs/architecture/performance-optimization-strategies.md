# Performance Optimization Strategies

## Overview

This document outlines comprehensive performance optimization strategies for achieving 1M+ events/sec processing with <1ms p99 latency in Tapio's correlation engine. These strategies are based on production-proven patterns from high-performance systems at scale.

## Performance Targets

| Metric | Target | Measurement Method |
|--------|--------|-------------------|
| Event Ingestion Rate | 1M+ events/sec | Ring buffer throughput |
| Processing Latency (p99) | <1ms | End-to-end event latency |
| Memory Usage | <500MB baseline | RSS memory monitoring |
| CPU Usage | <10% @ 100K eps | CPU profiling |
| GC Pause Time (p99) | <10ms | GC statistics |
| Timeline Query | <100μs | Query benchmarks |

## Architecture Optimizations

### Lock-Free Data Structures

#### Ring Buffer Implementation

```go
type LockFreeRingBuffer struct {
    buffer   []unsafe.Pointer
    capacity uint32
    mask     uint32
    head     uint64
    tail     uint64
    _pad1    [64]byte // Prevent false sharing
}

func NewLockFreeRingBuffer(capacity uint32) *LockFreeRingBuffer {
    // Ensure power of 2 for fast modulo
    capacity = nextPowerOf2(capacity)
    
    return &LockFreeRingBuffer{
        buffer:   make([]unsafe.Pointer, capacity),
        capacity: capacity,
        mask:     capacity - 1,
    }
}

func (rb *LockFreeRingBuffer) Push(item unsafe.Pointer) bool {
    for {
        head := atomic.LoadUint64(&rb.head)
        tail := atomic.LoadUint64(&rb.tail)
        
        if head-tail >= uint64(rb.capacity) {
            return false // Buffer full
        }
        
        if atomic.CompareAndSwapUint64(&rb.head, head, head+1) {
            rb.buffer[head&uint64(rb.mask)] = item
            return true
        }
    }
}

func (rb *LockFreeRingBuffer) Pop() unsafe.Pointer {
    for {
        tail := atomic.LoadUint64(&rb.tail)
        head := atomic.LoadUint64(&rb.head)
        
        if tail >= head {
            return nil // Buffer empty
        }
        
        item := atomic.LoadPointer(&rb.buffer[tail&uint64(rb.mask)])
        if item == nil {
            continue // Not yet written
        }
        
        if atomic.CompareAndSwapUint64(&rb.tail, tail, tail+1) {
            return item
        }
    }
}
```

#### MPMC Queue

```go
type MPMCQueue struct {
    capacity uint64
    mask     uint64
    buffer   []slot
    _pad1    [64]byte
    head     uint64
    _pad2    [64]byte
    tail     uint64
    _pad3    [64]byte
}

type slot struct {
    sequence uint64
    data     unsafe.Pointer
}

func (q *MPMCQueue) Enqueue(item unsafe.Pointer) bool {
    for {
        pos := atomic.LoadUint64(&q.head)
        slot := &q.buffer[pos&q.mask]
        seq := atomic.LoadUint64(&slot.sequence)
        
        if seq == pos {
            if atomic.CompareAndSwapUint64(&q.head, pos, pos+1) {
                slot.data = item
                atomic.StoreUint64(&slot.sequence, pos+1)
                return true
            }
        } else if seq < pos {
            return false // Queue full
        }
    }
}

func (q *MPMCQueue) Dequeue() unsafe.Pointer {
    for {
        pos := atomic.LoadUint64(&q.tail)
        slot := &q.buffer[pos&q.mask]
        seq := atomic.LoadUint64(&slot.sequence)
        
        if seq == pos+1 {
            if atomic.CompareAndSwapUint64(&q.tail, pos, pos+1) {
                data := slot.data
                slot.data = nil
                atomic.StoreUint64(&slot.sequence, pos+q.capacity)
                return data
            }
        } else if seq < pos+1 {
            return nil // Queue empty
        }
    }
}
```

### Memory Pool Management

#### Object Pooling

```go
type EventPool struct {
    pools []*sync.Pool
}

func NewEventPool() *EventPool {
    ep := &EventPool{
        pools: make([]*sync.Pool, 32), // Different size classes
    }
    
    for i := range ep.pools {
        size := 1 << (i + 4) // 16, 32, 64, ... bytes
        ep.pools[i] = &sync.Pool{
            New: func() interface{} {
                return &Event{
                    buffer: make([]byte, size),
                }
            },
        }
    }
    
    return ep
}

func (ep *EventPool) Get(size int) *Event {
    idx := sizeClassIndex(size)
    if idx >= len(ep.pools) {
        return &Event{buffer: make([]byte, size)}
    }
    
    event := ep.pools[idx].Get().(*Event)
    event.Reset()
    return event
}

func (ep *EventPool) Put(event *Event) {
    size := cap(event.buffer)
    idx := sizeClassIndex(size)
    
    if idx < len(ep.pools) {
        ep.pools[idx].Put(event)
    }
}

func sizeClassIndex(size int) int {
    if size <= 16 {
        return 0
    }
    return bits.Len(uint(size-1)) - 4
}
```

#### Arena Allocator

```go
type Arena struct {
    blocks   [][]byte
    current  []byte
    offset   int
    mu       sync.Mutex
}

func NewArena(blockSize int) *Arena {
    return &Arena{
        blocks:  make([][]byte, 0, 16),
        current: make([]byte, blockSize),
    }
}

func (a *Arena) Alloc(size int) []byte {
    a.mu.Lock()
    defer a.mu.Unlock()
    
    // Align to 8 bytes
    size = (size + 7) &^ 7
    
    if a.offset+size > len(a.current) {
        // Allocate new block
        blockSize := len(a.current)
        if size > blockSize {
            blockSize = size
        }
        
        a.blocks = append(a.blocks, a.current)
        a.current = make([]byte, blockSize)
        a.offset = 0
    }
    
    buf := a.current[a.offset : a.offset+size]
    a.offset += size
    return buf
}

func (a *Arena) Reset() {
    a.mu.Lock()
    defer a.mu.Unlock()
    
    // Keep first block, release others
    if len(a.blocks) > 0 {
        a.current = a.blocks[0]
        a.blocks = a.blocks[:0]
    }
    a.offset = 0
}
```

### CPU Optimizations

#### SIMD Pattern Matching

```go
//go:build amd64

package correlation

import (
    "golang.org/x/sys/cpu"
    "unsafe"
)

// Uses AVX2 for parallel string matching
func findPatternSIMD(data []byte, pattern []byte) []int {
    if !cpu.X86.HasAVX2 {
        return findPatternScalar(data, pattern)
    }
    
    if len(pattern) == 0 || len(data) < len(pattern) {
        return nil
    }
    
    var matches []int
    
    // Use SIMD for first byte search
    first := pattern[0]
    i := 0
    
    for i+32 <= len(data) {
        mask := simdFindByte(data[i:i+32], first)
        
        for mask != 0 {
            idx := bits.TrailingZeros32(uint32(mask))
            
            // Verify full pattern match
            if i+idx+len(pattern) <= len(data) &&
               bytes.Equal(data[i+idx:i+idx+len(pattern)], pattern) {
                matches = append(matches, i+idx)
            }
            
            mask &= mask - 1 // Clear lowest bit
        }
        
        i += 32
    }
    
    // Handle remaining bytes
    for ; i < len(data)-len(pattern)+1; i++ {
        if bytes.Equal(data[i:i+len(pattern)], pattern) {
            matches = append(matches, i)
        }
    }
    
    return matches
}

//go:noescape
func simdFindByte(data []byte, b byte) uint32
```

Assembly implementation:
```asm
// func simdFindByte(data []byte, b byte) uint32
TEXT ·simdFindByte(SB), NOSPLIT, $0-33
    MOVQ data_base+0(FP), SI
    MOVB b+24(FP), AL
    VPBROADCASTB AL, Y0
    VMOVDQU (SI), Y1
    VPCMPEQB Y0, Y1, Y2
    VPMOVMSKB Y2, AX
    MOVL AX, ret+32(FP)
    VZEROUPPER
    RET
```

#### CPU Cache Optimization

```go
type CacheAlignedEvent struct {
    _pad0    [64]byte // Cache line padding
    id       uint64
    timestamp int64
    source   uint8
    severity uint8
    _pad1    [46]byte // Fill cache line
    data     [64]byte // Separate cache line for data
}

// Prefetch hints for timeline traversal
func (t *Timeline) PrefetchTraversal(start, count int) {
    events := t.events[start:]
    
    // Prefetch next cache lines
    for i := 0; i < count && i < len(events); i++ {
        addr := unsafe.Pointer(&events[i])
        prefetchT0(addr) // Temporal, all cache levels
        
        // Prefetch index entries
        if i+4 < len(events) {
            prefetchT2(unsafe.Pointer(&events[i+4])) // Non-temporal
        }
    }
}

//go:noescape
func prefetchT0(addr unsafe.Pointer)

//go:noescape  
func prefetchT2(addr unsafe.Pointer)
```

### Batch Processing

#### Vectorized Operations

```go
type BatchProcessor struct {
    batchSize int
    processor func([]Event) error
    timeout   time.Duration
}

func (bp *BatchProcessor) ProcessStream(input <-chan Event) {
    batch := make([]Event, 0, bp.batchSize)
    timer := time.NewTimer(bp.timeout)
    timer.Stop()
    
    for {
        select {
        case event, ok := <-input:
            if !ok {
                if len(batch) > 0 {
                    bp.processor(batch)
                }
                return
            }
            
            batch = append(batch, event)
            
            if len(batch) == 1 {
                timer.Reset(bp.timeout)
            }
            
            if len(batch) >= bp.batchSize {
                timer.Stop()
                bp.processor(batch)
                batch = batch[:0]
            }
            
        case <-timer.C:
            if len(batch) > 0 {
                bp.processor(batch)
                batch = batch[:0]
            }
        }
    }
}

// Vectorized severity calculation
func calculateSeveritiesSIMD(events []Event) []uint8 {
    severities := make([]uint8, len(events))
    
    // Process 16 events at a time using SIMD
    for i := 0; i+16 <= len(events); i += 16 {
        calculateSeverityVector(
            events[i:i+16],
            severities[i:i+16],
        )
    }
    
    // Handle remaining events
    for i := len(events) &^ 15; i < len(events); i++ {
        severities[i] = calculateSeverity(events[i])
    }
    
    return severities
}
```

### I/O Optimizations

#### Zero-Copy Event Transfer

```go
type ZeroCopyBuffer struct {
    data     []byte
    refCount int32
    pool     *BufferPool
}

func (b *ZeroCopyBuffer) Slice(start, end int) *ZeroCopyBuffer {
    atomic.AddInt32(&b.refCount, 1)
    
    return &ZeroCopyBuffer{
        data:     b.data[start:end:end],
        refCount: 0, // This is a view
        pool:     b.pool,
    }
}

func (b *ZeroCopyBuffer) Release() {
    if b.refCount == 0 {
        // This is a view, release parent
        return
    }
    
    if atomic.AddInt32(&b.refCount, -1) == 0 {
        b.pool.Put(b)
    }
}

// Direct I/O for large batches
func (w *WALWriter) WriteDirectIO(events []Event) error {
    // Align buffer for O_DIRECT
    alignedSize := (len(events)*EventSize + 4095) &^ 4095
    buf := memalign(4096, alignedSize)
    defer free(buf)
    
    // Serialize events to aligned buffer
    offset := 0
    for _, event := range events {
        event.SerializeTo(buf[offset:])
        offset += EventSize
    }
    
    // Write with O_DIRECT to bypass page cache
    n, err := w.file.WriteAt(buf[:offset], w.offset)
    if err != nil {
        return err
    }
    
    w.offset += int64(n)
    return nil
}
```

#### Memory-Mapped Timeline

```go
type MMapTimeline struct {
    file     *os.File
    data     []byte
    header   *TimelineHeader
    events   []Event
    writable bool
}

func OpenMMapTimeline(path string, writable bool) (*MMapTimeline, error) {
    flag := os.O_RDONLY
    prot := syscall.PROT_READ
    
    if writable {
        flag = os.O_RDWR
        prot |= syscall.PROT_WRITE
    }
    
    file, err := os.OpenFile(path, flag, 0644)
    if err != nil {
        return nil, err
    }
    
    info, err := file.Stat()
    if err != nil {
        return nil, err
    }
    
    // Memory map the file
    data, err := syscall.Mmap(
        int(file.Fd()),
        0,
        int(info.Size()),
        prot,
        syscall.MAP_SHARED,
    )
    if err != nil {
        return nil, err
    }
    
    // Cast to timeline structure
    header := (*TimelineHeader)(unsafe.Pointer(&data[0]))
    events := (*[1<<30]Event)(unsafe.Pointer(&data[TimelineHeaderSize]))
    
    return &MMapTimeline{
        file:     file,
        data:     data,
        header:   header,
        events:   events[:header.EventCount],
        writable: writable,
    }, nil
}

func (mt *MMapTimeline) Sync() error {
    return syscall.Msync(mt.data, syscall.MS_SYNC)
}

func (mt *MMapTimeline) Close() error {
    if err := syscall.Munmap(mt.data); err != nil {
        return err
    }
    return mt.file.Close()
}
```

## Concurrency Optimizations

### Work Stealing Queue

```go
type WorkStealingQueue struct {
    top    int64
    bottom int64
    array  unsafe.Pointer // *circularArray
    mask   int64
}

type circularArray struct {
    items []*Task
}

func (q *WorkStealingQueue) Push(task *Task) {
    bottom := atomic.LoadInt64(&q.bottom)
    top := atomic.LoadInt64(&q.top)
    
    arr := (*circularArray)(atomic.LoadPointer(&q.array))
    
    if bottom-top > int64(len(arr.items)-1) {
        // Grow array
        newArr := &circularArray{
            items: make([]*Task, len(arr.items)*2),
        }
        
        for i := top; i < bottom; i++ {
            newArr.items[i&int64(len(newArr.items)-1)] = 
                arr.items[i&int64(len(arr.items)-1)]
        }
        
        atomic.StorePointer(&q.array, unsafe.Pointer(newArr))
        arr = newArr
    }
    
    arr.items[bottom&int64(len(arr.items)-1)] = task
    atomic.AddInt64(&q.bottom, 1)
}

func (q *WorkStealingQueue) Pop() *Task {
    bottom := atomic.LoadInt64(&q.bottom) - 1
    atomic.StoreInt64(&q.bottom, bottom)
    
    top := atomic.LoadInt64(&q.top)
    
    if top <= bottom {
        task := q.getTask(bottom)
        
        if top == bottom {
            if !atomic.CompareAndSwapInt64(&q.top, top, top+1) {
                atomic.StoreInt64(&q.bottom, bottom+1)
                return nil
            }
            atomic.StoreInt64(&q.bottom, bottom+1)
        }
        return task
    } else {
        atomic.StoreInt64(&q.bottom, bottom+1)
        return nil
    }
}

func (q *WorkStealingQueue) Steal() *Task {
    top := atomic.LoadInt64(&q.top)
    bottom := atomic.LoadInt64(&q.bottom)
    
    if top < bottom {
        task := q.getTask(top)
        
        if atomic.CompareAndSwapInt64(&q.top, top, top+1) {
            return task
        }
    }
    
    return nil
}
```

### Sharded Processing

```go
type ShardedProcessor struct {
    shards    []*ProcessorShard
    numShards int
}

type ProcessorShard struct {
    id       int
    queue    chan Event
    timeline *Timeline
    mu       sync.RWMutex
}

func NewShardedProcessor(numShards int) *ShardedProcessor {
    sp := &ShardedProcessor{
        shards:    make([]*ProcessorShard, numShards),
        numShards: numShards,
    }
    
    for i := 0; i < numShards; i++ {
        sp.shards[i] = &ProcessorShard{
            id:       i,
            queue:    make(chan Event, 10000),
            timeline: NewTimeline(),
        }
        
        go sp.shards[i].process()
    }
    
    return sp
}

func (sp *ShardedProcessor) Submit(event Event) {
    // Hash to shard
    shard := int(event.Entity.Hash() % uint64(sp.numShards))
    
    select {
    case sp.shards[shard].queue <- event:
    default:
        // Queue full, use random shard
        shard = rand.Intn(sp.numShards)
        sp.shards[shard].queue <- event
    }
}

func (ps *ProcessorShard) process() {
    batch := make([]Event, 0, 100)
    ticker := time.NewTicker(10 * time.Millisecond)
    
    for {
        select {
        case event := <-ps.queue:
            batch = append(batch, event)
            
            if len(batch) >= 100 {
                ps.processBatch(batch)
                batch = batch[:0]
            }
            
        case <-ticker.C:
            if len(batch) > 0 {
                ps.processBatch(batch)
                batch = batch[:0]
            }
        }
    }
}
```

## Memory Optimizations

### Compression

```go
type CompressedEventStore struct {
    compressor Compressor
    chunks     []*CompressedChunk
    index      *ChunkIndex
}

type Compressor interface {
    Compress([]byte) []byte
    Decompress([]byte) []byte
}

type SnappyCompressor struct{}

func (sc *SnappyCompressor) Compress(data []byte) []byte {
    return snappy.Encode(nil, data)
}

func (sc *SnappyCompressor) Decompress(data []byte) []byte {
    decoded, _ := snappy.Decode(nil, data)
    return decoded
}

// Dictionary compression for repeated strings
type DictionaryCompressor struct {
    dict     map[string]uint32
    reverse  []string
    nextID   uint32
}

func (dc *DictionaryCompressor) Intern(s string) uint32 {
    if id, ok := dc.dict[s]; ok {
        return id
    }
    
    id := dc.nextID
    dc.nextID++
    dc.dict[s] = id
    dc.reverse = append(dc.reverse, s)
    
    return id
}

func (dc *DictionaryCompressor) Lookup(id uint32) string {
    if int(id) < len(dc.reverse) {
        return dc.reverse[id]
    }
    return ""
}
```

### Garbage Collection Tuning

```go
func init() {
    // Tune GC for low latency
    debug.SetGCPercent(50) // More frequent, smaller GCs
    
    // Use GOGC=50 GOMEMLIMIT=400MiB for production
}

// Manual memory management for hot paths
type ManualMemoryManager struct {
    heap     []byte
    offset   int
    freeList *FreeList
}

func (mm *ManualMemoryManager) Alloc(size int) unsafe.Pointer {
    // Try free list first
    if ptr := mm.freeList.Get(size); ptr != nil {
        return ptr
    }
    
    // Allocate from heap
    if mm.offset+size > len(mm.heap) {
        return nil // Out of memory
    }
    
    ptr := unsafe.Pointer(&mm.heap[mm.offset])
    mm.offset += size
    
    return ptr
}

func (mm *ManualMemoryManager) Free(ptr unsafe.Pointer, size int) {
    mm.freeList.Put(ptr, size)
}
```

## Monitoring and Profiling

### Performance Metrics

```go
type PerformanceMonitor struct {
    eventRate      *rate.Limiter
    latencyHist    *Histogram
    gcStats        debug.GCStats
    lastGC         time.Time
}

func (pm *PerformanceMonitor) RecordEvent(latency time.Duration) {
    pm.eventRate.Allow()
    pm.latencyHist.Record(latency.Microseconds())
    
    // Check GC impact
    debug.ReadGCStats(&pm.gcStats)
    if pm.gcStats.LastGC.After(pm.lastGC) {
        pm.lastGC = pm.gcStats.LastGC
        
        if pm.gcStats.PauseTotal > 10*time.Millisecond {
            log.Warnf("GC pause exceeded 10ms: %v", pm.gcStats.Pause[0])
        }
    }
}

func (pm *PerformanceMonitor) Report() PerformanceReport {
    return PerformanceReport{
        EventRate:    pm.eventRate.Limit(),
        LatencyP50:   time.Duration(pm.latencyHist.Percentile(0.5)) * time.Microsecond,
        LatencyP99:   time.Duration(pm.latencyHist.Percentile(0.99)) * time.Microsecond,
        GCPauseP99:   pm.gcStats.Pause[0],
        HeapAlloc:    pm.gcStats.HeapAlloc,
        NumGC:        pm.gcStats.NumGC,
    }
}
```

### CPU Profiling Integration

```go
func EnableProfiling(addr string) {
    go func() {
        runtime.SetBlockProfileRate(1)
        runtime.SetMutexProfileFraction(1)
        
        http.HandleFunc("/debug/pprof/", pprof.Index)
        http.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
        http.HandleFunc("/debug/pprof/profile", pprof.Profile)
        http.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
        http.HandleFunc("/debug/pprof/trace", pprof.Trace)
        
        log.Fatal(http.ListenAndServe(addr, nil))
    }()
}

// Custom profiling for hot paths
var cpuProfile = NewCustomProfiler()

func ProcessEventOptimized(event Event) {
    cpuProfile.Start("ProcessEvent")
    defer cpuProfile.Stop("ProcessEvent")
    
    cpuProfile.Start("Normalize")
    normalized := normalizeEvent(event)
    cpuProfile.Stop("Normalize")
    
    cpuProfile.Start("Correlate")
    correlateEvent(normalized)
    cpuProfile.Stop("Correlate")
}
```

## Benchmarking Framework

```go
func BenchmarkCorrelationEngine(b *testing.B) {
    engine := NewCorrelationEngine()
    events := generateTestEvents(1000000)
    
    b.ResetTimer()
    b.ReportAllocs()
    
    b.RunParallel(func(pb *testing.PB) {
        i := 0
        for pb.Next() {
            engine.Process(events[i%len(events)])
            i++
        }
    })
    
    b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/sec")
    b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N), "ns/event")
}

// Latency benchmark
func BenchmarkEventLatency(b *testing.B) {
    engine := NewCorrelationEngine()
    
    latencies := make([]int64, b.N)
    
    b.ResetTimer()
    
    for i := 0; i < b.N; i++ {
        event := generateEvent()
        start := time.Now()
        
        engine.Process(event)
        
        latencies[i] = time.Since(start).Nanoseconds()
    }
    
    // Calculate percentiles
    sort.Slice(latencies, func(i, j int) bool {
        return latencies[i] < latencies[j]
    })
    
    p50 := latencies[b.N/2]
    p99 := latencies[b.N*99/100]
    p999 := latencies[b.N*999/1000]
    
    b.ReportMetric(float64(p50), "p50-ns")
    b.ReportMetric(float64(p99), "p99-ns")
    b.ReportMetric(float64(p999), "p99.9-ns")
}
```

## Production Deployment Optimizations

### NUMA Awareness

```go
func SetNUMAAffinity(node int) {
    var cpuset unix.CPUSet
    
    // Get CPUs for NUMA node
    cpus, _ := GetNUMACPUs(node)
    for _, cpu := range cpus {
        cpuset.Set(cpu)
    }
    
    // Set CPU affinity
    tid := unix.Gettid()
    unix.SchedSetaffinity(tid, &cpuset)
    
    // Set memory policy
    unix.Mbind(
        nil, 0,
        unix.MPOL_BIND,
        1<<uint(node),
        unix.MPOL_MF_MOVE,
    )
}
```

### Kernel Tuning

```bash
# Increase ring buffer sizes
echo 4096 > /sys/kernel/debug/tracing/buffer_size_kb

# Network optimizations
echo 1 > /proc/sys/net/core/busy_poll
echo 50 > /proc/sys/net/core/busy_read

# Memory optimizations
echo madvise > /sys/kernel/mm/transparent_hugepage/enabled
echo 0 > /proc/sys/vm/swappiness

# CPU optimizations
echo performance > /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor
```

### Container Optimizations

```dockerfile
FROM golang:1.21-alpine AS builder

# Build with optimizations
RUN go build -ldflags="-s -w" \
    -gcflags="-B -l=4" \
    -o tapio-engine

FROM alpine:3.18

# Install performance tools
RUN apk add --no-cache \
    numactl \
    perf \
    sysstat

# Copy binary
COPY --from=builder /tapio-engine /usr/local/bin/

# Set runtime optimizations
ENV GOGC=50
ENV GOMEMLIMIT=400MiB
ENV GOMAXPROCS=8

# Run with optimizations
CMD ["numactl", "--cpunodebind=0", "--membind=0", "/usr/local/bin/tapio-engine"]
```