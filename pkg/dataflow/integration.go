package dataflow

import (
    "context"
    "log"
    
    "github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
    "github.com/yairfalse/tapio/pkg/domain"
)

// Import the simplified monster functions directly
// We'll copy just the SimpleSemanticGrouper from simplified_monster.go

// SimpleSemanticGrouper - copied from simplified_monster.go
type SimpleSemanticGrouper struct {
    tracer interface{} // simplified for now
}

// NewSimpleSemanticGrouper creates a new semantic grouper
func NewSimpleSemanticGrouper() *SimpleSemanticGrouper {
    return &SimpleSemanticGrouper{}
}

// ProcessEvent processes a single event (simplified version)
func (sg *SimpleSemanticGrouper) ProcessEvent(ctx context.Context, event *domain.Event) (*domain.Finding, error) {
    // Simple correlation logic for now
    finding := &domain.Finding{
        ID:          domain.FindingID("finding-" + string(event.ID)),
        Type:        "semantic-correlation",
        Severity:    event.Severity,
        Title:       "Semantic correlation for " + string(event.Type),
        Description: "Simple semantic analysis of event",
        // Add basic metadata
    }
    
    return finding, nil
}

// DataFlow connects collectors to correlation engine
type DataFlow struct {
    collector  core.Collector
    correlator *SimpleSemanticGrouper
    findings   chan *domain.Finding
}

// NewDataFlow creates a new data flow
func NewDataFlow(collector core.Collector) *DataFlow {
    return &DataFlow{
        collector:  collector,
        correlator: NewSimpleSemanticGrouper(),
        findings:   make(chan *domain.Finding, 100),
    }
}

// Start begins the data flow
func (df *DataFlow) Start(ctx context.Context) error {
    // Start the collector
    if err := df.collector.Start(ctx); err != nil {
        return err
    }
    
    // Start processing events
    go df.processEvents(ctx)
    return nil
}

// processEvents handles the event flow
func (df *DataFlow) processEvents(ctx context.Context) {
    eventChan := df.collector.Events()
    
    for {
        select {
        case event := <-eventChan:
            // Process through correlation engine
            finding, err := df.correlator.ProcessEvent(ctx, &event)
            if err != nil {
                log.Printf("Correlation error: %v", err)
                continue
            }
            
            // Send to findings channel
            select {
            case df.findings <- finding:
            default:
                log.Printf("Findings channel full, dropping finding")
            }
            
        case <-ctx.Done():
            return
        }
    }
}

// Findings returns the findings channel
func (df *DataFlow) Findings() <-chan *domain.Finding {
    return df.findings
}

// Stop gracefully shuts down the data flow
func (df *DataFlow) Stop() error {
    close(df.findings)
    return df.collector.Stop()
}
