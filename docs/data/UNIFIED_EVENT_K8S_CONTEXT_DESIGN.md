# UnifiedEvent K8s Context Enhancement Design

## Overview

This document outlines how to integrate the rich K8s context extraction capabilities into our UnifiedEvent structure and intelligence pipeline, building on top of OTEL for a superior observability experience.

## Enhanced UnifiedEvent Structure

### Current vs Enhanced Design

```go
// CURRENT: Basic K8s data
type UnifiedEvent struct {
    // ... existing fields ...
    Kubernetes  *KubernetesData  `json:"kubernetes,omitempty"`
}

// ENHANCED: Rich K8s context extraction
type UnifiedEvent struct {
    // Core Identity (existing)
    ID        string    `json:"id"`
    Timestamp time.Time `json:"timestamp"`
    Type      EventType `json:"type"`
    Source    string    `json:"source"`
    
    // OTEL Trace Context (existing)
    TraceContext *TraceContext `json:"trace_context,omitempty"`
    
    // ENHANCED: Multi-dimensional K8s Context
    K8sContext *K8sContextBundle `json:"k8s_context,omitempty"`
    
    // Existing layer-specific data
    Kernel      *KernelData      `json:"kernel,omitempty"`
    Network     *NetworkData     `json:"network,omitempty"`
    Application *ApplicationData `json:"application,omitempty"`
    Metrics     *MetricsData     `json:"metrics,omitempty"`
    
    // ENHANCED: Extracted semantic meaning
    ExtractedContext *ExtractedContext `json:"extracted_context,omitempty"`
    
    // Existing analysis contexts
    Impact      *ImpactContext      `json:"impact,omitempty"`
    Correlation *CorrelationContext `json:"correlation,omitempty"`
}
```

### K8sContextBundle Design

```go
// K8sContextBundle contains all extractable K8s context
type K8sContextBundle struct {
    // Identity & Ownership
    Identity    *K8sIdentityContext    `json:"identity"`
    Ownership   *K8sOwnershipContext   `json:"ownership"`
    
    // Relationships
    Topology    *K8sTopologyContext    `json:"topology"`
    
    // Intent vs Reality
    Declarative *K8sDeclarativeContext `json:"declarative"`
    
    // Historical Context
    Evolution   *K8sEvolutionContext   `json:"evolution"`
    
    // Business Mapping
    Business    *K8sBusinessContext    `json:"business"`
    
    // Extraction metadata
    ExtractionTime  time.Time `json:"extraction_time"`
    ExtractionDepth string    `json:"extraction_depth"` // shallow, medium, deep
    ContextPoints   int       `json:"context_points"`   // How many data points extracted
}

// K8sIdentityContext - Multi-layer identity
type K8sIdentityContext struct {
    // K8s identity
    Name            string            `json:"name"`
    Namespace       string            `json:"namespace"`
    UID             string            `json:"uid"`
    ResourceVersion string            `json:"resource_version"`
    Generation      int64             `json:"generation"`
    
    // Rich labels and annotations
    Labels          map[string]string `json:"labels"`
    Annotations     map[string]string `json:"annotations"`
    
    // Derived identities
    WorkloadName    string `json:"workload_name"`    // From owner chain
    ApplicationName string `json:"application_name"` // From labels
    ServiceName     string `json:"service_name"`     // From service selector
}

// K8sOwnershipContext - Full ownership chain
type K8sOwnershipContext struct {
    OwnerChain      []OwnerInfo `json:"owner_chain"`
    WorkloadType    string      `json:"workload_type"`    // Deployment, StatefulSet, etc
    WorkloadDetails interface{} `json:"workload_details"` // Type-specific details
    
    // Controller context
    ControllerName  string `json:"controller_name"`
    ControllerKind  string `json:"controller_kind"`
}

// K8sTopologyContext - Relationship graph
type K8sTopologyContext struct {
    // Node placement
    Node            NodeContext `json:"node"`
    Zone            string      `json:"zone"`
    Region          string      `json:"region"`
    
    // Network relationships  
    Services        []ServiceBinding    `json:"services"`
    Endpoints       []EndpointInfo      `json:"endpoints"`
    NetworkPolicies []NetworkPolicyInfo `json:"network_policies"`
    
    // Configuration relationships
    ConfigMaps      []ConfigMapBinding  `json:"config_maps"`
    Secrets         []SecretBinding     `json:"secrets"`
    Volumes         []VolumeBinding     `json:"volumes"`
    
    // Dependency graph
    DependsOn       []ResourceRef `json:"depends_on"`
    UsedBy          []ResourceRef `json:"used_by"`
}

// K8sDeclarativeContext - Intent vs Reality
type K8sDeclarativeContext struct {
    // Desired state (from spec)
    DesiredState    ResourceSpec `json:"desired_state"`
    
    // Actual state (from status)
    ActualState     ResourceStatus `json:"actual_state"`
    
    // Reconciliation state
    Reconciling     bool          `json:"reconciling"`
    Divergence      []Divergence  `json:"divergence"`
    LastTransition  time.Time     `json:"last_transition"`
    
    // Conditions with full history
    Conditions      []ConditionHistory `json:"conditions"`
}

// K8sEvolutionContext - Historical patterns
type K8sEvolutionContext struct {
    // Lifecycle events
    CreatedAt       time.Time          `json:"created_at"`
    UpdatedAt       time.Time          `json:"updated_at"`
    UpdateCount     int                `json:"update_count"`
    
    // State transitions
    StateHistory    []StateTransition  `json:"state_history"`
    
    // Restart/failure patterns
    RestartCount    int                `json:"restart_count"`
    RestartHistory  []RestartEvent     `json:"restart_history"`
    FailurePattern  string             `json:"failure_pattern"`
    
    // Scaling history
    ScalingHistory  []ScalingEvent     `json:"scaling_history"`
    
    // Version history
    VersionHistory  []VersionChange    `json:"version_history"`
}

// K8sBusinessContext - Business mapping
type K8sBusinessContext struct {
    // From standard labels
    Application     string            `json:"application"`
    Version         string            `json:"version"`
    Component       string            `json:"component"`
    PartOf          string            `json:"part_of"`
    ManagedBy       string            `json:"managed_by"`
    
    // From annotations or ConfigMaps
    Team            string            `json:"team"`
    Environment     string            `json:"environment"`
    Criticality     string            `json:"criticality"`
    SLA             string            `json:"sla"`
    CostCenter      string            `json:"cost_center"`
    
    // Business relationships
    CustomerFacing  bool              `json:"customer_facing"`
    RevenueImpact   bool              `json:"revenue_impact"`
    Dependencies    []string          `json:"dependencies"`
    
    // Custom business metadata
    CustomMetadata  map[string]string `json:"custom_metadata"`
}
```

