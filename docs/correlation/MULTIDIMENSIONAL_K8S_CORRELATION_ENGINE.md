# Multi-Dimensional K8s Correlation Engine

## Philosophy: Beyond Time-Series Thinking

Traditional observability treats events as points on a timeline. But K8s events exist in multiple dimensions simultaneously. This document outlines how to rebuild the correlation engine to leverage ALL dimensions K8s provides.

## The K8s Dimension Model

### 1. Temporal Dimension (Traditional)
```go
type TemporalDimension struct {
    Timestamp   time.Time
    Duration    time.Duration
    Periodicity *Pattern // Cron jobs, rolling updates
}
```

### 2. Hierarchical Dimension (K8s Native)
```go
type HierarchicalDimension struct {
    Level       int      // 0=Cluster, 1=Namespace, 2=Workload...
    OwnerChain  []string // Full ownership path
    Scope       string   // cluster|namespace|workload|pod
    
    // Navigation
    Parent   string
    Children []string
    Siblings []string
}

// Example: Pod exists in hierarchy
Pod.Hierarchy = {
    Level: 4,
    OwnerChain: ["Cluster", "Namespace/prod", "Deployment/api", "ReplicaSet/api-v2", "Pod/api-v2-abc"],
    Parent: "ReplicaSet/api-v2",
    Siblings: ["Pod/api-v2-def", "Pod/api-v2-ghi"],
}
```

### 3. Lifecycle Dimension (State Machine)
```go
type LifecycleDimension struct {
    CurrentPhase  string
    DesiredPhase  string
    Transitions   []StateTransition
    TimeInPhase   time.Duration
    
    // K8s Conditions
    Conditions []Condition
    
    // State machine rules
    ValidTransitions map[string][]string
}

// K8s enforces these transitions
PodLifecycle = {
    "Pending": ["Running", "Failed"],
    "Running": ["Succeeded", "Failed", "Terminating"],
    "Terminating": ["Terminated"],
}
```

### 4. Intentional Dimension (Declarative Nature)
```go
type IntentionalDimension struct {
    // What the human declared
    DeclaredIntent interface{} // Deployment spec
    
    // What K8s is trying to achieve
    ReconcilingTo interface{} // Current target state
    
    // The gap
    DivergenceReason string
    DivergenceMetric float64 // How far from intent
    
    // Intent metadata
    IntentAuthor     string // Who made the change
    IntentReason     string // Commit message, PR description
    BusinessIntent   string // "Scale for Black Friday"
}

// This is UNIQUE to K8s!
// We can always ask: "What was supposed to happen?"
```

### 5. Relational Dimension (Dependency Graph)
```go
type RelationalDimension struct {
    // Direct relationships
    Provides   []Resource // Services this pod provides
    Consumes   []Resource // ConfigMaps, Secrets, PVCs
    NetworksTo []Resource // Other pods/services
    
    // Indirect relationships  
    SharedNode      []Resource // Pod neighbors
    SharedNamespace []Resource // Namespace neighbors
    SharedLabels    []Resource // Label selector matches
    
    // Relationship strength
    CouplingScore float64 // How tightly coupled
}
```

### 6. Semantic Dimension (Meaning)
```go
type SemanticDimension struct {
    // Technical classification
    WorkloadType string // "stateless-app", "database", "cache"
    Pattern      string // "web-server", "worker", "cron-job"
    
    // Business classification
    BusinessUnit    string
    CriticalityTier string // "critical", "important", "standard"
    DataClass       string // "public", "confidential", "restricted"
    
    // Behavioral classification
    TrafficPattern string // "user-facing", "internal-only", "batch"
    ScalingPattern string // "horizontal", "vertical", "static"
}
```

### 7. Evolutionary Dimension (Change Over Time)
```go
type EvolutionaryDimension struct {
    // Version tracking
    Generation        int64
    RevisionHistory   []Revision
    ChangeFrequency   float64
    
    // Change patterns
    DeploymentPattern string // "blue-green", "rolling", "recreate"
    UpdateVelocity    float64 // Changes per day
    
    // Stability metrics
    SettlingTime      time.Duration // Time to stability after change
    FailureRate       float64       // Rollback frequency
}
```

## Multi-Dimensional Correlation Algorithm

```go
type MultiDimensionalCorrelator struct {
    dimensions []Dimension
    weights    map[string]float64
}

func (m *MultiDimensionalCorrelator) Correlate(event *UnifiedEvent) *StoryContext {
    // 1. Project event into all dimensions
    projections := map[string]interface{}{}
    for _, dim := range m.dimensions {
        projections[dim.Name()] = dim.Project(event)
    }
    
    // 2. Find related events in each dimension
    candidates := map[string][]*UnifiedEvent{}
    
    // Temporal: Events within time window
    candidates["temporal"] = m.findTemporalNeighbors(event, 5*time.Minute)
    
    // Hierarchical: Events in ownership chain
    candidates["hierarchy"] = m.findHierarchicalRelatives(event)
    
    // Lifecycle: Events in state machine path
    candidates["lifecycle"] = m.findLifecycleCompanions(event)
    
    // Intentional: Events diverging from same intent
    candidates["intent"] = m.findIntentionalDivergences(event)
    
    // Relational: Events from dependencies
    candidates["relations"] = m.findRelationalConnections(event)
    
    // 3. Score correlations using multi-dimensional distance
    scored := m.scoreMultiDimensional(candidates, projections)
    
    // 4. Build story context
    return &StoryContext{
        PrimaryEvent: event,
        Dimensions:   projections,
        Correlated:   scored.TopN(10),
        Confidence:   scored.Confidence(),
        Narrative:    m.generateNarrative(scored),
    }
}

// Multi-dimensional distance function
func (m *MultiDimensionalCorrelator) distance(e1, e2 *UnifiedEvent) float64 {
    distance := 0.0
    
    // Each dimension contributes to distance
    distance += m.weights["temporal"] * m.temporalDistance(e1, e2)
    distance += m.weights["hierarchy"] * m.hierarchicalDistance(e1, e2)
    distance += m.weights["lifecycle"] * m.lifecycleDistance(e1, e2)
    distance += m.weights["intent"] * m.intentionalDistance(e1, e2)
    distance += m.weights["relations"] * m.relationalDistance(e1, e2)
    distance += m.weights["semantic"] * m.semanticDistance(e1, e2)
    distance += m.weights["evolution"] * m.evolutionaryDistance(e1, e2)
    
    return distance
}
```

