# Multi-Dimensional Correlation Engine Deep Dive

## How The Correlation Engine Works

Instead of simple time-based correlation, our engine correlates events across multiple dimensions simultaneously, leveraging K8s's rich context.

## Correlation Algorithm

```go
type MultiDimensionalCorrelator struct {
    dimensions []CorrelationDimension
    k8sGraph   *K8sResourceGraph
    scorer     *CorrelationScorer
}

func (c *MultiDimensionalCorrelator) Correlate(events []*UnifiedEvent) []Correlation {
    // Step 1: Build correlation candidates across dimensions
    candidates := c.buildCandidates(events)
    
    // Step 2: Score each candidate across all dimensions
    scored := c.scoreCorrelations(candidates)
    
    // Step 3: Build causal chains using K8s knowledge
    withCausality := c.analyzeCausality(scored)
    
    // Step 4: Merge overlapping correlations
    merged := c.mergeCorrelations(withCausality)
    
    return merged
}
```

## Real-World Example: Pod OOMKilled Cascade

Let's trace how the correlator handles a real scenario where a memory leak causes cascading failures.

### Input Events

```yaml
Event 1:
  ID: e001
  Timestamp: 14:32:00
  Type: metrics
  Source: prometheus
  K8sContext:
    Name: api-server-7f9c5-x2kj
    Namespace: production
    WorkloadName: api-server
    NodeName: node-us-east-1a-3
  Metrics:
    memory_usage: 1.8Gi (90% of limit)

Event 2:
  ID: e002
  Timestamp: 14:32:30
  Type: kernel
  Source: ebpf
  K8sContext:
    Name: api-server-7f9c5-x2kj
  Kernel:
    Syscall: mmap
    ReturnCode: -12 (ENOMEM)
    PID: 3847

Event 3:
  ID: e003
  Timestamp: 14:32:31
  Type: kubernetes
  Source: k8s-api
  K8sContext:
    Name: api-server-7f9c5-x2kj
    Namespace: production
  Kubernetes:
    EventType: Warning
    Reason: OOMKilling
    Message: "Container api exceeded memory limit"

Event 4:
  ID: e004  
  Timestamp: 14:32:32
  Type: network
  Source: ebpf
  K8sContext:
    Name: frontend-8a7d2-m5pq
    Namespace: production
  Network:
    DestIP: 10.0.1.15 (api-server service)
    Event: connection_refused

Event 5:
  ID: e005
  Timestamp: 14:32:33
  Type: kubernetes
  Source: k8s-api
  K8sContext:
    Name: frontend-8a7d2-m5pq
  Kubernetes:
    EventType: Warning
    Reason: BackOff
    Message: "Back-off restarting failed container"

Event 6:
  ID: e006
  Timestamp: 14:32:35
  Type: kubernetes
  Source: k8s-api
  K8sContext:
    Name: api-server-7f9c5-y8mk (new pod)
    WorkloadName: api-server
  Kubernetes:
    EventType: Normal
    Reason: Created
    Message: "Created container api"
```

### Step 1: Build Correlation Candidates

```go
func (c *MultiDimensionalCorrelator) buildCandidates(events []*UnifiedEvent) []CorrelationCandidate {
    candidates := []CorrelationCandidate{}
    
    // Temporal Dimension: Events within 5 minute window
    temporalGroups := c.groupByTimeWindow(events, 5*time.Minute)
    // Result: All 6 events in one temporal group
    
    // Spatial Dimension: Same node/namespace/cluster
    spatialGroups := c.groupBySpatialProximity(events)
    // Result: e001,e002,e003 on same pod; e004,e005 on same namespace
    
    // Ownership Dimension: Same workload hierarchy  
    ownershipGroups := c.groupByOwnership(events)
    // Result: e001,e003,e006 belong to api-server deployment
    
    // Dependency Dimension: Service dependencies
    dependencyGroups := c.groupByDependencies(events)
    // Result: e004 depends on api-server service (e001,e003,e006)
    
    // Causal Dimension: Cause-effect relationships
    causalGroups := c.groupByCausality(events)
    // Result: e002→e003 (OOM kill), e003→e004 (connection refused)
    
    return candidates
}
```

