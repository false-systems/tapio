# K8s Behavior Research Engine - Complete Architecture

## Executive Summary

Tapio is not an observability platform - it's a **K8s behavior research engine** that reveals the hidden deterministic patterns in Kubernetes. While kubectl shows symptoms, Tapio reveals the complete context and causality chains that led to those symptoms.

### Core Insight
Kubernetes is a deterministic chaos-to-order machine that does the same things repeatedly, but hides its actual behavior from operators. Tapio makes this hidden behavior visible through comprehensive event collection, graph-based correlation, and pattern recognition.

## System Architecture

### Complete Data Flow

```
Collectors → RawEvent → Pipeline → UnifiedEvent → NATS → Correlation Engine → Neo4j Graph → Insights
    ↑                                                           ↑                      ↑
    |                                                           |                      |
  eBPF/K8s                                                  Patterns              User Feedback
```

## 1. Data Collection Layer

### Collectors (8 Types)

We control the entire data collection pipeline with kernel-level visibility:

```go
// pkg/collectors/interface.go
type RawEvent struct {
    Timestamp time.Time           // When collected
    Type      string              // "kernel", "kubeapi", "cni", etc.
    Data      []byte              // Raw event data
    Metadata  map[string]string   // Parsed metadata
    TraceID   string              // OTEL trace propagation
    SpanID    string              // OTEL span ID
}
```

#### Active Collectors:

1. **Kernel (eBPF)** - Syscalls, processes, network at kernel level
2. **KubeAPI** - K8s resource changes, watch events
3. **CNI** - Container network events
4. **DNS** - DNS queries and failures
5. **CRI** - Container runtime events
6. **Kubelet** - Node-level metrics
7. **ETCD** - Configuration changes
8. **SystemD** - System service events

### What Makes Our Collection Unique

Unlike traditional observability tools that passively receive metrics/logs, we:
- **See events at kernel level** before they reach applications
- **Track full causality** from syscall to service failure
- **Inject trace context** at collection time
- **Capture complete K8s context** for every event

## 2. Event Enrichment Pipeline

### RawEvent → UnifiedEvent Transformation

```go
// pkg/domain/unified_event.go
type UnifiedEvent struct {
    // Core Identity
    ID        string    
    Timestamp time.Time 
    Type      EventType 
    Source    string    // Which collector
    
    // OTEL Context
    TraceContext *TraceContext
    
    // Semantic Context (What this means)
    Semantic *SemanticContext {
        Intent     string   // "config-change", "oom-kill"
        Category   string   // "performance", "availability"
        Narrative  string   // Human description
    }
    
    // K8s Context (Critical for correlation)
    K8sContext *K8sContext {
        Kind      string              // Pod, Service, ConfigMap
        Name      string
        Namespace string
        UID       string
        Labels    map[string]string
    }
    
    // Layer-Specific Data
    Kernel     *KernelData      // eBPF events
    Network    *NetworkData     // Network events
    Kubernetes *KubernetesData  // K8s events
    
    // Correlation Hints
    CorrelationHints []string
}
```

### Enrichment Process

```go
// pkg/collectors/pipeline/types.go
func (e *EnrichedEvent) ConvertToUnified() *domain.UnifiedEvent {
    event := &domain.UnifiedEvent{
        ID:        GenerateEventID(),
        Timestamp: e.Raw.Timestamp,
        Type:      mapCollectorTypeToDomain(e.Raw.Type),
        Source:    e.Raw.Type,
    }
    
    // Add K8s context from enricher
    if e.K8sObject != nil {
        event.K8sContext = &domain.K8sContext{
            Kind:      e.K8sObject.Kind,
            Name:      e.K8sObject.Name,
            Namespace: e.K8sObject.Namespace,
            UID:       e.K8sObject.UID,
            Labels:    e.K8sObject.Labels,
        }
    }
    
    // Propagate OTEL traces
    if e.TraceID != "" {
        event.TraceContext = &domain.TraceContext{
            TraceID: e.TraceID,
            SpanID:  e.SpanID,
        }
    }
    
    return event
}
```

## 3. Event Distribution (NATS)

### NATS JetStream Configuration

