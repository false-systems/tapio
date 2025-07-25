# Tapio Event Persistence Design

## Overview

This document outlines the design for adding persistence capabilities to Tapio while maintaining its high-performance characteristics.

## Design Principles

1. **No Data Loss**: Events must be persisted before acknowledgment
2. **High Performance**: Maintain 100k+ events/sec throughput
3. **Flexibility**: Support multiple storage backends
4. **OTEL Native**: Leverage OpenTelemetry where possible
5. **Cost Effective**: Tiered storage for different data ages

## Architecture

### Layer 1: Write-Ahead Logging (WAL)

**Purpose**: Immediate durability without blocking collectors

```go
// pkg/persistence/wal/wal.go
type WAL interface {
    // Append writes events to WAL, returns position
    Append(events []*domain.UnifiedEvent) (int64, error)
    
    // Read reads events from position
    Read(from, to int64) ([]*domain.UnifiedEvent, error)
    
    // Checkpoint marks events as persisted to storage
    Checkpoint(position int64) error
    
    // Replay replays events from last checkpoint
    Replay() (<-chan *domain.UnifiedEvent, error)
}
```

**Implementation Options**:
1. **Custom Binary Format**: Fast, compact, Tapio-specific
2. **Apache BookKeeper**: Distributed, proven in production
3. **Embedded RocksDB**: Low latency, good for single-node

### Layer 2: Tiered Storage System

#### Hot Storage (Last 1 hour)
- **Technology**: In-Memory + WAL backup
- **Use Case**: Real-time queries, correlation
- **Implementation**: Enhanced current MemoryEventStorage

#### Warm Storage (1 hour - 30 days)  
- **Technology**: TimescaleDB (PostgreSQL extension)
- **Why TimescaleDB**:
  - Native time-series optimization
  - Automatic partitioning
  - Compression (95%+ reduction)
  - SQL compatibility
  - Continuous aggregates

```sql
-- Schema design for TimescaleDB
CREATE TABLE unified_events (
    time        TIMESTAMPTZ NOT NULL,
    id          UUID NOT NULL,
    type        TEXT NOT NULL,
    source      TEXT NOT NULL,
    namespace   TEXT,
    cluster     TEXT,
    severity    INT,
    
    -- JSONB for flexible schema evolution
    metadata    JSONB,
    context     JSONB,
    data        JSONB,
    
    -- Optimization columns
    hour_bucket TIMESTAMPTZ GENERATED ALWAYS AS (time_bucket('1 hour', time)) STORED
);

-- Convert to hypertable with 1-day chunks
SELECT create_hypertable('unified_events', 'time', chunk_time_interval => INTERVAL '1 day');

-- Indexes for common queries
CREATE INDEX idx_events_type_time ON unified_events (type, time DESC);
CREATE INDEX idx_events_source_time ON unified_events (source, time DESC);
CREATE INDEX idx_events_metadata ON unified_events USING GIN (metadata);
```

#### Cold Storage (30+ days)
- **Technology**: S3-compatible object storage
- **Format**: Parquet files (columnar, compressed)
- **Organization**: Daily partitions
- **Query**: Athena/Presto for analytics

### Layer 3: Storage Router

```go
// pkg/persistence/router/router.go
type StorageRouter struct {
    hot     EventStorage      // In-memory
    warm    EventStorage      // TimescaleDB
    cold    EventStorage      // S3
    wal     wal.WAL
}

func (r *StorageRouter) Store(ctx context.Context, event *domain.UnifiedEvent) error {
    // 1. Write to WAL first
    pos, err := r.wal.Append([]*domain.UnifiedEvent{event})
    if err != nil {
        return err
    }
    
    // 2. Write to hot storage (async)
    go func() {
        if err := r.hot.Store(ctx, event); err == nil {
            r.wal.Checkpoint(pos)
        }
    }()
    
    // 3. Batch write to warm storage
    // (handled by background worker)
    
    return nil
}
```

## OTEL Integration Options

### Option 1: OTEL Collector with Custom Exporter

