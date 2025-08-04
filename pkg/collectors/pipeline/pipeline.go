package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
)

// New creates a new event pipeline
func New(logger *zap.Logger, config Config) (*EventPipeline, error) {
	if config.Workers <= 0 {
		config.Workers = DefaultConfig().Workers
	}
	if config.BufferSize <= 0 {
		config.BufferSize = DefaultConfig().BufferSize
	}

	// Create NATS publisher
	publisher, err := NewNATSPublisher(logger, config.NATSConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create NATS publisher: %w", err)
	}

	// Create K8s enricher
	enricher, err := NewK8sEnricher(logger)
	if err != nil {
		// Log but don't fail - K8s enrichment is optional
		logger.Warn("Failed to create K8s enricher, running without enrichment", zap.Error(err))
	}

	return &EventPipeline{
		collectors: make(map[string]collectors.Collector),
		enricher:   enricher,
		publisher:  publisher,
		eventsChan: make(chan *collectors.RawEvent, config.BufferSize),
		workers:    config.Workers,
	}, nil
}

// RegisterCollector adds a collector to the pipeline
func (p *EventPipeline) RegisterCollector(name string, collector collectors.Collector) error {
	if p.ctx != nil {
		return fmt.Errorf("cannot register collector after pipeline started")
	}
	if _, exists := p.collectors[name]; exists {
		return fmt.Errorf("collector %s already registered", name)
	}
	p.collectors[name] = collector
	return nil
}

// Start begins processing events
func (p *EventPipeline) Start(ctx context.Context) error {
	if p.ctx != nil {
		return fmt.Errorf("pipeline already started")
	}

	p.ctx, p.cancel = context.WithCancel(ctx)

	// Start all collectors
	for name, collector := range p.collectors {
		if err := collector.Start(p.ctx); err != nil {
			p.Stop()
			return fmt.Errorf("failed to start collector %s: %w", name, err)
		}
	}

	// Start collector consumers
	for name, collector := range p.collectors {
		go p.consumeCollector(name, collector)
	}

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go p.worker(&wg)
	}

	// Wait for shutdown
	go func() {
		<-p.ctx.Done()
		close(p.eventsChan)
		wg.Wait()
	}()

	return nil
}

// Stop gracefully shuts down the pipeline
func (p *EventPipeline) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}

	// Stop all collectors
	for name, collector := range p.collectors {
		if err := collector.Stop(); err != nil {
			// Log but continue stopping others
			fmt.Printf("Error stopping collector %s: %v\n", name, err)
		}
	}

	// Close publisher
	if p.publisher != nil {
		p.publisher.Close()
	}

	return nil
}

// consumeCollector reads events from a collector and forwards to processing
func (p *EventPipeline) consumeCollector(name string, collector collectors.Collector) {
	for {
		select {
		case event, ok := <-collector.Events():
			if !ok {
				return
			}
			// Add collector name to metadata
			if event.Metadata == nil {
				event.Metadata = make(map[string]string)
			}
			event.Metadata["collector_name"] = name

			select {
			case p.eventsChan <- &event:
			case <-p.ctx.Done():
				return
			}
		case <-p.ctx.Done():
			return
		}
	}
}

// worker processes events
func (p *EventPipeline) worker(wg *sync.WaitGroup) {
	defer wg.Done()

	for event := range p.eventsChan {
		// Enrich event
		enriched := p.enrichEvent(event)

		// Convert to unified event
		unified := enriched.ConvertToUnified()

		// Publish to NATS
		if p.publisher != nil {
			if err := p.publisher.Publish(unified); err != nil {
				// Log error but continue
				fmt.Printf("Failed to publish event: %v\n", err)
			}
		}
	}
}

// enrichEvent adds context to raw event
func (p *EventPipeline) enrichEvent(raw *collectors.RawEvent) *EnrichedEvent {
	enriched := &EnrichedEvent{
		Raw:     raw,
		TraceID: raw.TraceID,
		SpanID:  raw.SpanID,
	}

	// Add K8s context if enricher available
	if p.enricher != nil {
		if k8sInfo := p.enricher.GetObjectInfo(raw); k8sInfo != nil {
			enriched.K8sObject = k8sInfo
		}
	}

	return enriched
}

// GetHealthStatus returns the health status of all collectors
func (p *EventPipeline) GetHealthStatus() map[string]CollectorHealthStatus {
	status := make(map[string]CollectorHealthStatus)

	for name, collector := range p.collectors {
		health := CollectorHealthStatus{
			Healthy: collector.IsHealthy(),
		}

		// Check if collector implements detailed health interface
		if healthReporter, ok := collector.(interface {
			Health() (bool, map[string]interface{})
		}); ok {
			healthy, details := healthReporter.Health()
			health.Healthy = healthy
			if err, ok := details["error"].(string); ok {
				health.Error = err
			}
			if lastEvent, ok := details["last_event"].(time.Time); ok {
				health.LastEvent = lastEvent
			}
		}

		status[name] = health
	}

	return status
}
