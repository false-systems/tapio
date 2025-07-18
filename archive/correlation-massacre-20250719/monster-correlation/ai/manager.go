package correlation

import (
    "context"
    "fmt"
    "sync"
    "time"

    "github.com/yairfalse/tapio/pkg/domain"
    "github.com/yairfalse/tapio/pkg/patternrecognition"
)

// CollectionManager provides AI pattern recognition for collected events
type CollectionManager struct {
    patternManager *patternrecognition.Manager
    eventBus       chan domain.Event
    findingChan    chan domain.Finding
    ctx            context.Context
    cancel         context.CancelFunc
    wg             sync.WaitGroup
    config         Config
}

// Config for the collection manager
type Config struct {
    EventBufferSize          int
    PatternDetectionInterval time.Duration
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
    return Config{
        EventBufferSize:          1000,
        PatternDetectionInterval: 5 * time.Second,
    }
}

// NewCollectionManager creates a collection manager with AI pattern recognition
func NewCollectionManager(config Config) *CollectionManager {
    ctx, cancel := context.WithCancel(context.Background())
    
    patternConfig := patternrecognition.DefaultConfig()
    patternManager := patternrecognition.NewManager(patternConfig)
    
    // Register the memory leak pattern
    memoryLeakPattern := patternrecognition.NewMemoryLeakPattern()
    if err := patternManager.RegisterPattern(memoryLeakPattern); err != nil {
        fmt.Printf("Warning: Failed to register memory leak pattern: %v\n", err)
    }
    
    return &CollectionManager{
        patternManager: patternManager,
        eventBus:      make(chan domain.Event, config.EventBufferSize),
        findingChan:   make(chan domain.Finding, 100),
        ctx:           ctx,
        cancel:        cancel,
        config:        config,
    }
}

// Start begins AI pattern recognition processing
func (cm *CollectionManager) Start() error {
    cm.wg.Add(1)
    go cm.processEvents()
    return nil
}

// ProcessEvents processes a batch of events through AI pattern recognition
func (cm *CollectionManager) ProcessEvents(events []domain.Event) []domain.Finding {
    for _, event := range events {
        select {
        case cm.eventBus <- event:
        case <-cm.ctx.Done():
            return nil
        }
    }
    
    var findings []domain.Finding
    timeout := time.After(100 * time.Millisecond)
    
    for {
        select {
        case finding := <-cm.findingChan:
            findings = append(findings, finding)
        case <-timeout:
            return findings
        case <-cm.ctx.Done():
            return findings
        }
    }
}

// processEvents runs continuous AI pattern recognition
func (cm *CollectionManager) processEvents() {
    defer cm.wg.Done()
    
    eventBuffer := make([]domain.Event, 0, 100)
    ticker := time.NewTicker(cm.config.PatternDetectionInterval)
    defer ticker.Stop()
    
    for {
        select {
        case event := <-cm.eventBus:
            eventBuffer = append(eventBuffer, event)
            
            if len(eventBuffer) >= 10 {
                cm.analyzePatterns(eventBuffer)
                eventBuffer = eventBuffer[:0]
            }
            
        case <-ticker.C:
            if len(eventBuffer) > 0 {
                cm.analyzePatterns(eventBuffer)
                eventBuffer = eventBuffer[:0]
            }
            
        case <-cm.ctx.Done():
            return
        }
    }
}

// analyzePatterns runs AI pattern recognition on events
func (cm *CollectionManager) analyzePatterns(events []domain.Event) {
    matches, err := cm.patternManager.DetectPatterns(cm.ctx, events)
    if err != nil {
        fmt.Printf("Pattern detection error: %v\n", err)
        return
    }
    
    for _, match := range matches {
        finding := domain.Finding{
            ID:          domain.FindingID(fmt.Sprintf("ai-pattern-%d", time.Now().UnixNano())),
            Type:        domain.FindingType("pattern"),
            Title:       fmt.Sprintf("AI Pattern: %s", match.Pattern.ID),
            Description: fmt.Sprintf("Pattern detected (Confidence: %.2f)", match.Confidence),
            Severity:    domain.Severity("info"),
            Confidence:  domain.FloatToConfidenceScore(match.Confidence),
            Timestamp:   time.Now(),
            Metadata: domain.FindingMetadata{
                Algorithm:     "ai_pattern_recognition",
                ProcessedBy:   "tapio_ai_manager",
                ProcessedAt:   time.Now(),
                SchemaVersion: "1.0",
                Annotations: map[string]string{
                    "pattern_id":       match.Pattern.ID,
                    "pattern_name":     match.Pattern.Name,
                    "events_analyzed":  fmt.Sprintf("%d", len(events)),
                    "confidence_score": fmt.Sprintf("%.2f", match.Confidence),
                    "detected_at":      match.Detected.Format(time.RFC3339),
                },
            },
        }
        
        // Adjust severity based on confidence
        if match.Confidence > 0.8 {
            finding.Severity = domain.Severity("warning")
        }
        if match.Confidence > 0.9 {
            finding.Severity = domain.Severity("critical")
        }
        
        select {
        case cm.findingChan <- finding:
        case <-cm.ctx.Done():
            return
        }
    }
}

// Findings returns the channel of AI-generated findings
func (cm *CollectionManager) Findings() <-chan domain.Finding {
    return cm.findingChan
}

// Stop gracefully shuts down pattern recognition
func (cm *CollectionManager) Stop() error {
    cm.cancel()
    cm.wg.Wait()
    close(cm.eventBus)
    close(cm.findingChan)
    return nil
}

// Statistics returns processing statistics
func (cm *CollectionManager) Statistics() map[string]interface{} {
    return map[string]interface{}{
        "event_buffer_size":  len(cm.eventBus),
        "finding_queue_size": len(cm.findingChan),
        "status":            "active",
    }
}
