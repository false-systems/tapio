package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

// CollectorPipeline is the main pipeline implementation
type CollectorPipeline struct {
	config PipelineConfig

	// Converters for different sources
	converters map[string]EventConverter

	// Enrichers
	enrichers []Enricher

	// Output channel
	output chan *domain.UnifiedEvent

	// State
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.RWMutex
	started bool
	healthy bool

	// Metrics
	processedCount uint64
	errorCount     uint64
}

// NewPipeline creates a new pipeline
func NewPipeline(config PipelineConfig) Pipeline {
	if config.OutputBufferSize <= 0 {
		config.OutputBufferSize = 10000
	}
	if config.Workers <= 0 {
		config.Workers = 4
	}

	return &CollectorPipeline{
		config:     config,
		converters: make(map[string]EventConverter),
		enrichers:  make([]Enricher, 0),
		output:     make(chan *domain.UnifiedEvent, config.OutputBufferSize),
		healthy:    true,
	}
}

// RegisterConverter registers an event converter for a source type
func (p *CollectorPipeline) RegisterConverter(converter EventConverter) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.converters[converter.SourceType()] = converter
}

// AddEnricher adds an enricher to the pipeline
func (p *CollectorPipeline) AddEnricher(enricher Enricher) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.enrichers = append(p.enrichers, enricher)
}

// Process handles a raw event
func (p *CollectorPipeline) Process(ctx context.Context, event collectors.RawEvent) error {
	p.mu.RLock()
	converter, exists := p.converters[event.Type]
	p.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no converter for source type: %s", event.Type)
	}

	// Convert to UnifiedEvent
	unified, err := converter.Convert(ctx, event)
	if err != nil {
		p.incrementErrorCount()
		return fmt.Errorf("conversion failed: %w", err)
	}

	// Apply enrichers
	for _, enricher := range p.enrichers {
		if err := enricher.Enrich(ctx, unified); err != nil {
			// Log but continue - enrichment failures shouldn't stop processing
			// In production, use proper logging
		}
	}

	// Send to output
	select {
	case p.output <- unified:
		p.incrementProcessedCount()
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Buffer full, drop event
		// In production, add metrics for dropped events
		return fmt.Errorf("output buffer full")
	}

	return nil
}

// Start starts the pipeline
func (p *CollectorPipeline) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return fmt.Errorf("pipeline already started")
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.started = true

	// Start health monitor
	p.wg.Add(1)
	go p.healthMonitor()

	return nil
}

// Stop stops the pipeline
func (p *CollectorPipeline) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started {
		return nil
	}

	p.cancel()
	p.wg.Wait()

	close(p.output)
	p.started = false
	p.healthy = false

	return nil
}

// Output returns the output channel
func (p *CollectorPipeline) Output() <-chan *domain.UnifiedEvent {
	return p.output
}

// IsHealthy returns true if the pipeline is healthy
func (p *CollectorPipeline) IsHealthy() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.healthy
}

// healthMonitor monitors pipeline health
func (p *CollectorPipeline) healthMonitor() {
	defer p.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			// Simple health check - could be more sophisticated
			p.mu.Lock()
			p.healthy = p.errorCount < 100 // Arbitrary threshold
			p.mu.Unlock()
		}
	}
}

// incrementProcessedCount safely increments processed count
func (p *CollectorPipeline) incrementProcessedCount() {
	p.mu.Lock()
	p.processedCount++
	p.mu.Unlock()
}

// incrementErrorCount safely increments error count
func (p *CollectorPipeline) incrementErrorCount() {
	p.mu.Lock()
	p.errorCount++
	p.mu.Unlock()
}
