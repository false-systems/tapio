# CRITICAL: Correlation System Redesign

## The Truth: We Built an Event Enrichment System, Not a Correlation System

### Current Reality
- **What we have**: Event → Insight Generator
- **What we need**: Event A + Event B + Event C → "These are related because X"

## Core Correlation Capabilities We Must Build

### 1. K8s Relationship Mapping (Week 1)
```go
// MUST populate these relationships from K8s API
type K8sRelationshipMap struct {
    // Pod → ReplicaSet → Deployment
    PodOwners map[string]ResourceRef
    
    // Service → Endpoints → Pods  
    ServiceEndpoints map[string][]string
    
    // ConfigMap/Secret → Pods using them
    ConfigUsers map[string][]string
    
    // Node → Pods running on it
    NodePods map[string][]string
}

// Populate on startup and keep updated
func (k *K8sCorrelator) SyncWithK8sAPI() {
    // Watch Deployments, Services, Pods
    // Build relationship graph
    // Update on changes
}
```

### 2. Event Relationship Tracking (Week 1)
```go
type EventRelationshipTracker struct {
    // Which events happened to same resource
    ResourceEvents map[ResourceKey][]EventRef
    
    // Which events happened in sequence
    EventSequences map[string]*Sequence
    
    // Parent-child event relationships
    EventCausality map[EventID][]EventID
}

// Core correlation logic
func (t *Tracker) RelateEvents(eventA, eventB Event) *Relationship {
    // Same resource?
    if eventA.ResourceKey == eventB.ResourceKey {
        return &Relationship{Type: "SameResource", Confidence: 1.0}
    }
    
    // Owner relationship?
    if t.k8sMap.IsOwner(eventA.Resource, eventB.Resource) {
        return &Relationship{Type: "OwnerChild", Confidence: 0.95}
    }
    
    // Temporal relationship?
    if t.isTemporallyRelated(eventA, eventB) {
        return &Relationship{Type: "Temporal", Confidence: 0.8}
    }
}
```

### 3. Pattern Detection Engine (Week 2)
```go
type PatternEngine struct {
    Patterns []CorrelationPattern
}

type CorrelationPattern interface {
    Match(events []Event) *CorrelationMatch
}

// Example: Cascading Failure Pattern
type CascadingFailurePattern struct{}

func (p *CascadingFailurePattern) Match(events []Event) *CorrelationMatch {
    // Look for: Resource exhaustion → Pod failures → Service errors
    
    rootCause := findResourceExhaustion(events)
    if rootCause == nil {
        return nil
    }
    
    podFailures := findPodFailuresAfter(rootCause, events)
    if len(podFailures) < 2 {
        return nil
    }
    
    serviceErrors := findServiceErrorsAfter(podFailures, events)
    if len(serviceErrors) == 0 {
        return nil
    }
    
    return &CorrelationMatch{
        Pattern: "CascadingFailure",
        RootCause: rootCause,
        Chain: append(podFailures, serviceErrors...),
        Confidence: calculateConfidence(rootCause, podFailures, serviceErrors),
    }
}
```

### 4. Correlation State Machine (Week 2)
```go
type CorrelationStateMachine struct {
    ActiveCorrelations map[string]*ActiveCorrelation
}

type ActiveCorrelation struct {
    ID          string
    State       CorrelationState // Building, Active, Resolved
    RootEvent   Event
    RelatedEvents []Event
    Pattern     string
    StartTime   time.Time
    LastUpdate  time.Time
}

// State transitions
func (c *ActiveCorrelation) AddEvent(event Event) {
    switch c.State {
    case Building:
        c.RelatedEvents = append(c.RelatedEvents, event)
        if c.hasEnoughEvidence() {
            c.State = Active
            c.notify()
        }
    case Active:
        c.RelatedEvents = append(c.RelatedEvents, event)
        c.updateSeverity()
    }
}
```

## Implementation Priority

### Week 1: Make It Actually Correlate
1. **K8s Relationship Loader**
   - Connect to K8s API
   - Load all relationships
   - Keep cache updated
   
2. **Basic Event Correlation**
   - Same resource correlation
   - Owner-based correlation
   - Service dependency correlation

3. **Correlation Output**
   - Proper correlation results (not just insights)
   - Event relationship graph
   - Timeline visualization

### Week 2: Make It Smart
1. **Pattern Library**
   - Cascading failures
   - Resource exhaustion
   - Deployment issues
   - Network partitions

2. **Temporal Correlation**
   - Event sequences
   - Time-based patterns
   - Burst detection

3. **Correlation Quality**
   - Confidence scoring
   - False positive tracking
   - Feedback incorporation

### Week 3: Make It Scale
1. **Performance**
   - Efficient graph operations
   - Parallel pattern matching
   - Memory optimization

2. **Persistence**
   - Store correlations
   - Historical analysis
   - Pattern learning

## Success Criteria

A correlation system that can:

1. **Identify cascading failures**
   ```
   MySQL OOM → API timeouts → Frontend 503s
   "Root cause: MySQL memory exhaustion affecting 3 services"
   ```

2. **Group related events**
   ```
   10 pods failing → All from same deployment
   "Deployment 'api-server' experiencing widespread failures"
   ```

3. **Predict impact**
   ```
   Node going down → Which services affected
   "Node failure will impact: payment-service, order-service"
   ```

4. **Provide actionable insights**
   ```
   Pattern detected → Root cause → Fix
   "Recurring OOM every 6 hours. Cause: Memory leak. Action: Update to v2.1.3"
   ```

## Measuring Success

- **Correlation Accuracy**: >90% of related events correctly grouped
- **Root Cause Accuracy**: >80% correct root cause identification  
- **Noise Reduction**: 70% fewer individual alerts (grouped instead)
- **MTTR Impact**: 50% faster incident resolution

## The Bottom Line

We need to build a system that answers: **"What broke, why it broke, and what else is affected"** - not just "here are some enriched events."

This is the difference between a monitoring tool and an intelligence system.