```go
// Persistent stream with replay capability
Stream: "EVENTS"
Subjects: ["events.>"]
Retention: "Limits"  // Keep last 1M events
Replicas: 3
MaxAge: 24 * time.Hour

// Event publishing
Subject: fmt.Sprintf("events.%s.%s", event.Type, event.K8sContext.Namespace)

// Guaranteed delivery
PublishAsync with acknowledgment
At-least-once delivery semantics
```

## 4. Correlation Engine - The Intelligence Layer

### Design Philosophy

Instead of hardcoded correlators with complex abstractions, we use:
1. **Pattern-based matching** from YAML definitions
2. **Graph traversal** for context discovery
3. **User feedback** for continuous improvement

### Architecture (5,350 lines total)

```
pkg/
├── domain/                    # Core types (350 LOC)
│   ├── behavior_pattern.go    # Pattern definitions
│   ├── feedback.go            # User feedback model
│   └── prediction.go          # Prediction model
│
├── intelligence/              # Business logic (1,050 LOC)
│   ├── behavior/             
│   │   ├── engine.go          # Main correlation engine
│   │   ├── pattern_matcher.go # Pattern matching logic
│   │   └── predictor.go       # Prediction generation
│   └── pattern_loader/       
│       ├── loader.go          # YAML pattern loading
│       └── watcher.go         # Hot-reload capability
│
├── integrations/             # External services (400 LOC)
│   └── neo4j_feedback/      
│       └── store.go          # Neo4j operations
│
└── interfaces/              # APIs (400 LOC)
    └── api/
        ├── feedback_handler.go # User feedback API
        └── health_handler.go    # Health checks
```

### Pattern Definition System

Patterns are defined in YAML, not code. They can be modified without recompiling:

```yaml
# /etc/tapio/patterns/oom-cascade.yaml
apiVersion: behavior.tapio.io/v1
kind: BehaviorPattern
metadata:
  name: oom-cascade
  version: "1.0.0"
spec:
  enabled: true
  priority: 100
  
  # Conditions that trigger this pattern
  conditions:
    - type: metric
      field: memory.usage_percent
      operator: gt
      value: 85
      time_window: 5m
    - type: event
      field: type
      operator: equals
      value: "OOMKilled"
      
  # Predictions based on pattern match
  predictions:
    - type: cascading_failure
      confidence_base: 0.75
      time_horizon: 10m
      message: "Memory pressure will cascade to other pods"
      impact:
        - service_degradation
        - pod_evictions
```

### Core Correlation Process

```go
type BehaviorEngine struct {
    patterns        []BehaviorPattern  // Loaded from YAML
    patternMatcher  *PatternMatcher    // Matches events to patterns
    contextRevealer *ContextRevealer   // Queries Neo4j for context
    predictor       *Predictor         // Generates predictions
    
    // Reliability
    circuitBreaker  *CircuitBreaker    // Protects against failures
    backpressure    *BackpressureManager
    
    // OTEL (direct usage, no wrappers)
    tracer          trace.Tracer
    meter           metric.Meter
}

func (e *BehaviorEngine) Process(ctx context.Context, event *UnifiedEvent) (*Insight, error) {
    // 1. Match against patterns
    matches := e.patternMatcher.Match(ctx, event)
    
    // 2. Query Neo4j for context
    context := e.contextRevealer.RevealContext(ctx, event)
    
    // 3. Generate prediction with confidence
    prediction := e.predictor.Generate(ctx, matches, context)
    
    // 4. Store in Neo4j for graph building
    e.storeEventAndRelationships(ctx, event, prediction)
    
    return &Insight{
        Event:       event,
        Context:     context,
        Prediction:  prediction,
        Remediation: e.generateRemediation(prediction),
    }, nil
}
```

## 5. Neo4j Graph - The Knowledge Base

### Graph Schema

```cypher
// Resource Nodes (K8s topology)
(:Pod {uid, name, namespace, labels, state})
(:Service {uid, name, namespace, type, selector})
(:ConfigMap {uid, name, namespace, data_keys})
(:Node {uid, name, capacity, conditions})

// Event Nodes (What happened)
(:Event {id, type, timestamp, severity, source})

// Pattern Nodes (Learned behaviors)
(:Pattern {id, name, confidence, occurrences})

// Prediction Nodes (What we predicted)
(:Prediction {id, pattern_id, confidence, time_horizon})

// Feedback Nodes (User validation)
(:Feedback {id, rating, accurate, timestamp})
```

