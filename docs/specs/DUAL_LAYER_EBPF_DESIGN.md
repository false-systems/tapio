# Dual-Layer eBPF Event Design

## Overview

This document outlines a clean architectural approach for preserving rich eBPF event data while maintaining the unified event pipeline architecture.

## Problem Statement

eBPF collectors generate extremely detailed kernel-level data that includes:
- Process context (PID, TID, UID, GID, comm)
- CPU core information
- Raw network packet data
- File operation details
- Memory allocation patterns
- Kernel stack traces

Converting this rich data to UnifiedEvent loses valuable debugging and security analysis information.

## Proposed Solution: Separate Raw Event Pipeline

### Architecture

```
                    ┌─────────────────────┐
                    │   eBPF Programs     │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │  Kernel Ring Buffer │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │  eBPF Collector     │
                    └──────────┬──────────┘
                               │
                ┌──────────────┴──────────────┐
                │                             │
     ┌──────────▼──────────┐      ┌──────────▼──────────┐
     │  Raw Event Ring     │      │ UnifiedEvent Convert │
     │  (Lock-free)        │      │                      │
     └──────────┬──────────┘      └──────────┬──────────┘
                │                             │
     ┌──────────▼──────────┐      ┌──────────▼──────────┐
     │ Raw eBPF Export API │      │ Intelligence Pipeline│
     │  (gRPC Streaming)   │      │  (Correlation)       │
     └─────────────────────┘      └──────────────────────┘
                │                             │
     ┌──────────▼──────────┐      ┌──────────▼──────────┐
     │ Specialized Tools   │      │  Tapio Server       │
     │ (Security/Debug)    │      │                     │
     └─────────────────────┘      └──────────────────────┘
```

### Key Components

#### 1. Raw Event Types (`pkg/collectors/ebpf/raw_event.go`)

```go
type RawEvent struct {
    // Core metadata
    Type      EventType
    Timestamp uint64    // Kernel timestamp
    CPU       uint32
    
    // Process context
    PID  uint32
    TID  uint32
    UID  uint32
    GID  uint32
    Comm [16]byte
    
    // Event-specific data
    NetworkEvent *NetworkEventData
    ProcessEvent *ProcessEventData
    FileEvent    *FileEventData
    MemoryEvent  *MemoryEventData
}
```

#### 2. Raw Event Ring Buffer (`pkg/collectors/ebpf/raw_ring_buffer.go`)

- Lock-free ring buffer specifically for raw eBPF events
- Power-of-2 sizing for efficient bit operations
- Atomic operations only
- Overwrites old events when full (like Hubble)
- Separate from UnifiedEvent pipeline

#### 3. eBPF Collector Dual Output

```go
type Collector struct {
    rawRing      *RawEventRing      // Raw events
    unifiedChan  chan UnifiedEvent  // Normalized events
}

func (c *Collector) processKernelEvent(raw *RawEvent) {
    // Path 1: Store raw event
    c.rawRing.Put(raw)
    
    // Path 2: Convert to UnifiedEvent
    unified := c.convertToUnified(raw)
    c.unifiedChan <- unified
}
```

#### 4. Raw eBPF Export Service (`pkg/interfaces/grpc/ebpf_export_service.go`)

```go
service EBPFExport {
    // Stream raw eBPF events
    rpc StreamRawEvents(StreamRequest) returns (stream RawEvent);
    
    // Query historical raw events
    rpc QueryRawEvents(QueryRequest) returns (QueryResponse);
    
    // Get ring buffer metrics
    rpc GetRingMetrics(Empty) returns (RingMetrics);
}
```

### Benefits

1. **No Information Loss**: All kernel data preserved
2. **Clean Architecture**: UnifiedEvent stays clean, no `interface{}`
3. **Performance**: Separate pipelines, no overhead on main path
4. **Specialized Tools**: Raw data available for security/debugging tools
5. **Type Safety**: Strongly typed raw events
6. **Independent Scaling**: Can scale raw export separately

### Implementation Plan

#### Phase 1: Core Infrastructure
1. Define raw event types with all eBPF fields
2. Implement lock-free raw event ring buffer
3. Add dual-output to eBPF collector

#### Phase 2: Export API
1. Create gRPC service for raw event streaming
2. Add authentication/authorization
3. Implement filtering and query capabilities

#### Phase 3: Integration
1. Update collector to populate both pipelines
2. Add metrics for both paths
3. Create example specialized consumers

#### Phase 4: Advanced Features
1. Raw event persistence (optional)
2. Time-based replay
3. Advanced filtering (BPF expressions)

### API Design

#### Raw Event Streaming
```
GET /api/v1/ebpf/stream
```
- Real-time WebSocket/gRPC stream
- Filters: event type, process, network
- Authentication required

#### Raw Event Query
```
POST /api/v1/ebpf/query
{
    "time_range": {...},
    "filters": {...},
    "limit": 1000
}
```

#### Ring Buffer Metrics
```
GET /api/v1/ebpf/metrics
```
- Ring buffer utilization
- Drop rates
- Event rates by type

### Security Considerations

1. **Access Control**: Raw events contain sensitive kernel data
2. **Rate Limiting**: Prevent DoS through event streaming
3. **Filtering**: Allow/deny specific event types per consumer
4. **Audit Logging**: Track who accesses raw kernel data

### Performance Targets

- **Raw Event Ring**: 1M+ events/second
- **Export Streaming**: 100K events/second per client
- **Memory Usage**: Fixed ring buffer size (configurable)
- **CPU Overhead**: < 5% for dual-path processing

### Future Enhancements

1. **Compression**: Compress raw events for storage
2. **Sampling**: Intelligent sampling for high-volume events
3. **ML Integration**: Feed raw events to anomaly detection
4. **Distributed Ring**: Multi-node raw event aggregation

## Conclusion

This dual-layer approach provides the best of both worlds:
- Clean, normalized UnifiedEvents for intelligence processing
- Complete raw eBPF data for specialized analysis

The architecture maintains separation of concerns while enabling advanced use cases that require full kernel visibility.