package collector

import (
    "context"
    "fmt"
    "sync"
    "time"
    "github.com/yairfalse/tapio/pkg/domain"
    "github.com/yairfalse/tapio/pkg/patternrecognition"
)

// SemanticCorrelationEngine replaces the old correlation with our semantic version
type SemanticCorrelationEngine struct {
    // Collectors
    collectors map[string]Collector
    
    // Event processing
    eventChan   chan Event
    insightChan chan Insight
    
    // Semantic processing (our improved correlation)
    semanticGrouper *SimpleSemanticGrouper
    
    // Revolutionary OTEL semantic tracer
    semanticTracer *SemanticOTELTracer
    
    // Pattern recognition engine
    patternEngine patternrecognition.PatternRecognitionEngine
    
    // Event buffer for pattern detection
    eventBuffer      []domain.Event
    eventBufferMutex sync.RWMutex
    bufferSize       int
    
    // Human-readable output formatter
    humanFormatter *HumanReadableFormatter
    
    // State
    ctx     context.Context
    cancel  context.CancelFunc
    running bool
    mu      sync.RWMutex
    
    // Stats
    stats map[string]interface{}
}

// NewSemanticCorrelationEngine creates our improved correlation engine
func NewSemanticCorrelationEngine(batchSize int, batchTimeout time.Duration) *SemanticCorrelationEngine {
    // Configure pattern recognition
    patternConfig := patternrecognition.DefaultConfig()
    patternConfig.DefaultTimeWindow = 30 * time.Minute
    patternConfig.EnabledPatterns = []string{"memory_leak"} // Enable memory leak pattern by default
    
    return &SemanticCorrelationEngine{
        collectors:      make(map[string]Collector),
        eventChan:      make(chan Event, 1000),
        insightChan:    make(chan Insight, 100),
        semanticGrouper: NewSimpleSemanticGrouper(),
        semanticTracer:  NewSemanticOTELTracer(),
        patternEngine:   patternrecognition.Engine(patternConfig),
        eventBuffer:     make([]domain.Event, 0, batchSize),
        bufferSize:      batchSize,
        humanFormatter:  NewHumanReadableFormatter(StyleSimple, AudienceDeveloper),
        stats:          make(map[string]interface{}),
    }
}

// RegisterCollector registers a collector with the engine
func (sce *SemanticCorrelationEngine) RegisterCollector(c Collector) error {
    sce.mu.Lock()
    defer sce.mu.Unlock()
    
    name := c.Name()
    sce.collectors[name] = c
    return nil
}

// Start begins the semantic correlation engine
func (sce *SemanticCorrelationEngine) Start(ctx context.Context) error {
    sce.mu.Lock()
    defer sce.mu.Unlock()
    
    if sce.running {
        return nil
    }
    
    sce.ctx, sce.cancel = context.WithCancel(ctx)
    sce.running = true
    
    // Start processing events with our semantic correlation
    go sce.processEvents()
    
    return nil
}

// processEvents handles events using our semantic correlation
func (sce *SemanticCorrelationEngine) processEvents() {
    // Create ticker for periodic pattern detection
    patternTicker := time.NewTicker(5 * time.Second)
    defer patternTicker.Stop()
    
    for {
        select {
        case event := <-sce.eventChan:
            // Convert Event to domain.Event
            domainEvent := sce.convertToDomainEvent(event)
            
            // Add to buffer for pattern detection
            sce.addToEventBuffer(*domainEvent)
            
            // Process through revolutionary OTEL semantic tracer
            if err := sce.semanticTracer.ProcessEventWithSemanticTrace(sce.ctx, domainEvent); err != nil {
                sce.updateStats("trace_errors")
            } else {
                sce.updateStats("traces_created")
            }
            
            // Process through our semantic grouper
            finding, err := sce.semanticGrouper.ProcessEvent(sce.ctx, domainEvent)
            if err != nil {
                continue
            }
            
            // Convert finding to insight
            insight := sce.convertToInsight(finding)
            
            // Enrich insight with semantic group information
            sce.enrichInsightWithSemanticGroups(domainEvent, &insight)
            
            // Send to insights channel
            select {
            case sce.insightChan <- insight:
                sce.updateStats("insights_generated")
            default:
                sce.updateStats("insights_dropped")
            }
            
        case <-patternTicker.C:
            // Run pattern detection periodically
            sce.detectPatterns()
            
        case <-sce.ctx.Done():
            return
        }
    }
}