### ExtractedContext - Semantic Meaning

```go
// ExtractedContext contains interpreted meaning from K8s context
type ExtractedContext struct {
    // Patterns detected
    Patterns        []DetectedPattern  `json:"patterns"`
    
    // Anomalies found
    Anomalies       []ContextAnomaly   `json:"anomalies"`
    
    // Risk assessment
    Risks           []IdentifiedRisk   `json:"risks"`
    
    // Semantic classification
    WorkloadProfile WorkloadProfile    `json:"workload_profile"`
    
    // Story hints
    StoryHints      []StoryHint        `json:"story_hints"`
}

type DetectedPattern struct {
    Type        string    `json:"type"`        // "restart-loop", "scaling-thrash"
    Confidence  float64   `json:"confidence"`
    Evidence    []string  `json:"evidence"`
    FirstSeen   time.Time `json:"first_seen"`
    Occurrences int       `json:"occurrences"`
}

type WorkloadProfile struct {
    Type            string  `json:"type"`     // "stateless-api", "batch-job", "database"
    ScalingBehavior string  `json:"scaling"`  // "horizontal", "vertical", "static"
    ResourceProfile string  `json:"resource"` // "cpu-intensive", "memory-intensive"
    TrafficPattern  string  `json:"traffic"`  // "steady", "spiky", "periodic"
    Reliability     float64 `json:"reliability"`
}
```

## Intelligence Pipeline Enhancement

### 1. Context Extraction Stage

