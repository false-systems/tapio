# Tapio Semantic Correlation - Complete Technical Implementation Plan

## Overview
Transform Tapio from event collection to true K8s intelligence by implementing semantic correlation based on Kubernetes relationships, not just time.

## Architecture Components

### 1. K8sGrapher Service (NEW)
**Purpose**: Build and maintain a graph of K8s object relationships in Neo4j

#### Technical Details
```go
// cmd/k8s-grapher/main.go
type K8sGrapher struct {
    kubeClient      kubernetes.Interface
    neo4jDriver     neo4j.Driver
    instrumentation *telemetry.K8sGrapherInstrumentation
}

// Relationships to extract:
1. Service → Pod (via label selectors)
2. Pod → ConfigMap/Secret (via volumes and envFrom)
3. Pod → PVC → StorageClass
4. Deployment → ReplicaSet → Pod (ownership chain)
5. Ingress → Service → Pod (traffic flow)
6. NetworkPolicy → Pod (network rules)
```

#### OTEL Integration
```go
// pkg/integrations/telemetry/instrumentation.go (ADD)
type K8sGrapherInstrumentation struct {
    *ServiceInstrumentation
    
    // Metrics
    RelationshipsDiscovered metric.Int64Counter      // by type
    GraphUpdateDuration     metric.Float64Histogram
    K8sWatchEvents         metric.Int64Counter       // by resource
    GraphQueryDuration     metric.Float64Histogram
    ActiveRelationships    metric.Int64UpDownCounter // current count
}
```

#### Neo4j Schema
```cypher
// Nodes
(:Service {name, namespace, uid, selector})
(:Pod {name, namespace, uid, labels})
(:ConfigMap {name, namespace, uid})
(:Secret {name, namespace, uid})
(:PVC {name, namespace, uid})
(:StorageClass {name, provisioner})
(:Deployment {name, namespace, uid})
(:ReplicaSet {name, namespace, uid})

// Relationships
(:Service)-[:SELECTS {selector}]->(:Pod)
(:Pod)-[:MOUNTS {path}]->(:ConfigMap)
(:Pod)-[:USES_SECRET {type}]->(:Secret)
(:Pod)-[:CLAIMS]->(:PVC)-[:USES]->(:StorageClass)
(:Deployment)-[:OWNS]->(:ReplicaSet)-[:OWNS]->(:Pod)
```

### 2. K8s Semantic Correlators (NEW)
**Purpose**: Use graph relationships to find root causes

#### Implementations

##### DependencyCorrelator
```go
// pkg/intelligence/correlation/dependency_correlator.go
type DependencyCorrelator struct {
    neo4jDriver neo4j.Driver
    logger      *zap.Logger
}
// Handles: Service→Pod, Pod→ConfigMap, Pod→PVC relationships
```

##### ConfigImpactCorrelator
```go
// pkg/intelligence/correlation/config_impact_correlator.go
type ConfigImpactCorrelator struct {
    neo4jDriver neo4j.Driver
    logger      *zap.Logger
}
// Handles: ConfigMap/Secret changes affecting pods
```

##### OwnershipCorrelator
```go
// pkg/intelligence/correlation/ownership_correlator.go
type OwnershipCorrelator struct {
    neo4jDriver neo4j.Driver
    logger      *zap.Logger
}
// Handles: Deployment→ReplicaSet→Pod chains
```

#### Correlation Patterns:
1. ConfigImpactCorrelator: "Pod crash due to bad ConfigMap"
   MATCH (p:Pod)-[:MOUNTS]->(cm:ConfigMap)
   WHERE p.lastRestart > cm.lastModified
   
2. DependencyCorrelator: "Service has no endpoints"
   MATCH (s:Service)
   WHERE NOT EXISTS ((s)-[:SELECTS]->(:Pod {ready: true}))
   
3. CascadeCorrelator: "Cascading failure"
   MATCH path = (s1:Service)-[:DEPENDS_ON*]->(s2:Service)
   WHERE s2.healthy = false
```

### 3. Enhanced Correlation Storage
**Purpose**: Store correlations with full semantic context

#### Neo4j Storage
```cypher
// Store correlation results
CREATE (c:Correlation {
    id: $id,
    type: 'config_change_impact',
    confidence: 0.95,
    timestamp: datetime(),
    rootCause: 'ConfigMap change'
})

// Link to events
MATCH (e:Event {id: $eventId})
CREATE (e)-[:CAUSED]->(c)

// Link to resources
MATCH (cm:ConfigMap {name: $configName})
CREATE (c)-[:INVOLVES]->(cm)
```

### 4. Correlation Aggregator (NEW)
**Purpose**: Combine outputs from multiple correlators into a single, confident answer

#### Technical Details
```go
// pkg/intelligence/aggregator/aggregator.go
type CorrelationAggregator struct {
    neo4jDriver     neo4j.Driver
    logger          *zap.Logger
    confidenceCalc  ConfidenceCalculator
    conflictResolver ConflictResolver
    instrumentation *telemetry.AggregatorInstrumentation
}

// Core functionality
func (a *CorrelationAggregator) Aggregate(outputs []*CorrelatorOutput) *FinalResult {
    // 1. Check data sufficiency
    // 2. Resolve conflicts between correlators
    // 3. Build causality chain
    // 4. Calculate final confidence
    // 5. Generate unified answer
}
```

#### Aggregation Logic
```go
// Conflict Resolution
- If multiple correlators disagree → use confidence scores + specificity
- If they agree → boost confidence
- If partial agreement → combine insights

// Confidence Calculation
- Base: Average of correlator confidences
- Boost: +20% if multiple correlators agree
- Boost: +10% if matches known pattern
- Penalty: -20% if missing critical data