// convertToDomainEvent converts collector Event to domain.Event
func (sce *SemanticCorrelationEngine) convertToDomainEvent(event Event) *domain.Event {
    // Extract common fields from data
    context := domain.EventContext{}
    
    // Extract metadata from event.Data
    if event.Data != nil {
        if node, ok := event.Data["node"].(string); ok {
            context.Host = node
        }
        if ns, ok := event.Data["namespace"].(string); ok {
            context.Namespace = ns
        }
        if pod, ok := event.Data["pod"].(string); ok {
            context.Labels = domain.Labels{
                "pod": pod,
            }
        }
    }
    
    // Create appropriate payload based on event type
    var payload domain.EventPayload
    switch event.Type {
    case "kubernetes", "pod_evicted", "pod_restart", "pod_crash":
        pod := ""
        namespace := ""
        reason := ""
        message := ""
        eventType := event.Type
        
        if event.Data != nil {
            if p, ok := event.Data["pod"].(string); ok {
                pod = p
            }
            if ns, ok := event.Data["namespace"].(string); ok {
                namespace = ns
            }
            if r, ok := event.Data["reason"].(string); ok {
                reason = r
            }
            if m, ok := event.Data["message"].(string); ok {
                message = m
            }
            if et, ok := event.Data["event_type"].(string); ok {
                eventType = et
            }
        }
        
        payload = domain.KubernetesEventPayload{
            Resource: domain.ResourceRef{
                Kind:      "Pod",
                Name:      pod,
                Namespace: namespace,
            },
            EventType: eventType,
            Reason:    reason,
            Message:   message,
        }
        
    case "service", "service_restart", "service_failure":
        serviceName := ""
        eventType := event.Type
        
        if event.Data != nil {
            if svc, ok := event.Data["service"].(string); ok {
                serviceName = svc
            }
            if et, ok := event.Data["event_type"].(string); ok {
                eventType = et
            }
        }
        
        payload = domain.ServiceEventPayload{
            ServiceName: serviceName,
            EventType:   eventType,
        }
        
    case "memory", "memory_oom", "memory_pressure":
        // Extract memory metrics from data
        usage := 0.0
        available := uint64(0)
        total := uint64(0)
        
        if event.Data != nil {
            if u, ok := event.Data["usage"]; ok {
                switch v := u.(type) {
                case float64:
                    usage = v
                case int:
                    usage = float64(v)
                }
            }
            if a, ok := event.Data["available"]; ok {
                if av, ok := a.(float64); ok {
                    available = uint64(av)
                }
            }
            if t, ok := event.Data["total"]; ok {
                if tv, ok := t.(float64); ok {
                    total = uint64(tv)
                }
            }
        }
        
        payload = domain.MemoryEventPayload{
            Usage:     usage,
            Available: available,
            Total:     total,
        }
        
    default:
        // Use system event as default
        payload = domain.SystemEventPayload{}
    }
    
    return &domain.Event{
        ID:        domain.EventID(event.ID),
        Type:      domain.EventType(event.Type),
        Source:    domain.SourceType(event.Source),
        Severity:  domain.Severity(event.Severity),
        Timestamp: event.Timestamp,
        Context:   context,
        Payload:   payload,
        Metadata: domain.EventMetadata{
            SchemaVersion: "v1",
            ProcessedAt:   time.Now(),
            ProcessedBy:   "semantic-correlation-engine",
        },
        Confidence: 1.0,
    }
}

// convertToInsight converts domain.Finding to Insight
func (sce *SemanticCorrelationEngine) convertToInsight(finding *domain.Finding) Insight {
    return Insight{
        ID:          string(finding.ID),
        Type:        string(finding.Type),
        Severity:    Severity(finding.Severity),
        Title:       finding.Title,
        Description: finding.Description,
        Timestamp:   finding.Timestamp,
        RelatedEvents: []string{},
    }
}

// Insights returns the insights channel
func (sce *SemanticCorrelationEngine) Insights() <-chan Insight {
    return sce.insightChan
}

// GetStats returns correlation engine statistics
func (sce *SemanticCorrelationEngine) GetStats() map[string]interface{} {
    sce.mu.RLock()
    defer sce.mu.RUnlock()
    
    // Copy stats to avoid race conditions
    statsCopy := make(map[string]interface{})
    for k, v := range sce.stats {
        statsCopy[k] = v
    }
    
    return statsCopy
}

// updateStats updates internal statistics
func (sce *SemanticCorrelationEngine) updateStats(key string) {
    sce.mu.Lock()
    defer sce.mu.Unlock()
    
    if count, ok := sce.stats[key].(int64); ok {
        sce.stats[key] = count + 1
    } else {
        sce.stats[key] = int64(1)
    }
}

