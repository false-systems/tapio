# Correlation System Design

## Current State Analysis

### What We Have
1. **Correlation Engine** (`engine.go`)
   - Orchestrates multiple correlators
   - Processes events through correlators
   - Stores results in storage backend
   - Has proper interfaces and clean architecture

2. **Correlators** (all implementing `Correlator` interface)
   - K8s Correlator: Kubernetes resource relationships
   - Temporal Correlator: Time-based patterns
   - Sequence Correlator: Event sequence detection
   - Performance Correlator: Performance degradation patterns
   - ServiceMap Correlator: Service dependency mapping

3. **Storage** (`storage.go`)
   - Interface for correlation result persistence
   - Neo4j implementation in integrations layer

4. **Intelligence Service** (`../service.go`)
   - Processes events and stores in Neo4j
   - Has query capabilities (WhyDidPodFail, etc.)
   - Pattern detection

5. **API Server** (`/cmd/api/main.go`)
   - Direct Neo4j queries (bypasses intelligence layer)
   - Basic endpoints: /why, /impact, /health

### Architecture Issues
1. API server bypasses the intelligence layer
2. No connection between correlation engine results and API queries
3. Missing caching layer for expensive correlations
4. No real-time correlation result streaming
5. No metrics/monitoring for correlation performance

## Proposed Design

### Phase 1: Connect API to Intelligence Layer ✅
**Goal**: Make API use intelligence service instead of direct Neo4j queries

1. **Add HTTP handlers to intelligence service**
   - Create `pkg/interfaces/api/correlation/` package
   - Implement handlers that use intelligence service
   - Keep existing API contract for compatibility

2. **Refactor API server**
   - Use intelligence service client
   - Remove direct Neo4j dependency
   - Add proper error handling and logging

### Phase 2: Real-time Correlation Results
**Goal**: Stream correlation results as they're discovered

1. **Add NATS publisher to correlation engine**
   - Publish results to NATS topics
   - Topic structure: `correlation.{type}.{severity}`
   - Include correlation metadata

2. **Add WebSocket endpoint for real-time updates**
   - Subscribe to NATS topics
   - Stream to connected clients
   - Filter by resource/namespace

### Phase 3: Correlation Caching
**Goal**: Cache expensive correlation queries

1. **Add caching layer**
   - Redis for distributed cache
   - TTL based on correlation type
   - Invalidation on related events

2. **Query optimization**
   - Cache Neo4j query results
   - Pre-compute common patterns
   - Background refresh for hot paths

### Phase 4: Monitoring & Metrics
**Goal**: Visibility into correlation performance

1. **Add Prometheus metrics**
   - Correlation processing time
   - Correlator success/failure rates
   - Cache hit/miss ratios
   - Query performance

2. **Health endpoints**
   - Individual correlator health
   - Storage backend health
   - Processing queue depth

## Implementation Order

1. **Clean up existing code** ✅
   - Remove over-engineering
   - Consolidate types
   - Fix architectural violations

2. **Phase 1: API Integration** (Current Focus)
   - Create API handlers in interfaces layer
   - Update API server to use handlers
   - Add integration tests

3. **Phase 2: Real-time Streaming**
   - Add NATS publisher
   - Implement WebSocket endpoint
   - Create demo dashboard

4. **Phase 3: Caching Layer**
   - Add Redis integration
   - Implement cache logic
   - Performance testing

5. **Phase 4: Monitoring**
   - Add metrics
   - Create Grafana dashboards
   - Set up alerts

## Success Criteria

1. **Functionality**
   - All API queries go through intelligence layer
   - Real-time correlation updates work
   - Cache improves query performance by 10x
   - Full observability of correlation system

2. **Code Quality**
   - Clean, maintainable code
   - >80% test coverage
   - No architectural violations
   - Clear documentation

3. **Performance**
   - <100ms correlation processing (p95)
   - <50ms cached query response (p95)
   - Support 1000 events/second
   - <5% CPU overhead

## Next Steps

1. Review this design with team
2. Start Phase 1 implementation
3. Create integration tests
4. Update documentation