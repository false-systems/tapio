# Enhanced Data Structures for K8s Context-Rich Observability

## Design Principles

1. **Data structures are neutral and rich** - no "story" terminology
2. **Multi-dimensional correlation data** - facts, not narratives  
3. **Presentation layer handles narrative construction** - GUI/CLI responsibility
4. **OTEL-compatible with K8s extensions** - industry standard + our secret sauce

## Core Data Structure Enhancements

### 1. Enhanced UnifiedEvent

```go
// UnifiedEvent with rich K8s context - the foundation
type UnifiedEvent struct {
    // Core Identity
    ID        string    `json:"id"`
    Timestamp time.Time `json:"timestamp"`
    Type      EventType `json:"type"`
    Source    string    `json:"source"`
    
    // OTEL Trace Context
    TraceContext *TraceContext `json:"trace_context,omitempty"`
    
    // Multi-dimensional Contexts
    EntityContext    *EntityContext    `json:"entity,omitempty"`        // What this is about
    K8sContext       *K8sContext       `json:"k8s_context,omitempty"`   // Rich K8s data
    ResourceContext  *ResourceContext  `json:"resource,omitempty"`      // Resource state
    OperationalContext *OperationalContext `json:"operational,omitempty"` // How it's operating
    
    // Layer-specific data (existing)
    Kernel      *KernelData      `json:"kernel,omitempty"`
    Network     *NetworkData     `json:"network,omitempty"`
    Application *ApplicationData `json:"application,omitempty"`
    Metrics     *MetricsData     `json:"metrics,omitempty"`
    
    // Analysis Results
    Correlations []CorrelationRef `json:"correlations,omitempty"`
    Patterns     []PatternMatch   `json:"patterns,omitempty"`
    Anomalies    []AnomalyRef     `json:"anomalies,omitempty"`
    
    // Metadata
    ProcessingMetadata *ProcessingMetadata `json:"processing_metadata,omitempty"`
}
```

### 2. K8sContext - Rich Kubernetes Data

```go
// K8sContext contains comprehensive K8s information
type K8sContext struct {
    // Resource Identity
    APIVersion string         `json:"api_version"`
    Kind       string         `json:"kind"`
    UID        string         `json:"uid"`
    Name       string         `json:"name"`
    Namespace  string         `json:"namespace,omitempty"`
    
    // Ownership & Management
    OwnerReferences []OwnerReference `json:"owner_references,omitempty"`
    Controller      *ControllerRef   `json:"controller,omitempty"`
    ManagedFields   []ManagedField   `json:"managed_fields,omitempty"`
    
    // Resource Metadata
    Labels          map[string]string `json:"labels,omitempty"`
    Annotations     map[string]string `json:"annotations,omitempty"`
    Generation      int64             `json:"generation,omitempty"`
    ResourceVersion string            `json:"resource_version,omitempty"`
    
    // Relationships
    Selectors       map[string]string   `json:"selectors,omitempty"`
    Dependencies    []ResourceDependency `json:"dependencies,omitempty"`
    Consumers       []ResourceRef        `json:"consumers,omitempty"`
    
    // Placement & Topology
    NodeName        string              `json:"node_name,omitempty"`
    Zone            string              `json:"zone,omitempty"`
    Region          string              `json:"region,omitempty"`
    ClusterName     string              `json:"cluster_name,omitempty"`
    
    // Workload Context
    WorkloadKind    string              `json:"workload_kind,omitempty"`    // Deployment, StatefulSet
    WorkloadName    string              `json:"workload_name,omitempty"`
    ReplicaIndex    *int                `json:"replica_index,omitempty"`    // For StatefulSets
    
    // State Information
    Phase           string              `json:"phase,omitempty"`
    Conditions      []ConditionSnapshot `json:"conditions,omitempty"`
    
    // Resource Specifications
    ResourceRequests ResourceList       `json:"resource_requests,omitempty"`
    ResourceLimits   ResourceList       `json:"resource_limits,omitempty"`
    QoSClass         string             `json:"qos_class,omitempty"`
}

// ResourceDependency represents a dependency relationship
type ResourceDependency struct {
    Kind         string `json:"kind"`
    Name         string `json:"name"`
    Namespace    string `json:"namespace,omitempty"`
    Type         string `json:"type"` // "config", "storage", "network", "service"
    Required     bool   `json:"required"`
    Status       string `json:"status,omitempty"` // "satisfied", "missing", "error"
}

// ConditionSnapshot captures condition state at event time
type ConditionSnapshot struct {
    Type               string    `json:"type"`
    Status             string    `json:"status"`
    LastTransitionTime time.Time `json:"last_transition_time"`
    Reason             string    `json:"reason,omitempty"`
    Message            string    `json:"message,omitempty"`
}
```