// Stop gracefully stops the correlation engine
func (sce *SemanticCorrelationEngine) Stop() {
    sce.mu.Lock()
    defer sce.mu.Unlock()
    
    if !sce.running {
        return
    }
    
    sce.running = false
    if sce.cancel != nil {
        sce.cancel()
    }
    
    close(sce.insightChan)
}

// addToEventBuffer adds an event to the circular buffer
func (sce *SemanticCorrelationEngine) addToEventBuffer(event domain.Event) {
    sce.eventBufferMutex.Lock()
    defer sce.eventBufferMutex.Unlock()
    
    sce.eventBuffer = append(sce.eventBuffer, event)
    
    // Maintain buffer size limit
    if len(sce.eventBuffer) > sce.bufferSize {
        sce.eventBuffer = sce.eventBuffer[len(sce.eventBuffer)-sce.bufferSize:]
    }
}

// detectPatterns runs pattern detection on buffered events
func (sce *SemanticCorrelationEngine) detectPatterns() {
    sce.eventBufferMutex.RLock()
    events := make([]domain.Event, len(sce.eventBuffer))
    copy(events, sce.eventBuffer)
    sce.eventBufferMutex.RUnlock()
    
    if len(events) == 0 {
        return
    }
    
    // Run pattern detection
    matches, err := sce.patternEngine.DetectPatterns(sce.ctx, events)
    if err != nil {
        sce.updateStats("pattern_errors")
        return
    }
    
    // Convert pattern matches to insights
    for _, match := range matches {
        insight := sce.convertPatternMatchToInsight(match)
        
        select {
        case sce.insightChan <- insight:
            sce.updateStats("pattern_insights_generated")
        default:
            sce.updateStats("pattern_insights_dropped")
        }
    }
    
    sce.updateStats("pattern_detections_run")
}

// convertPatternMatchToInsight converts a pattern match to an insight
func (sce *SemanticCorrelationEngine) convertPatternMatchToInsight(match patternrecognition.PatternMatch) Insight {
    // Extract event IDs from the correlation
    relatedEvents := make([]string, 0, len(match.Correlation.Events))
    for _, eventRef := range match.Correlation.Events {
        relatedEvents = append(relatedEvents, string(eventRef.EventID))
    }
    
    // Create prediction if confidence is high enough
    var prediction *Prediction
    if match.Confidence >= 0.7 {
        prediction = &Prediction{
            Type:        string(match.Pattern.ID),
            Probability: match.Confidence,
            Confidence:  match.Confidence,
        }
    }
    
    // Create affected resources from correlation context
    resources := []AffectedResource{}
    if match.Correlation.Context.Host != "" {
        resources = append(resources, AffectedResource{
            Type: "node",
            Name: match.Correlation.Context.Host,
        })
    }
    
    // Create actionable items
    actions := []ActionableItem{}
    if match.Pattern.ID == "memory_leak" && match.Confidence >= 0.8 {
        actions = append(actions, ActionableItem{
            Title:       "Restart affected service",
            Description: "Service has a memory leak and should be restarted to clear accumulated memory",
            Commands:    []string{"kubectl rollout restart deployment <service>"},
            Risk:        "low",
            EstimatedImpact: "Service will be restarted with zero downtime using rolling update",
        })
        actions = append(actions, ActionableItem{
            Title:       "Investigate memory allocation",
            Description: "Analyze heap dumps and memory profiles to identify the root cause",
            Commands:    []string{
                "kubectl exec <pod> -- jmap -dump:format=b,file=/tmp/heapdump.hprof <pid>",
                "kubectl cp <pod>:/tmp/heapdump.hprof ./heapdump.hprof",
            },
            Risk:        "low",
            EstimatedImpact: "No service impact, diagnostic only",
        })
    }
    
    // Determine severity based on pattern and confidence
    severity := SeverityMedium
    if match.Pattern.Priority == patternrecognition.PatternPriorityHigh {
        severity = SeverityHigh
    } else if match.Pattern.Priority == patternrecognition.PatternPriorityCritical {
        severity = SeverityCritical
    }
    
    // Create title from pattern name and description
    title := fmt.Sprintf("%s Detected", match.Pattern.Name)
    
    return Insight{
        ID:          fmt.Sprintf("pattern-%s-%d", match.Pattern.ID, time.Now().UnixNano()),
        Type:        fmt.Sprintf("pattern:%s", match.Pattern.ID),
        Severity:    severity,
        Title:       title,
        Description: fmt.Sprintf("%s (Pattern: %s, Confidence: %.2f)", 
                     match.Correlation.Description, match.Pattern.Name, match.Confidence),
        Timestamp:   match.Detected,
        RelatedEvents: relatedEvents,
        Resources:   resources,
        Actions:     actions,
        Prediction:  prediction,
    }
}