## K8s-Specific Correlation Patterns

### Pattern 1: Deployment Story (Multi-Dimensional)
```go
type DeploymentStory struct {
    // Temporal: When did it start/end
    Timeline Timeline
    
    // Hierarchical: What was affected  
    AffectedHierarchy []string // Deploy → RS → Pods
    
    // Lifecycle: State transitions
    StateFlow []StateTransition // Creating → Progressing → Complete
    
    // Intentional: What was desired
    Intent    string // "Update to v2.0"
    Achieved  bool   // Did we get there?
    
    // Relational: What else was impacted
    ImpactedServices []string
    
    // Semantic: What kind of deployment
    DeploymentType string // "feature", "hotfix", "rollback"
    
    // Evolutionary: How does this fit history
    ChangeVelocity float64 // Faster than usual?
}
```

### Pattern 2: Resource Pressure Story
```go
type ResourcePressureStory struct {
    // Spatial dimension comes into play
    AffectedNodes []string
    PressureType  string // "memory", "cpu", "disk"
    
    // Relational: Who got evicted and why
    EvictionChain []EvictionEvent
    
    // Intentional: Over-provisioned?
    RequestVsLimit map[string]ResourceGap
    
    // Semantic: Business impact
    AffectedServices []BusinessService
}
```

## Implementation Strategy

### Phase 1: Dimension Extractors
```go
// Extract all dimensional data from K8s events
type DimensionExtractor interface {
    Extract(event *UnifiedEvent) DimensionalProjection
    Name() string
}

type HierarchyExtractor struct{}
func (h *HierarchyExtractor) Extract(event *UnifiedEvent) DimensionalProjection {
    // Use ownerReferences
    // Use namespace
    // Use labels/selectors
}

type IntentExtractor struct{}
func (i *IntentExtractor) Extract(event *UnifiedEvent) DimensionalProjection {
    // Compare spec vs status
    // Extract deployment strategy
    // Find declared replicas vs actual
}
```

### Phase 2: Multi-Dimensional Index
```go
// Instead of time-series DB, we need multi-dimensional index
type MultiDimensionalIndex struct {
    temporal   *TimeSeriesIndex      // Traditional
    hierarchy  *TreeIndex            // For ownership
    lifecycle  *StateMachineIndex    // For phase transitions
    relational *GraphIndex           // For dependencies
    semantic   *VectorIndex          // For meaning similarity
}
```

### Phase 3: Story Patterns
```go
// Stories emerge from dimensional patterns
type StoryPattern struct {
    Name       string
    Dimensions []DimensionCriteria
    
    // Example: "Pod Failure Story"
    // - Temporal: Events within 5 min
    // - Hierarchy: Same ReplicaSet
    // - Lifecycle: Running → Failed
    // - Intent: Replicas < Desired
    // - Semantic: Same workload type
}
```

## The Philosophical Advantage

1. **Time is just ONE dimension** - Not privileged
2. **K8s relationships are multi-dimensional** - Use them all
3. **Stories exist in dimension intersections** - Not just time sequences
4. **Correlation is geometric** - Distance in multi-dimensional space

## Why This Changes Everything

### Traditional: "Show me errors in the last hour"
```sql
SELECT * FROM events 
WHERE severity = 'error' 
AND timestamp > NOW() - 1h
```

### Multi-Dimensional: "Show me the deployment story"
```go
story := correlator.FindStory(
    Hierarchical("Deployment/api"),
    Intentional("scale to 10 replicas"),
    Lifecycle("Progressing"),
    Relational("affects Service/api"),
    Temporal(LastHour),
)
// Returns rich, multi-dimensional narrative
```

## Competitive Moat

1. **Requires deep K8s understanding** - Not just metrics
2. **Requires philosophical approach** - Not just engineering
3. **Requires complete rewrite** - Not bolt-on feature
4. **Requires K8s-only focus** - Not generic platform

## Conclusion

By embracing K8s's multi-dimensional nature, we transform observability from "data overload" to "story clarity". This is the paradigm shift the industry needs.

Time-series thinking is single-dimensional. K8s thinking is multi-dimensional. That's our secret sauce.

---

*"In K8s, every event exists not just in time, but in a rich dimensional space of intent, hierarchy, relationships, and meaning. Our job is to navigate this space and extract stories."*