### Step 2: Score Correlations

```go
type CorrelationScore struct {
    Dimensions map[string]float64
    Overall    float64
    Evidence   []string
}

func (c *MultiDimensionalCorrelator) scoreCorrelations(candidates []CorrelationCandidate) []ScoredCorrelation {
    scored := []ScoredCorrelation{}
    
    // Score the main correlation group
    mainGroup := []string{"e001", "e002", "e003", "e004", "e005", "e006"}
    
    score := CorrelationScore{
        Dimensions: map[string]float64{
            "temporal":   0.95,  // All within 35 seconds
            "spatial":    0.80,  // Same namespace, partial node overlap
            "ownership":  0.70,  // Mix of api-server and frontend
            "dependency": 0.90,  // Clear service dependency
            "causal":     0.95,  // Strong cause-effect chain
        },
    }
    
    // Calculate overall score (weighted average)
    score.Overall = 0.86
    
    // Gather evidence
    score.Evidence = []string{
        "Memory usage at 90% before OOM",
        "Kernel ENOMEM syscall failure",
        "OOMKilling event from kubelet",
        "Dependent service connection failures",
        "Pod recreation after OOM",
    }
    
    return scored
}
```

### Step 3: Build Causal Chain

```go
func (c *MultiDimensionalCorrelator) analyzeCausality(scored []ScoredCorrelation) []Correlation {
    // Use K8s domain knowledge to build causal chain
    
    causalChain := []CausalLink{
        {
            CauseEvent:  "e001",
            EffectEvent: "e002",
            LinkType:    "resource_exhaustion",
            Confidence:  0.95,
            Evidence:    "Memory at 90% → mmap ENOMEM failure",
        },
        {
            CauseEvent:  "e002", 
            EffectEvent: "e003",
            LinkType:    "triggers",
            Confidence:  0.99,
            Evidence:    "Kernel ENOMEM → Kubelet OOMKill",
        },
        {
            CauseEvent:  "e003",
            EffectEvent: "e004",
            LinkType:    "causes",
            Confidence:  0.90,
            Evidence:    "Pod killed → Service endpoint removed → Connection refused",
        },
        {
            CauseEvent:  "e004",
            EffectEvent: "e005",
            LinkType:    "triggers",
            Confidence:  0.85,
            Evidence:    "Connection failures → Container restart",
        },
        {
            CauseEvent:  "e003",
            EffectEvent: "e006",
            LinkType:    "remediation",
            Confidence:  0.95,
            Evidence:    "Pod killed → ReplicaSet creates replacement",
        },
    }
    
    return withCausalChains
}
```

### Step 4: Final Correlation Output

