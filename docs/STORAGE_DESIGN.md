# Tapio Storage Design (Inspired by Parca)

## Overview

Following Parca's successful model, we'll use embedded databases to avoid operational complexity while maintaining high performance.

## Storage Architecture

### 1. Correlation Storage (FrostDB)

Use FrostDB for storing correlation results and intelligence findings:

```go
// pkg/storage/correlations/frostdb_store.go
import "github.com/polarsignals/frostdb"

type CorrelationStore struct {
    db *frostdb.DB
}

// Schema for correlations
type CorrelationRecord struct {
    Timestamp   time.Time
    ID          string
    Type        string              // anomaly, attack, misconfiguration
    Severity    int
    Confidence  float64
    EventCount  int
    TimeWindow  int64               // duration in seconds
    Resources   map[string][]string // affected resources
    Pattern     []byte              // serialized pattern
    Embedding   []float32           // for AI similarity
}
```

### 2. Metadata Storage (Badger)

Use Badger for fast KV lookups:

```go
// pkg/storage/metadata/badger_store.go
import "github.com/dgraph-io/badger/v4"

type MetadataStore struct {
    db *badger.DB
}

// Store user data, system state, resource mappings
// - users:{id} → User data
// - api_keys:{key} → User ID
// - resources:{type}:{name} → Resource metadata
// - checkpoints:{collector} → Last processed position
```

### 3. Transit Persistence (WAL)

Simple write-ahead log for reliability:

```go
// pkg/storage/wal/simple_wal.go
type SimpleWAL struct {
    file     *os.File
    encoder  *json.Encoder
    position int64
}

func (w *SimpleWAL) Append(events []*domain.UnifiedEvent) error {
    // Batch write with fsync
    for _, event := range events {
        if err := w.encoder.Encode(event); err != nil {
            return err
        }
    }
    return w.file.Sync()
}
```

## Data Flow

```
1. Events arrive → RingBuffer (in-memory)
                 ↓
2. Correlation Engine processes → Findings to FrostDB
                                ↓
3. Metadata updates → Badger
```

## Configuration

```yaml
storage:
  correlations:
    type: "frostdb"
    path: "/var/lib/tapio/correlations"
    active_memory: "512MB"
    wal_enabled: true
    compaction:
      l0_size: 100MB
      l1_size: 1GB
      
  metadata:
    type: "badger"
    path: "/var/lib/tapio/metadata"
    in_memory: false
    compression: true
    
  transit:
    wal_path: "/var/lib/tapio/wal"
    retention: "1h"  # Only keep recent data
```

## Why This Works for Tapio

1. **No External Dependencies**: Everything embedded
2. **High Performance**: Both DBs designed for write-heavy workloads
3. **AI-Ready**: Store embeddings in FrostDB for similarity search
4. **Simple Operations**: Just Go binaries, no DB servers
5. **Proven in Production**: Parca handles similar scale

## Implementation Priority

1. **Phase 1**: Badger for user auth and metadata (1 week)
2. **Phase 2**: Simple WAL for transit reliability (3 days)
3. **Phase 3**: FrostDB for correlation storage (1 week)
4. **Phase 4**: Query layer with label selectors (1 week)

## Example Usage

```go
// Initialize storage
storage := &TapioStorage{
    Correlations: frostdb.New(config.Correlations),
    Metadata:     badger.New(config.Metadata),
    WAL:          wal.New(config.Transit),
}

// Store correlation finding
finding := &CorrelationRecord{
    Timestamp:  time.Now(),
    Type:       "anomaly",
    Severity:   4,
    Confidence: 0.95,
    Resources: map[string][]string{
        "pods": {"nginx-abc", "nginx-def"},
        "nodes": {"node-1"},
    },
}
storage.Correlations.Insert(finding)

// Query similar findings (AI-powered)
similar := storage.Correlations.Query(
    frostdb.Where("embedding").Similar(currentEmbedding, 0.8),
    frostdb.TimeRange(time.Now().Add(-24*time.Hour), time.Now()),
)
```

## Storage Sizing

For 100k events/sec:
- If 1% generate correlations → 1k correlations/sec
- Each correlation ~1KB → 86GB/day
- With compression → ~10GB/day
- 30 day retention → 300GB total

This is manageable on a single server with NVMe storage.