### 3. ResourceContext - Desired vs Actual State

```go
// ResourceContext captures resource state and intent
type ResourceContext struct {
    // Desired State (from spec)
    DesiredState    *ResourceState `json:"desired_state,omitempty"`
    
    // Actual State (from status)  
    ActualState     *ResourceState `json:"actual_state,omitempty"`
    
    // Divergence Analysis
    Divergences     []StateDivergence `json:"divergences,omitempty"`
    ReconcileStatus string            `json:"reconcile_status,omitempty"`
    
    // Historical Context
    PreviousState   *ResourceState    `json:"previous_state,omitempty"`
    StateTransition *StateTransition  `json:"state_transition,omitempty"`
    UpdateHistory   []UpdateRecord    `json:"update_history,omitempty"`
}

// ResourceState represents a state snapshot
type ResourceState struct {
    Replicas        *ReplicaState     `json:"replicas,omitempty"`
    ContainerStates []ContainerState  `json:"container_states,omitempty"`
    VolumeStates    []VolumeState     `json:"volume_states,omitempty"`
    NetworkState    *NetworkState     `json:"network_state,omitempty"`
    Custom          interface{}       `json:"custom,omitempty"` // For CRDs
}

// StateDivergence represents a specific divergence
type StateDivergence struct {
    Field          string      `json:"field"`
    DesiredValue   interface{} `json:"desired_value"`
    ActualValue    interface{} `json:"actual_value"`
    Reason         string      `json:"reason,omitempty"`
    Impact         string      `json:"impact,omitempty"`
    Since          time.Time   `json:"since"`
}
```

### 4. OperationalContext - Runtime Behavior

```go
// OperationalContext captures operational characteristics
type OperationalContext struct {
    // Performance Metrics
    ResourceUtilization *ResourceUtilization `json:"resource_utilization,omitempty"`
    LatencyProfile      *LatencyProfile      `json:"latency_profile,omitempty"`
    ThroughputMetrics   *ThroughputMetrics   `json:"throughput_metrics,omitempty"`
    
    // Reliability Indicators
    HealthStatus        string               `json:"health_status"`
    AvailabilityMetrics *AvailabilityMetrics `json:"availability_metrics,omitempty"`
    ErrorMetrics        *ErrorMetrics        `json:"error_metrics,omitempty"`
    
    // Behavioral Patterns
    ScalingBehavior     *ScalingBehavior     `json:"scaling_behavior,omitempty"`
    RestartPatterns     *RestartPatterns     `json:"restart_patterns,omitempty"`
    TrafficPatterns     *TrafficPatterns     `json:"traffic_patterns,omitempty"`
    
    // Operational Events
    RecentEvents        []OperationalEvent   `json:"recent_events,omitempty"`
    ActiveAlerts        []AlertReference     `json:"active_alerts,omitempty"`
}

// ResourceUtilization tracks resource usage
type ResourceUtilization struct {
    CPU    *ResourceMetric `json:"cpu,omitempty"`
    Memory *ResourceMetric `json:"memory,omitempty"`
    Disk   *ResourceMetric `json:"disk,omitempty"`
    Network *NetworkMetric `json:"network,omitempty"`
}

type ResourceMetric struct {
    Current    float64   `json:"current"`
    Average    float64   `json:"average"`
    Peak       float64   `json:"peak"`
    Trend      string    `json:"trend"` // "increasing", "stable", "decreasing"
    Percentile map[int]float64 `json:"percentile,omitempty"` // 50, 90, 95, 99
}
```

### 5. Correlation Data Structures