// Example Flow:
DependencyCorrelator: "Service has no pods" (0.8 confidence)
ConfigImpactCorrelator: "Selector changed 5m ago" (0.9 confidence)
OwnershipCorrelator: "Deployment updated selector" (0.85 confidence)

Aggregator Result: "Service has no endpoints because Deployment 
selector was changed 5m ago" (0.95 confidence)
```

#### OTEL Integration
```go
type AggregatorInstrumentation struct {
    *ServiceInstrumentation
    
    // Metrics
    CorrelationsAggregated metric.Int64Counter      // total processed
    ConflictsResolved      metric.Int64Counter      // by resolution type
    ConfidenceScores       metric.Float64Histogram  // distribution
    AggregationDuration    metric.Float64Histogram
    PatternMatches         metric.Int64Counter      // known patterns found
}
```

### 5. API Service (NEW)
**Purpose**: Expose semantic correlations via REST API

#### Endpoints
```yaml
GET /api/v1/why?pod={podName}&namespace={namespace}
Response:
{
  "answer": "Pod frontend-abc is crashing because ConfigMap 'app-config' was modified 2 hours ago with invalid JSON",
  "confidence": 0.95,
  "evidence": [
    "ConfigMap modified at 14:32",
    "Pod started crashing at 14:35",
    "Error in logs: 'invalid JSON at line 5'"
  ],
  "traceId": "abc123",  # Link to OTEL trace
  "recommendation": "Revert ConfigMap to previous version"
}

POST /api/v1/feedback
Body: {
  "correlationId": "corr-123",
  "accurate": true,
  "helpful": true,
  "notes": "Exactly right!"
}
```

#### OTEL Integration
```go
type APIInstrumentation struct {
    *ServiceInstrumentation
    
    // Metrics
    QueriesProcessed   metric.Int64Counter      // by type
    QueryDuration      metric.Float64Histogram
    CorrelationsFound  metric.Int64Histogram    // count per query
    FeedbackReceived   metric.Int64Counter      // by rating
}
```

## Implementation Phases

### Phase 1: Foundation (Week 1)
1. Create RelationshipExtractorInstrumentation
2. Build basic Relationship Extractor
   - Start with Service→Pod mappings
   - Add ownership chains
3. Deploy to test environment
4. Verify relationships in Neo4j

### Phase 2: Semantic Correlation (Week 2)
1. Implement K8s Semantic Correlator
2. Add basic correlation patterns:
   - Config change impacts
   - Service selector issues
   - Ownership chains
3. Store correlations in Neo4j
4. Test with real scenarios

### Phase 3: API & Integration (Week 3)
1. Build API service with OTEL
2. Implement /why endpoint
3. Add feedback mechanism
4. Create simple CLI for testing
5. Full end-to-end testing

### Phase 4: Polish & Deploy (Week 4)
1. Add more correlation patterns
2. Tune confidence scores
3. Performance optimization
4. Documentation
5. Demo preparation

## Testing Scenarios

### Scenario 1: ConfigMap Change Cascade
```yaml
1. Deploy app with ConfigMap
2. Modify ConfigMap with bad data
3. Watch pods crash
4. Query: "Why is frontend down?"
5. Expected: "ConfigMap change at X caused crash"
```

### Scenario 2: Service Selector Mismatch
```yaml
1. Deploy service with selector app=web
2. Deploy pods with label app=webapp
3. Service has no endpoints
4. Query: "Why is service empty?"
5. Expected: "No pods match selector app=web"
```

### Scenario 3: Resource Cascade
```yaml
1. Node runs out of disk
2. Pods get evicted
3. Services become unavailable
4. Query: "Why is API down?"
5. Expected: "Node disk pressure caused pod evictions"
```

## Success Metrics
- Correlation accuracy > 90%
- Root cause found in < 5 seconds
- Full trace visibility via OTEL
- Positive user feedback > 80%

## Technical Decisions

### Why Neo4j for Everything?
- Natural fit for graph relationships
- Cypher queries perfect for correlation patterns
- Can store events + relationships + correlations
- Single data store = simpler operations

### Why OTEL Everywhere?
- Distributed tracing shows event flow
- Metrics show system health
- Already implemented in key services
- Standard observability for our observability platform

### Why Separate Relationship Extractor?
- Single responsibility principle
- Can optimize K8s watches separately
- Easier to test and debug
- Can run multiple instances if needed

## Next Steps After MVP
1. Predictive correlations
2. Custom correlation rules (YAML/DSL)
3. Historical pattern analysis
4. Cost correlation (waste detection)
5. Security correlation (RBAC issues)

## Complete Component List

### Services
- **collectors** (existing - keep as is)
- **transformer** (existing - has OTEL)
- **correlation-service** (existing - has OTEL)
- **k8s-grapher** (new - builds K8s relationship graph)
- **api-service** (new - exposes correlations via REST)

### Correlators
- **PerformanceCorrelator** (existing - CPU/memory/crash patterns)
- **ServiceMapCorrelator** (existing - service dependencies)
- **DependencyCorrelator** (new - K8s object dependencies)
- **ConfigImpactCorrelator** (new - config change impacts)
- **OwnershipCorrelator** (new - ownership chain issues)
- **CascadeCorrelator** (future - cascading failures)

### Aggregator
- **CorrelationAggregator** (new - combines all correlator outputs into final answer)

### OTEL Instrumentation
- **ServiceInstrumentation** (base - already exists)
- **CorrelationInstrumentation** (exists)
- **TransformerInstrumentation** (exists)
- **K8sGrapherInstrumentation** (new)
- **AggregatorInstrumentation** (new)
- **APIInstrumentation** (new)

## Remember
The goal is to answer "Why is X broken?" not "What is broken?"
This is what makes Tapio different from every other tool.