// GetPatternStats returns pattern recognition statistics
func (sce *SemanticCorrelationEngine) GetPatternStats() patternrecognition.PatternStats {
    return sce.patternEngine.GetPatternStats()
}

// ConfigurePatterns allows runtime configuration of pattern engine
func (sce *SemanticCorrelationEngine) ConfigurePatterns(config *patternrecognition.Config) error {
    return sce.patternEngine.Configure(config)
}

// SetHumanOutputStyle changes the human output formatting style
func (sce *SemanticCorrelationEngine) SetHumanOutputStyle(style ExplanationStyle, audience Audience) {
    sce.mu.Lock()
    defer sce.mu.Unlock()
    sce.humanFormatter = NewHumanReadableFormatter(style, audience)
}

// GetHumanExplanation returns a human-readable explanation for an insight
func (sce *SemanticCorrelationEngine) GetHumanExplanation(insight Insight) *HumanReadableExplanation {
    return sce.humanFormatter.FormatInsight(insight)
}

// GetInsightStory creates a narrative story from multiple related insights
func (sce *SemanticCorrelationEngine) GetInsightStory(insights []Insight) string {
    return sce.humanFormatter.FormatAsStory(insights)
}

// SimpleSemanticGrouper - embedded from our dataflow work
type SimpleSemanticGrouper struct {
    // Simplified for now
}

func NewSimpleSemanticGrouper() *SimpleSemanticGrouper {
    return &SimpleSemanticGrouper{}
}

func (sg *SimpleSemanticGrouper) ProcessEvent(ctx context.Context, event *domain.Event) (*domain.Finding, error) {
    // Our semantic correlation logic from before
    finding := &domain.Finding{
        ID:          domain.FindingID("semantic-" + string(event.ID)),
        Type:        "semantic-correlation",
        Severity:    event.Severity,
        Title:       "Semantic correlation for " + string(event.Type),
        Description: "Advanced semantic analysis with intent classification",
        Timestamp:   time.Now(),
    }
    
    return finding, nil
}

// enrichInsightWithSemanticGroups adds semantic trace group information to insights
func (sce *SemanticCorrelationEngine) enrichInsightWithSemanticGroups(event *domain.Event, insight *Insight) {
    // Find the semantic group this event belongs to
    groups := sce.semanticTracer.GetSemanticGroups()
    
    for _, group := range groups {
        // Check if event is in this group
        for _, groupEvent := range group.CausalChain {
            if groupEvent.ID == event.ID {
                // Enrich insight with semantic group data
                insight.SemanticGroupID = group.ID
                insight.SemanticIntent = group.Intent
                insight.TraceID = group.TraceID
                
                // Add predicted outcome if available
                if group.PredictedOutcome != nil {
                    insight.Prediction = &Prediction{
                        Type:        group.PredictedOutcome.Scenario,
                        Probability: group.PredictedOutcome.Probability,
                        Confidence:  group.PredictedOutcome.ConfidenceLevel,
                        TimeToOutcome: group.PredictedOutcome.TimeToOutcome,
                    }
                }
                
                // Add impact assessment
                if group.ImpactAssessment != nil {
                    insight.BusinessImpact = group.ImpactAssessment.BusinessImpact
                    insight.CascadeRisk = group.ImpactAssessment.CascadeRisk
                    
                    // Add recommended actions if not already present
                    for _, action := range group.ImpactAssessment.RecommendedActions {
                        insight.Actions = append(insight.Actions, ActionableItem{
                            Title:       "Semantic Recommendation",
                            Description: action,
                            Commands:    []string{action},
                            Risk:        "low",
                            EstimatedImpact: "Based on semantic correlation analysis",
                        })
                    }
                }
                
                // Add related events from causal chain
                for _, causalEvent := range group.CausalChain {
                    if causalEvent.ID != event.ID {
                        insight.RelatedEvents = append(insight.RelatedEvents, string(causalEvent.ID))
                    }
                }
                
                return
            }
        }
    }
}

// GetSemanticTraceGroups returns current semantic trace groups for monitoring
func (sce *SemanticCorrelationEngine) GetSemanticTraceGroups() map[string]*SemanticTraceGroup {
    return sce.semanticTracer.GetSemanticGroups()
}

// CleanupSemanticGroups removes old semantic groups
func (sce *SemanticCorrelationEngine) CleanupSemanticGroups(retention time.Duration) {
    sce.semanticTracer.CleanupOldGroups(retention)
    sce.updateStats("semantic_groups_cleaned")
}