### Relationships - The Hidden Context

```cypher
// Resource relationships (K8s topology)
(Pod)-[:RUNS_ON]->(Node)
(Pod)-[:MOUNTS]->(ConfigMap)
(Pod)-[:USES_SECRET]->(Secret)
(Service)-[:SELECTS]->(Pod)
(Deployment)-[:OWNS]->(ReplicaSet)-[:OWNS]->(Pod)

// Event relationships (Causality)
(Event)-[:CAUSED]->(Event)
(Event)-[:AFFECTED]->(Resource)
(Event)-[:PRECEDED {delay: duration}]->(Event)

// Pattern relationships (Intelligence)
(Event)-[:MATCHES]->(Pattern)
(Pattern)-[:PREDICTED]->(Prediction)
(Feedback)-[:VALIDATES]->(Prediction)
```

### The Context Revelation Query

This is the query that reveals what kubectl cannot show:

```cypher
// Find complete context for an event
MATCH (e:Event {id: $eventId})

// Find affected resources
OPTIONAL MATCH (e)-[:AFFECTED]->(resource)

// Find ownership chain
OPTIONAL MATCH path = (resource)<-[:OWNS*]-(owner)

// Find dependencies
OPTIONAL MATCH (resource)-[:DEPENDS_ON|MOUNTS|SELECTS*1..3]-(dependent)

// Find temporal correlation
OPTIONAL MATCH (temporal:Event)
WHERE temporal.timestamp > e.timestamp - 300000  // 5 min before
  AND temporal.timestamp < e.timestamp + 300000   // 5 min after
  AND (temporal)-[:AFFECTED]->()<-[:AFFECTED]-(e)

// Find causal chain
OPTIONAL MATCH (root:Event)-[:CAUSED*]->(e)
OPTIONAL MATCH (e)-[:CAUSED*]->(consequence:Event)

RETURN e as event,
       collect(DISTINCT resource) as affected_resources,
       collect(DISTINCT owner) as ownership_chain,
       collect(DISTINCT dependent) as dependencies,
       collect(DISTINCT temporal) as correlated_events,
       root as root_cause,
       collect(DISTINCT consequence) as consequences
```

### Example: What This Reveals

**Scenario**: Pod is CrashLooping

**kubectl shows**:
```
NAME                    READY   STATUS             RESTARTS   AGE
nginx-7d9b8c5-x2kl9    0/1     CrashLoopBackOff   5          10m
```

**Tapio's graph reveals**:
```
23 minutes ago: ConfigMap 'nginx-config' updated
  ├─ Mounted by: 15 nginx pods across 3 nodes
  ├─ 3 pods already restarted
  ├─ 12 pods pending restart
  ├─ Service 'frontend' selecting these pods
  ├─ Ingress 'api.company.com' routing to service
  ├─ Previous occurrence: Same pattern 3 days ago
  ├─ Root cause: Config syntax error in line 42
  └─ Predicted impact: 10K user requests will fail
```

## 6. User Feedback Loop

### Feedback System Design

```go
// API: POST /api/v1/feedback
type FeedbackRequest struct {
    PredictionID string  `json:"prediction_id"`
    Accurate     bool    `json:"accurate"`
    Rating       int     `json:"rating"` // -1, 0, 1
    Comment      string  `json:"comment"`
}

// Stored in Neo4j
(:Feedback {
    id: "feedback-123",
    prediction_id: "pred-456",
    accurate: true,
    rating: 1,
    timestamp: 1234567890
})-[:VALIDATES]->(:Prediction)

// Confidence adjustment
newConfidence = baseConfidence * feedbackFactor
where feedbackFactor = 0.5 + (accuracyRate * 0.5)
```

### How Patterns Improve

1. **Initial**: Pattern has base confidence (0.75)
2. **User validates**: "Yes, this prediction was correct"
3. **System adjusts**: Confidence increases to 0.82
4. **After 100 validations**: Confidence stabilizes at actual accuracy
5. **Pattern evolution**: Low-confidence patterns removed, high-confidence promoted

## 7. Production Reliability

### Rock-Solid Design