```go
correlation := Correlation{
    ID: "corr-7a8b9c",
    Type: "resource_cascade_failure",
    
    Events: []EventReference{
        {EventID: "e001", Role: "precursor", Impact: "warning"},
        {EventID: "e002", Role: "trigger", Impact: "critical"},
        {EventID: "e003", Role: "root_cause", Impact: "critical"},
        {EventID: "e004", Role: "cascade", Impact: "high"},
        {EventID: "e005", Role: "cascade", Impact: "medium"},
        {EventID: "e006", Role: "recovery", Impact: "info"},
    },
    
    Dimensions: map[string]DimensionScore{
        "temporal": {
            Dimension: "temporal",
            Score: 0.95,
            Evidence: ["35 second cascade", "Clear sequence"],
        },
        "spatial": {
            Dimension: "spatial", 
            Score: 0.80,
            Evidence: ["Same namespace", "Cross-pod impact"],
        },
        "ownership": {
            Dimension: "ownership",
            Score: 0.70,
            Evidence: ["api-server workload", "Affects dependent services"],
        },
        "dependency": {
            Dimension: "dependency",
            Score: 0.90,
            Evidence: ["frontend→api-server dependency", "Service topology"],
        },
        "causal": {
            Dimension: "causal",
            Score: 0.95,
            Evidence: ["Memory→OOM→Kill→Unavailable→Restart chain"],
        },
    },
    
    OverallScore: 0.86,
    
    CommonFactors: []CommonFactor{
        {
            Type: "namespace",
            Value: "production", 
            Events: []string{"e001", "e003", "e004", "e005", "e006"},
            Significance: 0.9,
        },
        {
            Type: "service",
            Value: "api-server",
            Events: []string{"e001", "e002", "e003", "e006"},
            Significance: 0.95,
        },
    },
    
    CausalChain: causalChain, // From step 3
    
    RootEvents: []EventReference{
        {EventID: "e001", Confidence: 0.85}, // Memory pressure
        {EventID: "e003", Confidence: 0.95}, // OOM Kill
    },
    
    ImpactScope: &ImpactScope{
        DirectlyAffected: []ResourceRef{
            {Kind: "Pod", Name: "api-server-7f9c5-x2kj"},
        },
        IndirectlyAffected: []ResourceRef{
            {Kind: "Pod", Name: "frontend-8a7d2-m5pq"},
            {Kind: "Service", Name: "api-server"},
        },
        BusinessImpact: "High - Customer-facing API unavailable",
        EstimatedDuration: 35 * time.Second,
    },
}
```

## Why This Correlation is Powerful

### 1. **Multi-Dimensional Confidence**
Instead of just "these events happened near each other", we know:
- They're causally related (0.95 confidence)
- They share dependencies (0.90 confidence)  
- They're in the same failure domain (0.80 confidence)

### 2. **Root Cause Identification**
The engine identifies TWO root causes:
- **e001**: Early warning (memory pressure)
- **e003**: Actual trigger (OOM kill)

### 3. **Impact Analysis**
Automatically determines:
- Direct impact: api-server pod
- Cascade impact: frontend pods
- Business impact: Customer-facing service disruption

### 4. **K8s-Aware Causality**
Uses K8s knowledge:
- Knows OOM leads to pod termination
- Knows pod termination removes endpoints
- Knows missing endpoints cause connection failures
- Knows ReplicaSet will create replacement

## Correlation Engine Components

### 1. K8s Resource Graph
```go
type K8sResourceGraph struct {
    nodes map[string]*ResourceNode
    edges map[string][]*ResourceEdge
}

func (g *K8sResourceGraph) GetDependents(resource ResourceRef) []ResourceRef {
    // Returns all resources that depend on this one
    // Used for impact analysis
}
```

### 2. Pattern Library
```go
var K8sFailurePatterns = []FailurePattern{
    {
        Name: "OOM_CASCADE",
        Indicators: []string{"memory_pressure", "ENOMEM", "OOMKilling"},
        Sequence: []string{"memory_high", "allocation_fail", "kill", "unavailable"},
        Impact: "service_disruption",
    },
    // ... more patterns
}
```

### 3. Causal Rule Engine
```go
var CausalRules = []CausalRule{
    {
        If: "kernel.syscall == 'mmap' && kernel.return_code == -12",
        Then: "memory_allocation_failure",
        Confidence: 0.99,
    },
    {
        If: "memory_allocation_failure && k8s.container == $container",
        Then: "oom_kill_imminent",
        Confidence: 0.95,
    },
    // ... more rules
}
```

## Benefits Over Traditional Correlation

1. **Contextual Understanding**: Knows WHY events are related, not just that they occurred together
2. **Predictive Power**: Can identify early warnings (e001) not just failures
3. **Impact Mapping**: Automatically traces cascade effects through service dependencies
4. **Root Cause Clarity**: Distinguishes symptoms from causes
5. **Confidence Scoring**: Multi-dimensional confidence instead of binary correlation

This correlation engine turns raw events into actionable insights by leveraging K8s's rich context and domain knowledge!