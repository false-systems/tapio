# Tapio Event Processing Pipeline Design

## Overview
This document describes the complete event flow from raw collector data to Neo4j knowledge graph.

## Event Flow Architecture

```
Collectors → RawEvent → UnifiedEvent → EventProcessor → Neo4j Knowledge Graph
                ↓            ↓              ↓                    ↓
           (Raw Data)   (Normalized)   (Enriched)        (Intelligence)
```

## 1. RawEvent Structure

RawEvent is the initial event from collectors:

```go
type RawEvent struct {
    Timestamp time.Time              // When it happened
    Type      string                 // Collector-specific type
    Data      []byte                 // JSON payload
    Metadata  map[string]string      // Collector metadata
    Source    string                 // Which collector
}
```

### Example RawEvents:

**From Kubelet Collector:**
```json
{
    "timestamp": "2024-01-15T10:30:00Z",
    "type": "PodStatusUpdate",
    "source": "kubelet_collector",
    "metadata": {
        "node": "node-1",
        "cluster": "prod-west"
    },
    "data": {
        "pod": "frontend-5d4b5c6d7-xyz",
        "namespace": "production",
        "phase": "Failed",
        "reason": "OOMKilled",
        "containers": [{
            "name": "nginx",
            "restartCount": 3,
            "lastState": {
                "terminated": {
                    "reason": "OOMKilled",
                    "exitCode": 137
                }
            }
        }]
    }
}
```

**From eBPF Collector:**
```json
{
    "timestamp": "2024-01-15T10:30:00Z",
    "type": "NetworkConnection",
    "source": "ebpf_collector",
    "metadata": {
        "node": "node-1"
    },
    "data": {
        "src_pod": "frontend-5d4b5c6d7-xyz",
        "dst_service": "backend-service",
        "protocol": "TCP",
        "state": "connection_refused",
        "attempts": 15
    }
}
```

## 2. UnifiedEvent Structure

UnifiedEvent is the normalized format used throughout Tapio:

```go
type UnifiedEvent struct {
    // Identity
    ID        string              // Unique event ID
    Type      EventType          // Normalized event type
    Timestamp time.Time          // When it occurred
    
    // Context
    Source      string           // Collector source
    K8sContext  *K8sContext      // Kubernetes context
    Entity      *EntityInfo      // Alternative entity info
    
    // Event Data
    Severity    Severity         // critical/high/medium/low
    Message     string           // Human-readable message
    Data        interface{}      // Event-specific data
    Metadata    map[string]string // Additional metadata
    
    // Processing
    ProcessedAt time.Time        // When we processed it
    Annotations map[string]string // Processing annotations
}
```

## 3. Transformation Rules: RawEvent → UnifiedEvent

### Event Type Mapping

| Collector | Raw Type | Unified Type | Description |
|-----------|----------|--------------|-------------|
| Kubelet | PodStatusUpdate | pod_failed | Pod entered Failed state |
| Kubelet | PodStatusUpdate | pod_restart | Container restarted |
| Kubelet | PodReady | pod_ready | Pod became ready |
| eBPF | NetworkConnection | connection_failed | TCP connection refused |
| eBPF | NetworkConnection | service_unavailable | Service has no endpoints |
| CNI | NetworkPolicy | network_policy_violation | Traffic blocked by policy |
| Config | ConfigMapUpdate | config_changed | ConfigMap was modified |
| Config | SecretUpdate | secret_changed | Secret was modified |

### Transformation Example

```go
func TransformKubeletEvent(raw RawEvent) UnifiedEvent {
    var data KubeletEventData
    json.Unmarshal(raw.Data, &data)
    
    unified := UnifiedEvent{
        ID:        generateEventID(raw),
        Timestamp: raw.Timestamp,
        Source:    raw.Source,
    }
    
    // Determine event type
    switch {
    case data.Phase == "Failed":
        unified.Type = "pod_failed"
        unified.Severity = SeverityHigh
        unified.Message = fmt.Sprintf("Pod %s failed: %s", data.Pod, data.Reason)
        
    case data.Containers[0].RestartCount > 0:
        unified.Type = "pod_restart"
        unified.Severity = SeverityMedium
        unified.Message = fmt.Sprintf("Container %s restarted %d times", 
            data.Containers[0].Name, data.Containers[0].RestartCount)
    }
    
    // Build K8s context
    unified.K8sContext = &K8sContext{
        Name:        data.Pod,
        Namespace:   data.Namespace,
        Kind:        "Pod",
        ClusterName: raw.Metadata["cluster"],
        NodeName:    raw.Metadata["node"],
    }
    
    // Preserve original data
    unified.Data = data
    
    return unified
}
```

## 4. Neo4j Graph Schema

### Node Types

```cypher
// Resource nodes
(:Pod {
    name: string,
    namespace: string,
    cluster: string,
    uid: string,
    phase: string,
    ready: boolean,
    node: string,
    restart_count: integer,
    last_restart: datetime,
    created_at: datetime,
    updated_at: datetime
})

(:Service {
    name: string,
    namespace: string,
    cluster: string,
    uid: string,
    type: string,  // ClusterIP, LoadBalancer, etc
    selector: map,
    endpoint_count: integer,
    created_at: datetime,
    updated_at: datetime
})

(:ConfigMap {
    name: string,
    namespace: string,
    cluster: string,
    uid: string,
    version: string,
    last_modified: datetime,
    created_at: datetime
})

// Event nodes
(:Event {
    id: string,
    type: string,
    severity: string,
    message: string,
    timestamp: datetime,
    source: string,
    data: string  // JSON
})
```