```go
// Correlation represents multi-dimensional event correlation
type Correlation struct {
    ID              string            `json:"id"`
    Type            CorrelationType   `json:"type"`
    Events          []EventReference  `json:"events"`
    
    // Multi-dimensional correlation scores
    Dimensions      map[string]DimensionScore `json:"dimensions"`
    OverallScore    float64                   `json:"overall_score"`
    
    // Correlation metadata
    TimeWindow      TimeWindow        `json:"time_window"`
    CommonFactors   []CommonFactor    `json:"common_factors"`
    CausalChain     []CausalLink      `json:"causal_chain,omitempty"`
    
    // Analysis results
    RootEvents      []EventReference  `json:"root_events,omitempty"`
    ImpactScope     *ImpactScope      `json:"impact_scope,omitempty"`
    
    Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// DimensionScore represents correlation strength in one dimension
type DimensionScore struct {
    Dimension   string  `json:"dimension"` // "temporal", "spatial", "causal", "semantic"
    Score       float64 `json:"score"`     // 0.0 - 1.0
    Evidence    []string `json:"evidence"`
    Confidence  float64 `json:"confidence"`
}

// CommonFactor represents shared characteristics
type CommonFactor struct {
    Type        string      `json:"type"`  // "owner", "node", "service", "config"
    Value       interface{} `json:"value"`
    Events      []string    `json:"events"` // Event IDs sharing this factor
    Significance float64    `json:"significance"`
}

// CausalLink represents causality between events
type CausalLink struct {
    CauseEvent   string  `json:"cause_event"`
    EffectEvent  string  `json:"effect_event"`
    LinkType     string  `json:"link_type"` // "triggers", "causes", "correlates"
    Confidence   float64 `json:"confidence"`
    TimeDelta    time.Duration `json:"time_delta"`
}
```

### 6. Pattern Data Structures

```go
// Pattern represents a detected behavioral pattern
type Pattern struct {
    ID              string          `json:"id"`
    Type            string          `json:"type"`
    Name            string          `json:"name"`
    
    // Pattern characteristics
    Signature       PatternSignature `json:"signature"`
    Frequency       float64          `json:"frequency"`
    Periodicity     *time.Duration   `json:"periodicity,omitempty"`
    
    // Matching events
    Matches         []PatternMatch   `json:"matches"`
    Coverage        float64          `json:"coverage"` // % of events explained
    
    // Pattern metadata
    FirstSeen       time.Time        `json:"first_seen"`
    LastSeen        time.Time        `json:"last_seen"`
    OccurrenceCount int              `json:"occurrence_count"`
    
    // Predictive capability
    Predictability  float64          `json:"predictability"`
    NextOccurrence  *time.Time       `json:"next_occurrence,omitempty"`
}

// PatternSignature defines pattern matching rules
type PatternSignature struct {
    EventSequence   []EventCriteria  `json:"event_sequence"`
    TimeConstraints []TimeConstraint `json:"time_constraints"`
    RequiredContext []ContextCriteria `json:"required_context"`
}
```

### 7. Enriched Analysis Results

```go
// AnalysisResult contains all analysis outputs for an event
type AnalysisResult struct {
    EventID         string           `json:"event_id"`
    Timestamp       time.Time        `json:"timestamp"`
    
    // Multi-dimensional analysis
    CorrelationResults []CorrelationResult `json:"correlation_results"`
    PatternMatches     []PatternMatch      `json:"pattern_matches"`
    AnomalyScores      []AnomalyScore      `json:"anomaly_scores"`
    
    // Context-based insights
    ContextInsights    []ContextInsight    `json:"context_insights"`
    ImpactAssessment   *ImpactAssessment   `json:"impact_assessment,omitempty"`
    
    // Recommendations (data, not narrative)
    Recommendations    []Recommendation    `json:"recommendations,omitempty"`
    
    // Processing metadata
    AnalysisDuration   time.Duration       `json:"analysis_duration"`
    ContextDepth       string              `json:"context_depth"`
    Confidence         float64             `json:"confidence"`
}

// ContextInsight represents an insight derived from context
type ContextInsight struct {
    Type        string      `json:"type"`
    Category    string      `json:"category"`
    Observation interface{} `json:"observation"`
    Evidence    []string    `json:"evidence"`
    Confidence  float64     `json:"confidence"`
    Timestamp   time.Time   `json:"timestamp"`
}

// Recommendation represents actionable data (not narrative)
type Recommendation struct {
    Type        string                 `json:"type"`
    Action      string                 `json:"action"`
    Target      ResourceRef            `json:"target"`
    Parameters  map[string]interface{} `json:"parameters,omitempty"`
    Priority    float64                `json:"priority"`
    Impact      string                 `json:"impact"`
    Risks       []string               `json:"risks,omitempty"`
}
```

## Intelligence Pipeline Updates