```go
// Every external call protected
func (e *Engine) Process(ctx context.Context, event Event) error {
    // Circuit breaker prevents cascade failures
    return e.circuitBreaker.Execute(ctx, func() error {
        // Timeout on everything
        ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
        defer cancel()
        
        // Backpressure prevents overload
        if !e.backpressure.TryAccept() {
            return ErrOverloaded
        }
        defer e.backpressure.Release()
        
        // Process with full observability
        ctx, span := e.tracer.Start(ctx, "engine.process")
        defer span.End()
        
        return e.processInternal(ctx, event)
    })
}
```

### Graceful Degradation

```go
const (
    ModeNormal   = iota // All features enabled
    ModeDegraded        // Disable predictions, keep matching
    ModeReadOnly        // No writes, only queries
    ModeMaintenance     // Reject all requests
)

// Automatically degrade under pressure
if memoryUsage > 80 {
    e.SetMode(ModeDegraded)
}
```

## 8. Key Patterns We Detect

### Built-in Patterns (Start with these)

1. **OOM Cascade**: Memory pressure spreading across pods
2. **Config Drift**: ConfigMap changes causing instability
3. **Noisy Neighbor**: Resource contention on nodes
4. **Scheduler Thrash**: Eviction/reschedule loops
5. **DNS Black Hole**: CoreDNS failures cascading
6. **Liveness Death Spiral**: Probe failures causing restarts
7. **PVC Deadlock**: Volume attachment failures
8. **Network Partition**: CNI issues isolating pods

### Pattern Discovery (Future)

As the system collects data, Neo4j queries reveal new patterns:

```cypher
// Discover unknown patterns
MATCH (e1:Event)-[:CAUSED]->(e2:Event)
WHERE e2.type = 'ServiceDown'
WITH e1.type as cause, e2.type as effect, count(*) as occurrences
WHERE occurrences > 10
RETURN cause, effect, occurrences
ORDER BY occurrences DESC

// Result: "DiskPressure" → "ServiceDown" (73 times)
// New pattern discovered!
```

## 9. Implementation Timeline

### Week 1: Core Pattern System
- Pattern loader with YAML definitions
- Basic pattern matching
- OTEL instrumentation
- Hot-reload capability
- **Deliverable**: Load pattern, match event, see metrics

### Week 2: Feedback System
- REST API for feedback
- Neo4j storage with batching
- Confidence adjustment
- **Deliverable**: Submit feedback, see confidence change

### Week 3: Prediction Engine
- Prediction generation
- Context revelation from Neo4j
- Time-based correlation
- **Deliverable**: Event → Context → Prediction

### Week 4: Production Hardening
- Circuit breakers
- Backpressure handling
- Health checks
- Load testing
- **Deliverable**: Handle 10K events/sec

## 10. Why This Is Different

### Traditional Observability
- **What they show**: "Service is down"
- **When**: After it happens
- **Context**: Limited to recent logs/metrics
- **Improvement**: None - same alerts forever

### Tapio's Behavior Research
- **What we show**: "Service will fail in 10 minutes because ConfigMap change is propagating"
- **When**: Before it happens
- **Context**: Complete causality chain from kernel to service
- **Improvement**: Gets smarter with every user feedback

## Performance Targets

- **Event Processing**: 10,000+ events/sec
- **Pattern Matching**: < 100ms per event
- **Neo4j Queries**: < 200ms for context
- **Prediction Generation**: < 500ms total
- **Memory Usage**: < 4GB for 1M events/hour
- **Pattern Hot-reload**: < 1 second

## Success Metrics

- **Prediction Accuracy**: > 75% after feedback training
- **Time to Root Cause**: < 30 seconds (vs 30+ minutes manual)
- **Context Completeness**: 95% of related resources identified
- **False Positive Rate**: < 10% after tuning
- **User Satisfaction**: "Finally, I can see what K8s is actually doing!"

## Conclusion

Tapio is not monitoring K8s - it's studying K8s behavior. By collecting comprehensive data from kernel to application level, building a graph of relationships and causality, and learning from user feedback, we reveal the hidden deterministic patterns that cause failures.

The system is lean (5,350 lines vs 34,000), reliable (circuit breakers everywhere), and intelligent (learns from feedback). Most importantly, it provides what kubectl cannot: the complete context of why things happen in Kubernetes.

---

*"K8s does the same things repeatedly, but hides its behavior. We make it visible."*