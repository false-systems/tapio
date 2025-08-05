# Analysis Engine Integration Guide

## Integration Approach: Extending the Existing API Service

### Why Add to Existing API Service

1. **Single Entry Point**: Users interact with one API for all correlation and analysis needs
2. **Shared Infrastructure**: Reuse existing middleware, authentication, OTEL instrumentation
3. **Context Preservation**: Easy to pass correlation results to analysis engine
4. **Consistent Experience**: Same error handling, response formats, and conventions

## Integration Architecture

```
┌─────────────┐     ┌─────────────┐     ┌──────────────┐
│   API       │────▶│  Aggregator │────▶│   Analysis   │
│  Service    │     │             │     │   Engine     │
└─────────────┘     └─────────────┘     └──────────────┘
       │                    │                    │
       └────────────────────┴────────────────────┘
                           │
                      ┌─────────┐
                      │  Neo4j  │
                      └─────────┘
```

## Implementation Steps

### 1. Add Analysis Engine to API Service

```go
// In your API service initialization
type Server struct {
    aggregator     *aggregator.Aggregator
    analysisEngine *analysis.Engine  // Add this
    neo4jClient    neo4j.Driver
    logger         *zap.Logger
}

func NewServer(config *Config) (*Server, error) {
    // ... existing code ...
    
    analysisEngine := analysis.NewEngine(logger)
    
    return &Server{
        aggregator:     agg,
        analysisEngine: analysisEngine,
        neo4jClient:    driver,
        logger:         logger,
    }, nil
}
```

### 2. Add Analysis Endpoints

```go
// handlers/analysis.go
package handlers

import (
    "github.com/yairfalse/tapio/pkg/intelligence/analysis"
    "github.com/yairfalse/tapio/pkg/intelligence/aggregator"
)

// AnalyzeEvent handles POST /api/v1/analysis/event
func (s *Server) AnalyzeEvent(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    
    var req AnalysisRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    // Get event from Neo4j or request
    event, err := s.getEvent(ctx, req.EventID)
    if err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }
    
    // Get findings from aggregator or Neo4j
    findings, err := s.getFindings(ctx, req.EventID)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // Run analysis
    result := s.analysisEngine.AnalyzeFindings(ctx, findings, event)
    
    // Store result in Neo4j for historical queries
    if err := s.storeAnalysisResult(ctx, result); err != nil {
        s.logger.Error("Failed to store analysis result", zap.Error(err))
    }
    
    // Return response
    json.NewEncoder(w).Encode(result)
}

// DetectPatterns handles GET /api/v1/patterns/detect
func (s *Server) DetectPatterns(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    eventID := r.URL.Query().Get("event_id")
    
    // Get recent events from Neo4j
    events, err := s.getRecentEvents(ctx, eventID, 30*time.Minute)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // Detect patterns
    matcher := analysis.NewPatternMatcher()
    patterns := matcher.DetectPatterns(events)
    
    json.NewEncoder(w).Encode(patterns)
}

// CalculateConfidence handles POST /api/v1/confidence/calculate
func (s *Server) CalculateConfidence(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    
    var scoreCtx analysis.ScoreContext
    if err := json.NewDecoder(r.Body).Decode(&scoreCtx); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    scorer := analysis.NewConfidenceScorer(analysis.DefaultScoringConfig())
    confidence := scorer.CalculateConfidence(scoreCtx)
    
    json.NewEncoder(w).Encode(map[string]float64{
        "confidence": confidence,
    })
}

// GetAnalysisHistory handles GET /api/v1/analysis/history
func (s *Server) GetAnalysisHistory(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    resource := r.URL.Query().Get("resource")
    timeframe := r.URL.Query().Get("timeframe")
    
    duration, err := time.ParseDuration(timeframe)
    if err != nil {
        http.Error(w, "Invalid timeframe", http.StatusBadRequest)
        return
    }
    
    // Query Neo4j for historical analyses
    analyses, err := s.getHistoricalAnalyses(ctx, resource, duration)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    json.NewEncoder(w).Encode(analyses)
}
```

### 3. Integration with Aggregator

The aggregator already produces findings that the analysis engine needs:

```go
// In your correlation processing flow
func (s *Server) ProcessCorrelation(ctx context.Context, event *domain.UnifiedEvent) error {
    // Step 1: Correlators produce findings
    correlations, err := s.runCorrelators(ctx, event)
    if err != nil {
        return err
    }
    
    // Step 2: Aggregator combines findings
    findings := s.aggregator.Aggregate(ctx, correlations)
    
    // Step 3: Analysis engine analyzes findings
    analysis := s.analysisEngine.AnalyzeFindings(ctx, findings, event)
    
    // Step 4: Store everything in Neo4j
    if err := s.storeResults(ctx, event, findings, analysis); err != nil {
        return err
    }
    
    // Step 5: Send notifications if critical
    if analysis.Confidence > 0.8 && len(analysis.Insights) > 0 {
        s.notifyInsights(ctx, analysis)
    }
    
    return nil
}
```

### 4. Neo4j Schema for Analysis Storage

```cypher
// Analysis result node
CREATE (a:Analysis {
    id: $analysisId,
    eventId: $eventId,
    timestamp: datetime(),
    confidence: $confidence,
    quality: $quality,
    summary: $summary
})

// Link to event
MATCH (e:Event {id: $eventId})
CREATE (e)-[:HAS_ANALYSIS]->(a)

// Store patterns
FOREACH (pattern IN $patterns |
    CREATE (p:Pattern {
        name: pattern.name,
        confidence: pattern.confidence
    })
    CREATE (a)-[:DETECTED_PATTERN]->(p)
)

// Store insights
FOREACH (insight IN $insights |
    CREATE (i:Insight {
        type: insight.type,
        title: insight.title,
        message: insight.message,
        severity: insight.severity
    })
    CREATE (a)-[:GENERATED_INSIGHT]->(i)
)
```

## Benefits of This Approach

1. **Seamless Integration**: Analysis runs automatically on every correlation
2. **Historical Intelligence**: Neo4j stores all analyses for trend detection
3. **Real-time Insights**: Critical findings trigger immediate notifications
4. **Unified API**: One endpoint for all intelligence features
5. **Performance**: Reuse existing connections and context

## Next Steps

1. Add the analysis endpoints to your existing routes
2. Create Neo4j queries for storing/retrieving analysis results
3. Add WebSocket support for real-time analysis updates
4. Implement caching for frequently accessed patterns

The analysis engine is designed to plug right into your existing flow!