### Relationship Types

```cypher
// Resource relationships
(Service)-[:SELECTS]->(Pod)
(Deployment)-[:OWNS]->(ReplicaSet)-[:OWNS]->(Pod)
(Pod)-[:MOUNTS]->(ConfigMap)
(Pod)-[:USES_SECRET]->(Secret)
(Pod)-[:CLAIMS]->(PVC)
(Pod)-[:RUNS_ON]->(Node)

// Event relationships
(Event)-[:AFFECTS]->(Resource)
(Event)-[:TRIGGERED]->(Event)  // Causality
(Event)-[:CORRELATED_WITH]->(Event)  // Same time window
```

## 5. Event Processing Pipeline

### Step 1: Event Reception
```go
func (p *EventProcessor) ProcessEvent(event UnifiedEvent) error {
    // 1. Validate event
    if err := p.validateEvent(event); err != nil {
        return err
    }
    
    // 2. Begin transaction
    tx := p.neo4j.BeginTx()
    defer tx.Rollback()
    
    // 3. Process based on event type
    switch event.Type {
    case "pod_failed", "pod_restart":
        err = p.processPodEvent(tx, event)
    case "config_changed":
        err = p.processConfigEvent(tx, event)
    case "connection_failed":
        err = p.processNetworkEvent(tx, event)
    }
    
    // 4. Create event node
    err = p.createEventNode(tx, event)
    
    // 5. Detect patterns
    patterns := p.detectPatterns(tx, event)
    
    // 6. Commit
    return tx.Commit()
}
```

### Step 2: Resource Updates
```go
func (p *EventProcessor) processPodEvent(tx Transaction, event UnifiedEvent) error {
    // 1. Upsert Pod node
    pod := p.upsertPod(tx, event.K8sContext)
    
    // 2. Update pod state
    if event.Type == "pod_restart" {
        pod.RestartCount++
        pod.LastRestart = event.Timestamp
    }
    
    // 3. Find related resources
    service := p.findServiceForPod(tx, pod)
    configMaps := p.findConfigMapsForPod(tx, pod)
    
    // 4. Create relationships
    p.createEventRelationships(tx, event, pod, service, configMaps)
    
    return nil
}
```

### Step 3: Pattern Detection
```go
func (p *EventProcessor) detectPatterns(tx Transaction, event UnifiedEvent) []Pattern {
    patterns := []Pattern{}
    
    // Death spiral: >3 restarts in 5 minutes
    if event.Type == "pod_restart" {
        restarts := p.countRecentRestarts(tx, event.K8sContext.Name, 5*time.Minute)
        if restarts > 3 {
            patterns = append(patterns, Pattern{
                Type: "death_spiral",
                Confidence: 0.9,
                Message: fmt.Sprintf("Pod %s is crash-looping", event.K8sContext.Name),
            })
        }
    }
    
    // Config cascade: Config change → Pod restarts
    if event.Type == "config_changed" {
        affectedPods := p.findPodsUsingConfig(tx, event.K8sContext.Name)
        if len(affectedPods) > 0 {
            patterns = append(patterns, Pattern{
                Type: "config_cascade",
                Confidence: 0.8,
                Message: fmt.Sprintf("Config change will affect %d pods", len(affectedPods)),
            })
        }
    }
    
    return patterns
}
```

## 6. Query Examples

### Find Root Cause
```cypher
// Why did this pod fail?
MATCH (p:Pod {name: $podName})<-[:AFFECTS]-(e:Event)
WHERE e.timestamp > datetime() - duration({hours: 1})
OPTIONAL MATCH (e)<-[:TRIGGERED*]-(root:Event)
RETURN e, root
ORDER BY e.timestamp DESC
```

### Impact Analysis
```cypher
// What will this config change affect?
MATCH (cm:ConfigMap {name: $configName})<-[:MOUNTS]-(p:Pod)
MATCH (p)<-[:SELECTS]-(s:Service)
RETURN cm, p, s
```

### Pattern Matching
```cypher
// Find death spirals
MATCH (p:Pod)<-[:AFFECTS]-(e:Event {type: 'pod_restart'})
WHERE e.timestamp > datetime() - duration({minutes: 5})
WITH p, COUNT(e) as restart_count
WHERE restart_count > 3
RETURN p.name, restart_count
```

## 7. Implementation Priority

1. **Phase 1: Core Pipeline** (Week 1)
   - RawEvent → UnifiedEvent transformer
   - Basic EventProcessor
   - Pod and Service node creation
   - Event node creation

2. **Phase 2: Relationships** (Week 2)
   - Service → Pod selection
   - Pod → ConfigMap mounts
   - Event → Resource affects
   - Basic causality detection

3. **Phase 3: Intelligence** (Week 3)
   - Pattern detection
   - Causality chains
   - Impact prediction
   - Query API

4. **Phase 4: Advanced** (Week 4)
   - Machine learning patterns
   - Anomaly detection
   - Predictive analysis
   - Auto-remediation hooks

## 8. Testing Strategy

1. **Unit Tests**: Each transformation function
2. **Integration Tests**: Full pipeline with test events
3. **Graph Tests**: Verify correct graph structure
4. **Pattern Tests**: Validate pattern detection
5. **Load Tests**: Handle 1000+ events/second

## 9. Monitoring

- Event processing rate
- Transformation errors
- Graph write performance
- Pattern detection accuracy
- Queue depths

This design ensures every event contributes to our intelligence, building a living knowledge graph that gets smarter over time.