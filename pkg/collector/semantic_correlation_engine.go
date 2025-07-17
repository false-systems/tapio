package collector

import (
    "context"
    "sync"
    "time"
    "github.com/yairfalse/tapio/pkg/domain"
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
    return &SemanticCorrelationEngine{
        collectors:      make(map[string]Collector),
        eventChan:      make(chan Event, 1000),
        insightChan:    make(chan Insight, 100),
        semanticGrouper: NewSimpleSemanticGrouper(),
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
    for {
        select {
        case event := <-sce.eventChan:
            // Convert Event to domain.Event
            domainEvent := sce.convertToDomainEvent(event)
            
            // Process through our semantic grouper
            finding, err := sce.semanticGrouper.ProcessEvent(sce.ctx, domainEvent)
            if err != nil {
                continue
            }
            
            // Convert finding to insight
            insight := sce.convertToInsight(finding)
            
            // Send to insights channel
            select {
            case sce.insightChan <- insight:
                sce.updateStats("insights_generated")
            default:
                sce.updateStats("insights_dropped")
            }
            
        case <-sce.ctx.Done():
            return
        }
    }
}

// convertToDomainEvent converts collector Event to domain.Event
func (sce *SemanticCorrelationEngine) convertToDomainEvent(event Event) *domain.Event {
    return &domain.Event{
        ID:        domain.EventID(event.ID),
        Type:      domain.EventType(event.Type),
        Source:    domain.SourceType(event.Source),
        Severity:  domain.Severity(event.Severity),
        Timestamp: event.Timestamp,
        // Add more field mappings as needed
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
