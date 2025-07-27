# Multi-Dimensional OTEL Event Flow

## Visual Flow Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                    Multi-Dimensional UnifiedEvent                │
│                                                                  │
│  ┌─────────────┐  ┌──────────────┐  ┌─────────────────┐        │
│  │Trace Context│  │   Semantic   │  │  Layer Data     │        │
│  │ • TraceID   │  │ • Intent     │  │ • Kernel        │        │
│  │ • SpanID    │  │ • Category   │  │ • Network       │        │
│  │ • Parent    │  │ • Narrative  │  │ • Application   │        │
│  │ • Baggage   │  │ • Concepts   │  │ • Kubernetes    │        │
│  └─────────────┘  └──────────────┘  └─────────────────┘        │
│                                                                  │
│  ┌─────────────┐  ┌──────────────┐  ┌─────────────────┐        │
│  │   Impact    │  │ Correlation  │  │    Entity       │        │
│  │ • Severity  │  │ • Pattern    │  │ • Type          │        │
│  │ • Business  │  │ • CausalChain│  │ • Name          │        │
│  │ • SLO       │  │ • GroupID    │  │ • Namespace     │        │
│  └─────────────┘  └──────────────┘  └─────────────────┘        │
│                                                                  │
│  ┌─────────────┐  ┌──────────────┐                             │
│  │  Temporal   │  │  Behavioral  │                             │
│  │ • Patterns  │  │ • Anomalies  │                             │
│  │ • Frequency │  │ • Baseline   │                             │
│  └─────────────┘  └──────────────┘                             │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
        ┌────────────────────────────────────────┐
        │         Analytics Engine Pipeline      │
        │                                        │
        │  1. Validation: All dimensions valid? │
        │  2. Enrichment: Add missing dims      │
        │  3. Correlation: Multi-dim matching    │
        │  4. Analytics: Cross-dim analysis     │
        └────────────────────┬───────────────────┘
                             │
                             ▼
        ┌────────────────────────────────────────┐
        │    Multi-Dimensional Correlation       │
        └────────────────────────────────────────┘
                             │
        ┌────────────────────┼────────────────────┐
        ▼                    ▼                    ▼
┌──────────────┐   ┌──────────────┐   ┌──────────────┐
│ K8s Native   │   │  Temporal    │   │  Sequence    │
│              │   │              │   │              │
│ Uses:        │   │ Uses:        │   │ Uses:        │
│ • Entity     │   │ • Temporal   │   │ • Trace      │
│ • K8s Layer  │   │ • Behavioral │   │ • Correlation│
│ • Trace      │   │ • Impact     │   │ • Semantic   │
└──────────────┘   └──────────────┘   └──────────────┘
        │                    │                    │
        └────────────────────┴────────────────────┘
                             │
                             ▼
        ┌────────────────────────────────────────┐
        │         Correlation Insights           │
        │                                        │
        │  Combines ALL dimensions to tell:      │
        │  • WHAT happened (Layer data)         │
        │  • WHY it matters (Impact, Semantic)  │
        │  • HOW it relates (Trace, Correlation)│
        │  • WHEN patterns occur (Temporal)     │
        │  • WHO is affected (Entity, Impact)   │
        └────────────────────┬───────────────────┘
                             │
                             ▼
        ┌────────────────────────────────────────┐
        │      Enhanced Analytics Finding        │
        │                                        │
        │  • Multi-dim description              │
        │  • Business impact narrative          │
        │  • Causal chain visualization         │
        │  • Semantic grouping                   │
        └────────────────────────────────────────┘
```

## Example Multi-Dimensional Correlation

### Input Event
```json
{
  "id": "evt-123",
  "trace_context": {
    "trace_id": "deploy-456",
    "span_id": "oom-789",
    "parent_span_id": "scale-456"
  },
  "semantic": {
    "intent": "resource_exhaustion",
    "narrative": "API pod OOM during scale-up"
  },
  "kubernetes": {
    "event_type": "pod_oom_killed",
    "object": "api-server-v2"
  },
  "impact": {
    "business_impact": 0.9,
    "customer_facing": true
  }
}
```

### Correlation Process

1. **K8s Dimension Check**
   - Find owner: Deployment "api-server"
   - Find selector matches: 3 other pods
   - Check resource quotas: exceeded

2. **Trace Dimension Check**
   - Parent span: "scale-456" (scaling operation)
   - Root trace: "deploy-456" (deployment)
   - Find 15 events in same trace

3. **Semantic Dimension Check**
   - Intent pattern: "resource_exhaustion" → common after "scaling_operation"
   - Concepts: Links to "memory-management", "pod-lifecycle"

4. **Impact Dimension Check**
   - Business impact 0.9 → Critical
   - Customer-facing → Urgent
   - SLO impact → Alert needed

### Output: Multi-Dimensional Finding
```json
{
  "id": "finding-789",
  "pattern": "deployment-resource-cascade",
  "confidence": 0.95,
  "description": "Deployment scaling triggered memory exhaustion cascade affecting customer-facing API service (90% business impact)",
  "narrative": "During deployment 'deploy-456', automatic scaling increased replicas from 2 to 5. The new pods exceeded memory quotas, causing OOM kills. This pattern has occurred 3 times in the past week.",
  "dimensions_used": ["trace", "semantic", "kubernetes", "impact", "temporal"],
  "recommended_actions": [
    "Increase memory limits to 2Gi",
    "Implement pod disruption budget",
    "Add memory-based HPA metrics"
  ]
}
```

## Benefits of Multi-Dimensional Correlation

1. **Richer Context**: Every correlation uses multiple perspectives
2. **Higher Accuracy**: Multi-dimensional matching reduces false positives
3. **Better Narratives**: Combines technical and business context
4. **Causal Understanding**: Trace hierarchy shows true causality
5. **Actionable Insights**: Impact dimension prioritizes response

## Implementation in Code

```go
// The adapter leverages all dimensions
func (a *AnalyticsCorrelationAdapter) correlateMultiDimensional(event *UnifiedEvent) {
    // 1. Extract all dimensional data
    dimensions := extractDimensions(event)
    
    // 2. Score correlation across dimensions
    scores := map[string]float64{
        "trace": scoreTraceDimension(event.TraceContext),
        "semantic": scoreSemanticDimension(event.Semantic),
        "layer": scoreLayerDimension(event),
        "impact": scoreImpactDimension(event.Impact),
        "temporal": scoreTemporalDimension(event.Temporal),
    }
    
    // 3. Find multi-dimensional patterns
    patterns := findCrossDimensionalPatterns(dimensions, scores)
    
    // 4. Generate rich insights
    insight := generateMultiDimensionalInsight(patterns, event)
}
```

This multi-dimensional approach is what makes UnifiedEvent powerful - it's not just an event with OTEL context, it's a complete observability signal that tells the full story.