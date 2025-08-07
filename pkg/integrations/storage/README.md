# Tapio Storage Integrations

This package provides persistent storage integrations for the Tapio observability platform.

## Neo4j Persistent Storage

The Neo4j integration provides:

- **Long-term Event Storage**: Persist events with configurable retention policies
- **Correlation Storage**: Store detected patterns and correlations
- **Automatic Retention**: Clean up old data based on retention policies
- **Data Compaction**: Aggregate high-volume events for efficient storage
- **Time-based Queries**: Efficiently query events by time ranges

### Features

1. **Persistent Event Storage**
   - Events are stored with retention metadata
   - Automatic cleanup based on retention policy
   - Efficient time-based indexing

2. **Correlation Management**
   - Store relationships between events
   - Track pattern detections over time
   - Query correlations by pattern type

3. **Storage Optimization**
   - Automatic data compaction for old events
   - Aggregation of similar patterns
   - Efficient batch operations

4. **Monitoring**
   - Storage metrics tracking
   - Retention policy execution logs
   - Compaction performance metrics

### Usage

```go
import "github.com/yairfalse/tapio/pkg/integrations/storage/neo4j"

// Configure storage
config := neo4j.Config{
    GraphConfig: graph.Config{
        URI:      "bolt://localhost:7687",
        Username: "neo4j",
        Password: "password",
        Database: "neo4j",
    },
    RetentionDays:    30,               // Keep data for 30 days
    CompactionPeriod: 24 * time.Hour,   // Compact daily
    BatchSize:        1000,             // Batch size for operations
}

// Create client
client, err := neo4j.NewPersistentClient(config, logger)
if err != nil {
    log.Fatal(err)
}
defer client.Close(context.Background())

// Start retention scheduler
scheduler := neo4j.NewRetentionScheduler(client, 24*time.Hour, logger)
scheduler.Start(context.Background())
defer scheduler.Stop()
```

### Kubernetes Deployment

The storage integration works with the existing Neo4j StatefulSet deployment, which already includes:

- PersistentVolumeClaim for data storage
- Configurable memory settings
- Service exposure for bolt protocol

### Configuration

Environment variables for the correlation service:

```yaml
env:
  - name: NEO4J_URI
    value: "bolt://neo4j.tapio-system:7687"
  - name: NEO4J_DATABASE
    value: "neo4j"
  - name: RETENTION_DAYS
    value: "30"
  - name: COMPACTION_PERIOD
    value: "24h"
```

### Testing

Run integration tests with a local Neo4j instance:

```bash
# Start Neo4j locally
docker run -d \
  --name neo4j-test \
  -p 7474:7474 -p 7687:7687 \
  -e NEO4J_AUTH=neo4j/password \
  neo4j:5-community

# Run tests
go test ./pkg/integrations/storage/neo4j/...
```

### Architecture Compliance

This integration follows Tapio's 5-level architecture:

- **Level 0 (Domain)**: Uses domain event types
- **Level 2 (Intelligence)**: Imports graph client from intelligence layer
- **Level 3 (Integrations)**: Storage integration implementation
- No circular dependencies
- Clean interfaces and proper error handling