```go
// New pipeline stage for K8s context extraction
type K8sContextExtractor struct {
    k8sClient     kubernetes.Interface
    cache         *ContextCache
    extractors    map[string]Extractor
    maxDepth      ExtractionDepth
}

func (e *K8sContextExtractor) Process(event *domain.UnifiedEvent) error {
    // Skip if not K8s-related
    if !e.isK8sRelated(event) {
        return nil
    }
    
    // Determine extraction depth based on event importance
    depth := e.determineExtractionDepth(event)
    
    // Extract context in parallel
    ctx := &K8sContextBundle{
        ExtractionTime:  time.Now(),
        ExtractionDepth: depth.String(),
    }
    
    g := errgroup.Group{}
    
    // Identity extraction (always)
    g.Go(func() error {
        ctx.Identity = e.extractIdentity(event)
        return nil
    })
    
    // Ownership extraction (always)
    g.Go(func() error {
        ctx.Ownership = e.extractOwnership(event)
        return nil
    })
    
    // Topology extraction (medium+)
    if depth >= Medium {
        g.Go(func() error {
            ctx.Topology = e.extractTopology(event)
            return nil
        })
    }
    
    // Declarative extraction (medium+)
    if depth >= Medium {
        g.Go(func() error {
            ctx.Declarative = e.extractDeclarative(event)
            return nil
        })
    }
    
    // Evolution extraction (deep)
    if depth >= Deep {
        g.Go(func() error {
            ctx.Evolution = e.extractEvolution(event)
            return nil
        })
    }
    
    // Business extraction (always, it's lightweight)
    g.Go(func() error {
        ctx.Business = e.extractBusiness(event)
        return nil
    })
    
    if err := g.Wait(); err != nil {
        return fmt.Errorf("context extraction failed: %w", err)
    }
    
    // Count extracted context points
    ctx.ContextPoints = e.countContextPoints(ctx)
    
    // Attach to event
    event.K8sContext = ctx
    
    // Extract semantic meaning
    event.ExtractedContext = e.extractSemanticMeaning(ctx)
    
    return nil
}
```

### 2. Enhanced Correlation with K8s Context

```go
// Enhanced correlator using K8s context
type K8sAwareCorrelator struct {
    baseCorrelator    interfaces.CorrelationEngine
    contextAnalyzer   *K8sContextAnalyzer
}

func (c *K8sAwareCorrelator) Correlate(events []*domain.UnifiedEvent) (*Correlation, error) {
    // Group events by K8s context dimensions
    groups := c.groupByK8sContext(events)
    
    // For each group, build correlation using context
    correlations := []Correlation{}
    
    for _, group := range groups {
        corr := Correlation{
            ID: generateCorrelationID(),
        }
        
        // Use ownership chain for vertical correlation
        if owner := c.findCommonOwner(group); owner != nil {
            corr.OwnershipChain = c.buildOwnershipStory(owner, group)
        }
        
        // Use topology for horizontal correlation  
        if topology := c.findTopologyPattern(group); topology != nil {
            corr.TopologyPattern = c.buildTopologyStory(topology, group)
        }
        
        // Use declarative context for intent correlation
        if intent := c.findIntentDivergence(group); intent != nil {
            corr.IntentStory = c.buildIntentStory(intent, group)
        }
        
        // Use evolution for temporal correlation
        if pattern := c.findEvolutionPattern(group); pattern != nil {
            corr.EvolutionStory = c.buildEvolutionStory(pattern, group)
        }
        
        // Calculate multi-dimensional correlation score
        corr.Confidence = c.calculateContextCorrelation(corr)
        
        correlations = append(correlations, corr)
    }
    
    return c.mergeCorrelations(correlations), nil
}
```

### 3. Context-Aware Story Building

```go
// Story builder that leverages K8s context
type K8sContextStoryBuilder struct {
    templates    map[string]StoryTemplate
    contextRules map[string]ContextRule
}

func (b *K8sContextStoryBuilder) BuildStory(events []*domain.UnifiedEvent) *Story {
    // Extract primary event with richest context
    primary := b.findPrimaryEvent(events)
    
    story := &Story{
        ID:        generateStoryID(),
        Title:     b.generateContextAwareTitle(primary),
        Timestamp: primary.Timestamp,
    }
    
    // Build narrative using K8s context
    if primary.K8sContext != nil {
        // Use ownership for hierarchical narrative
        story.OwnershipNarrative = b.buildOwnershipNarrative(
            primary.K8sContext.Ownership,
        )
        
        // Use declarative for intent narrative
        story.IntentNarrative = b.buildIntentNarrative(
            primary.K8sContext.Declarative,
        )
        
        // Use topology for impact narrative
        story.ImpactNarrative = b.buildImpactNarrative(
            primary.K8sContext.Topology,
        )
        
        // Use business context for priority
        story.BusinessContext = b.buildBusinessNarrative(
            primary.K8sContext.Business,
        )
        
        // Use evolution for pattern narrative
        if primary.K8sContext.Evolution != nil {
            story.PatternNarrative = b.buildPatternNarrative(
                primary.K8sContext.Evolution,
            )
        }
    }
    
    // Add OTEL correlation if available
    if primary.TraceContext != nil {
        story.TraceNarrative = b.buildTraceNarrative(
            primary.TraceContext,
            events,
        )
    }
    
    // Generate recommendations based on context
    story.Recommendations = b.generateContextAwareRecommendations(
        primary,
        events,
    )
    
    return story
}
```

