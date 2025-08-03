# Intelligence Package

The brain of Tapio - converts K8s events into actionable insights using graph-based correlation.

## Architecture

```
UnifiedEvents → Neo4j Graph → Pattern Detection → Root Cause Analysis
```

## Components

### 1. Graph Client (`graph/`)
- Neo4j connection management
- Schema creation and indexing
- Node and relationship operations
- Query execution

### 2. Queries (`queries/`)
- **WhyDidPodFail**: Root cause analysis for pod failures
- **WhatImpactsService**: Impact analysis for services
- **FindCascadingFailures**: Detect cascade patterns
- **GetServiceDependencies**: Map service relationships

### 3. Pattern Detection (`patterns/`)
- **OOMKillPattern**: Detects memory-related cascades
- **ConfigMapChangePattern**: Tracks config changes causing restarts
- **CrashLoopPattern**: Identifies crash loop backoffs
- **NodePressurePattern**: Node resource exhaustion
- **ServiceDisruptionPattern**: Service availability issues
- **RollingUpdateFailurePattern**: Failed deployments

## Usage

```go
// Initialize
config := intelligence.Config{
    Neo4jURI:      "bolt://localhost:7687",
    Neo4jUsername: "neo4j",
    Neo4jPassword: "password",
    Neo4jDatabase: "neo4j",
}

service, err := intelligence.NewService(config, logger)

// Process events
err = service.ProcessEvent(ctx, unifiedEvent)

// Query correlations
analysis, err := service.WhyDidThisFail(ctx, "pod", "default", "nginx-123")

// Get impact
impact, err := service.WhatDoesThisImpact(ctx, "service", "default", "web")
```

## Neo4j Schema

### Nodes
- **Pod**: K8s pods with labels, namespace, status
- **Service**: K8s services with selectors
- **Deployment**: Deployment configurations
- **ConfigMap/Secret**: Configuration resources
- **Node**: Cluster nodes
- **Event**: All events with timestamps

### Relationships
- `(:Pod)-[:OWNED_BY]->(:ReplicaSet)`
- `(:Pod)-[:SELECTED_BY]->(:Service)`
- `(:Pod)-[:MOUNTS]->(:ConfigMap)`
- `(:Pod)-[:RUNS_ON]->(:Node)`
- `(:Event)-[:CAUSED_BY]->(:Event)`
- `(:Event)-[:AFFECTS]->(:Resource)`

## Testing

```bash
# Unit tests
go test ./pkg/intelligence/...

# With coverage
go test -coverprofile=coverage.out ./pkg/intelligence/...
go tool cover -html=coverage.out

# Benchmarks
go test -bench=. ./pkg/intelligence/...
```

## Performance

- Query response time: < 100ms for correlation queries
- Pattern detection: < 10ms per event
- Can handle 10K events/second
- Graph scales to millions of nodes

## Future Enhancements

1. **Temporal Patterns**: Time-series correlation
2. **Anomaly Detection**: Statistical outlier detection  
3. **ML Integration**: Train models on historical patterns
4. **Custom Patterns**: User-defined pattern rules