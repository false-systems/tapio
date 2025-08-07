# Neo4j Storage Integration

This package provides a Neo4j-based implementation of the `correlation.Storage` interface, enabling persistent storage of correlation results in a graph database.

## Features

- Persistent storage of correlation results with full graph relationships
- Efficient retrieval by trace ID or recent correlations
- Automatic cleanup of old correlations
- Rich relationship modeling between correlations, events, root causes, and impacts

## Architecture

This integration follows Tapio's 5-level architecture:
- **Level 2**: Uses `pkg/intelligence/graph` (Neo4j client)
- **Level 2**: Implements `pkg/intelligence/correlation.Storage` interface
- **Level 3**: Storage integration implementation

## Usage

### Basic Setup

```go
import "github.com/yairfalse/tapio/pkg/integrations/storage/neo4j"

// Configure Neo4j connection
config := graph.Config{
    URI:      "bolt://localhost:7687",
    Username: "neo4j",
    Password: "password",
    Database: "neo4j",
}

// Create storage
storage, err := neo4j.NewStorage(config, logger)
if err != nil {
    log.Fatal(err)
}
defer storage.Close(context.Background())

// Use with correlation engine
engine, err := correlation.NewEngine(logger, engineConfig, k8sClient, storage)
```

### Modifying Correlation Service

To use Neo4j storage instead of memory storage in the correlation service:

1. Add Neo4j configuration environment variables:
```yaml
env:
  - name: NEO4J_URI
    value: "bolt://neo4j.tapio-system:7687"
  - name: NEO4J_USERNAME
    valueFrom:
      secretKeyRef:
        name: neo4j-credentials
        key: username
  - name: NEO4J_PASSWORD
    valueFrom:
      secretKeyRef:
        name: neo4j-credentials
        key: password
```

2. Update `cmd/correlation-service/main.go`:
```go
// Replace memory storage with Neo4j storage
neo4jConfig := graph.Config{
    URI:      os.Getenv("NEO4J_URI"),
    Username: os.Getenv("NEO4J_USERNAME"),
    Password: os.Getenv("NEO4J_PASSWORD"),
    Database: os.Getenv("NEO4J_DATABASE"),
}

neo4jStorage, err := neo4j.NewStorage(neo4jConfig, logger)
if err != nil {
    logger.Fatal("Failed to create Neo4j storage", zap.Error(err))
}
defer neo4jStorage.Close(context.Background())

// Use neo4jStorage instead of memStorage
engine, err := correlation.NewEngine(logger, engineConfig, clientset, neo4jStorage)
```

## Graph Schema

The storage creates the following graph structure:

### Nodes
- **Correlation**: Main correlation result
  - Properties: id, type, confidence, traceId, summary, details, startTime, endTime, createdAt, evidence[]
- **Event**: Events that are part of the correlation
  - Properties: id
- **RootCause**: Root cause of the correlation
  - Properties: eventId, confidence, description, evidence[]
- **Impact**: Impact assessment
  - Properties: severity, scope, affectedServices[], estimatedRecovery

### Relationships
- `(Event)-[:PART_OF]->(Correlation)`
- `(RootCause)-[:ROOT_CAUSE_OF]->(Correlation)`
- `(Impact)-[:IMPACT_OF]->(Correlation)`

### Indexes
- Correlation.id (unique constraint)
- Correlation.traceId
- Correlation.type
- Correlation.startTime
- Correlation.confidence
- RootCause.eventId
- Impact.severity

## Testing

Run integration tests with a local Neo4j instance:

```bash
# Start Neo4j
docker run -d \
  --name neo4j-test \
  -p 7474:7474 -p 7687:7687 \
  -e NEO4J_AUTH=neo4j/password \
  neo4j:5-community

# Run tests
go test ./pkg/integrations/storage/neo4j/...

# Run with verbose output
go test -v ./pkg/integrations/storage/neo4j/...
```

## Performance Considerations

1. **Indexes**: The storage automatically creates necessary indexes during initialization
2. **Batch Operations**: For high-volume scenarios, consider batching Store operations
3. **Cleanup**: Run cleanup periodically to remove old correlations
4. **Connection Pooling**: The underlying Neo4j driver handles connection pooling

## Example Queries

You can query the stored data directly in Neo4j:

```cypher
// Find all correlations for a trace
MATCH (c:Correlation {traceId: "trace-123"})
OPTIONAL MATCH (e:Event)-[:PART_OF]->(c)
OPTIONAL MATCH (rc:RootCause)-[:ROOT_CAUSE_OF]->(c)
RETURN c, collect(e) as events, rc

// Find high-confidence correlations
MATCH (c:Correlation)
WHERE c.confidence > 0.9
RETURN c
ORDER BY c.startTime DESC
LIMIT 10

// Find correlations with specific impact
MATCH (c:Correlation)<-[:IMPACT_OF]-(i:Impact)
WHERE i.severity = "critical"
RETURN c, i
```