### 1. Context Extraction Stage

```go
// ContextExtractor enriches events with K8s context
type ContextExtractor struct {
    k8sClient       kubernetes.Interface
    contextCache    cache.Store
    extractionRules []ExtractionRule
}

func (e *ContextExtractor) Process(ctx context.Context, event *UnifiedEvent) error {
    // Extract K8s context
    k8sContext, err := e.extractK8sContext(event)
    if err != nil {
        return fmt.Errorf("k8s context extraction failed: %w", err)
    }
    event.K8sContext = k8sContext
    
    // Extract resource context (desired vs actual)
    resourceContext, err := e.extractResourceContext(event)
    if err != nil {
        return fmt.Errorf("resource context extraction failed: %w", err)
    }
    event.ResourceContext = resourceContext
    
    // Extract operational context
    operationalContext, err := e.extractOperationalContext(event)
    if err != nil {
        return fmt.Errorf("operational context extraction failed: %w", err)
    }
    event.OperationalContext = operationalContext
    
    return nil
}
```

### 2. Multi-Dimensional Correlator

```go
// MultiDimensionalCorrelator performs correlation across dimensions
type MultiDimensionalCorrelator struct {
    dimensions      []CorrelationDimension
    scorer          DimensionalScorer
    causalAnalyzer  CausalAnalyzer
}

func (c *MultiDimensionalCorrelator) Correlate(events []*UnifiedEvent) ([]Correlation, error) {
    correlations := []Correlation{}
    
    // Group events by different dimensions
    temporalGroups := c.groupByTime(events)
    spatialGroups := c.groupBySpatial(events)  // node, zone, cluster
    ownershipGroups := c.groupByOwnership(events)
    semanticGroups := c.groupBySemantic(events)
    
    // Find correlations in each dimension
    for _, dimension := range c.dimensions {
        dimCorrelations := dimension.FindCorrelations(events)
        correlations = append(correlations, dimCorrelations...)
    }
    
    // Merge and score multi-dimensional correlations
    merged := c.mergeCorrelations(correlations)
    
    // Analyze causal relationships
    for i := range merged {
        merged[i].CausalChain = c.causalAnalyzer.AnalyzeCausality(merged[i].Events)
    }
    
    return merged, nil
}
```

### 3. Pattern Detection Engine

```go
// PatternDetector identifies patterns in event streams
type PatternDetector struct {
    patternLibrary  []PatternDefinition
    learningEngine  PatternLearner
    predictor       PatternPredictor
}

func (d *PatternDetector) DetectPatterns(events []*UnifiedEvent) ([]Pattern, error) {
    detected := []Pattern{}
    
    // Check against known patterns
    for _, patternDef := range d.patternLibrary {
        if matches := d.matchPattern(events, patternDef); len(matches) > 0 {
            pattern := Pattern{
                ID:        generatePatternID(),
                Type:      patternDef.Type,
                Name:      patternDef.Name,
                Signature: patternDef.Signature,
                Matches:   matches,
            }
            detected = append(detected, pattern)
        }
    }
    
    // Discover new patterns
    if learned := d.learningEngine.DiscoverPatterns(events); len(learned) > 0 {
        detected = append(detected, learned...)
    }
    
    // Add predictive capabilities
    for i := range detected {
        d.predictor.AddPredictions(&detected[i])
    }
    
    return detected, nil
}
```

## Benefits of This Approach

1. **Clean Architecture**: Data structures focus on rich information, not presentation
2. **Flexibility**: UI/CLI can build different narratives from same data
3. **Testability**: Pure data structures are easier to test
4. **Performance**: No narrative building overhead in core pipeline
5. **Extensibility**: Easy to add new dimensions without changing narrative logic

## Presentation Layer (GUI/CLI)

The narrative building happens here:

```go
// NarrativeBuilder is part of presentation layer, not core
type NarrativeBuilder struct {
    templates TemplateLibrary
    language  LanguageProcessor
}

// BuildNarrative takes rich data and creates human narrative
func (n *NarrativeBuilder) BuildNarrative(
    event *UnifiedEvent, 
    correlations []Correlation,
    patterns []Pattern,
) string {
    // This is where "story" happens - at presentation time
    // Using all the rich data structures we've built
}
```

This separation gives us the best of both worlds - incredibly rich data structures that enable powerful analysis, with flexible narrative generation at the presentation layer!