## OTEL Enhancement Integration

### 1. Context Propagation via OTEL

```go
// Propagate K8s context through OTEL trace
type K8sContextPropagator struct {
    propagator propagation.TextMapPropagator
}

func (p *K8sContextPropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
    // Extract K8s context from span
    span := trace.SpanFromContext(ctx)
    
    // Add K8s identity as span attributes
    span.SetAttributes(
        attribute.String("k8s.namespace", k8sCtx.Identity.Namespace),
        attribute.String("k8s.workload", k8sCtx.Identity.WorkloadName),
        attribute.String("k8s.pod", k8sCtx.Identity.Name),
    )
    
    // Add business context
    span.SetAttributes(
        attribute.String("business.app", k8sCtx.Business.Application),
        attribute.String("business.team", k8sCtx.Business.Team),
        attribute.String("business.env", k8sCtx.Business.Environment),
    )
    
    // Propagate via baggage for context flow
    baggage.ContextWithBaggage(ctx,
        baggage.String("k8s.context.id", k8sCtx.Identity.UID),
    )
}
```

### 2. Enhanced OTEL Metrics with K8s Context

```go
// Metrics that include K8s context dimensions
func (c *Collector) RecordMetricWithK8sContext(event *UnifiedEvent) {
    labels := []attribute.KeyValue{
        // Standard OTEL labels
        attribute.String("service.name", event.Source),
        
        // K8s context labels
        attribute.String("k8s.namespace", event.K8sContext.Identity.Namespace),
        attribute.String("k8s.workload.type", event.K8sContext.Ownership.WorkloadType),
        attribute.String("k8s.workload.name", event.K8sContext.Identity.WorkloadName),
        
        // Business context labels
        attribute.String("business.app", event.K8sContext.Business.Application),
        attribute.String("business.criticality", event.K8sContext.Business.Criticality),
    }
    
    // Record with rich context
    c.histogram.Record(context.Background(), value, labels...)
}
```

## Implementation Plan

### Phase 1: UnifiedEvent Enhancement (Week 1-2)
1. Implement K8sContextBundle structure
2. Add ExtractedContext for semantic meaning
3. Update event builders and converters
4. Maintain backward compatibility

### Phase 2: Context Extraction (Week 3-4)
1. Build K8sContextExtractor
2. Implement parallel extraction strategies
3. Add caching layer for performance
4. Create extraction depth strategies

### Phase 3: Pipeline Integration (Week 5-6)
1. Add context extraction stage to pipeline
2. Enhance correlation with K8s awareness
3. Update story builder for context
4. Integrate with existing stages

### Phase 4: OTEL Enhancement (Week 7-8)
1. Add K8s context propagation
2. Enhance metrics with K8s dimensions
3. Update trace attributes
4. Ensure OTEL compliance

## Performance Considerations

### 1. Lazy Loading Strategy
```go
type LazyK8sContext struct {
    // Always loaded (lightweight)
    identity   *K8sIdentityContext
    
    // Loaded on demand
    ownership  func() *K8sOwnershipContext
    topology   func() *K8sTopologyContext
    evolution  func() *K8sEvolutionContext
}
```

### 2. Context Caching
```go
type ContextCache struct {
    // LRU cache for static context
    staticCache *lru.Cache
    
    // TTL cache for dynamic context
    dynamicCache *ttl.Cache
    
    // Pre-computed indexes
    ownershipIndex map[string][]string
    topologyIndex  map[string][]string
}
```

### 3. Extraction Throttling
```go
type ExtractionThrottler struct {
    maxConcurrent   int
    maxPerSecond    int
    priorityQueue   PriorityQueue
}
```

## Benefits

1. **100x richer context** in every event
2. **Multi-dimensional correlation** using K8s relationships
3. **Intent-aware stories** showing desired vs actual
4. **Business context** in technical events
5. **OTEL compliance** with K8s awareness

## Conclusion

By enhancing UnifiedEvent with rich K8s context extraction, we transform our intelligence pipeline from simple event correlation to multi-dimensional story construction. This design leverages K8s's declarative nature to provide context that traditional observability tools miss, while maintaining OTEL compatibility for industry-standard integration.

This is our path to delivering the "K8s whisperer" that tells coherent stories from chaos.