```yaml
# otel-collector-config.yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317

processors:
  batch:
    send_batch_size: 10000
    timeout: 100ms

exporters:
  tapio:
    endpoint: "localhost:9000"
    storage:
      hot_retention: "1h"
      warm_retention: "30d"
      cold_enabled: true

service:
  pipelines:
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [tapio]
```

### Option 2: Direct OTEL SDK Usage

```go
// Use OTEL's log SDK (when stable) with custom LogRecordProcessor
type TapioPersistentProcessor struct {
    router *StorageRouter
}

func (p *TapioPersistentProcessor) OnEmit(ctx context.Context, record log.Record) error {
    event := convertOTELToUnified(record)
    return p.router.Store(ctx, event)
}
```

## Implementation Phases

### Phase 1: WAL + TimescaleDB (2-3 weeks)
1. Implement WAL interface with RocksDB
2. Create TimescaleDB EventStorage implementation  
3. Add background worker for WAL → TimescaleDB sync
4. Update gRPC server to use persistent storage

### Phase 2: Storage Router + Tiering (2 weeks)
1. Implement StorageRouter with policy engine
2. Add data migration between tiers
3. Implement query federation across tiers

### Phase 3: S3 Cold Storage (1 week)
1. Implement Parquet writer for daily exports
2. Add S3 EventStorage for cold queries
3. Create lifecycle policies

### Phase 4: OTEL Integration (1 week)  
1. Create custom OTEL exporter
2. Add OTEL collector configuration
3. Support OTLP log ingestion

## Performance Considerations

### Write Path Optimization
```
Collector → RingBuffer → WAL (async) → Hot Storage
                      ↓
                 Batch Worker
                      ↓
                 TimescaleDB (batch inserts)
```

- WAL writes are sequential (fast)
- Batch inserts to TimescaleDB (10k events/batch)
- Async checkpoint updates
- No blocking on collector path

### Query Path Optimization
1. Recent data (< 1 hour): Memory only
2. Medium range: TimescaleDB with time-based indexes
3. Long range: Parallel S3 queries with predicate pushdown

## Configuration Example

```yaml
# config/persistence.yaml
persistence:
  wal:
    type: "rocksdb"
    path: "/var/lib/tapio/wal"
    sync: true
    
  hot:
    retention: "1h"
    max_memory: "4GB"
    
  warm:
    type: "timescaledb"
    connection: "postgres://tapio@localhost/tapio"
    retention: "30d"
    chunk_interval: "1d"
    compression:
      after: "7d"
      
  cold:
    type: "s3"
    bucket: "tapio-events"
    region: "us-east-1"
    format: "parquet"
    compression: "snappy"
```

## Monitoring & Operations

### Metrics to Track
- WAL lag (events pending persistence)
- Storage tier sizes
- Query latencies by tier
- Compression ratios
- Migration success rates

### Health Checks
```go
func (r *StorageRouter) Health() HealthStatus {
    return HealthStatus{
        WALLag:        r.wal.GetLag(),
        HotEvents:     r.hot.Count(),
        WarmEvents:    r.warm.Count(),
        ColdEvents:    r.cold.Count(),
        LastMigration: r.lastMigration,
    }
}
```

## Decision Matrix

| Aspect | TimescaleDB | ClickHouse | Elasticsearch | Kafka + Druid |
|--------|-------------|------------|---------------|---------------|
| Write Performance | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ |
| Query Flexibility | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ |
| Operational Complexity | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐ | ⭐ |
| Compression | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐ | ⭐⭐⭐⭐ |
| OTEL Integration | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐ |
| Cost | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐ | ⭐⭐ |

**Recommendation**: TimescaleDB for warm storage due to:
- SQL compatibility (easier integration)
- Excellent compression
- Built-in time-series features
- Lower operational overhead
- Good OTEL ecosystem support

## Next Steps

1. Prototype WAL implementation with RocksDB
2. Create TimescaleDB schema and test with 1M events/sec
3. Benchmark query performance across time ranges
4. Design migration policies between storage tiers
5. Plan OTEL collector integration

## Open Questions

1. Should we support multiple WAL implementations?
2. How to handle schema evolution in cold storage?
3. Should queries federate across all tiers by default?
4. How to implement multi-tenancy in storage layer?
5. Backup and disaster recovery strategy?