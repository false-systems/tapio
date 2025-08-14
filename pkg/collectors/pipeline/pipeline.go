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

	// Create NATS publisher (enhanced version for production)
	var publisher NATSPublisherInterface
	var err error
	if config.UseEnhancedNATS {
		publisher, err = NewEnhancedNATSPublisher(logger, config.NATSConfig)
	} else {
		publisher, err = NewNATSPublisher(logger, config.NATSConfig)
	}
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
		logger:     logger,
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

	// Use WaitGroup to track all goroutines
	p.wg = &sync.WaitGroup{}

	// Start collector consumers
	for name, collector := range p.collectors {
		p.wg.Add(1)
		go p.consumeCollector(name, collector)
	}

	// Start workers
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}

	return nil
}

// Stop gracefully shuts down the pipeline
func (p *EventPipeline) Stop() error {
	p.logger.Info("Stopping event pipeline")

	// Prevent multiple shutdown attempts
	if p.cancel == nil {
		p.logger.Warn("Pipeline already stopped or not started")
		return nil
	}

	// Step 1: Signal shutdown to all components first
	p.cancel()

	// Step 2: Stop all collectors to prevent new events
	// This must be done BEFORE waiting for goroutines to prevent new events
	for name, collector := range p.collectors {
		if err := collector.Stop(); err != nil {
			// Log but continue stopping others
			p.logger.Warn("Error stopping collector",
				zap.String("collector", name),
				zap.Error(err))
		}
	}

	// Step 3: Wait for all goroutines to finish with timeout
	// This ensures all collector consumers and workers have stopped before closing channels
	done := make(chan struct{})
	go func() {
		if p.wg != nil {
			p.wg.Wait()
		}
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("All workers stopped gracefully")
	case <-time.After(10 * time.Second):
		p.logger.Error("Timeout waiting for workers to stop")
		// Continue with shutdown even after timeout
	}

	// Step 4: Close event channel safely after all goroutines have stopped
	// Use a safe close pattern to prevent panic if already closed
	if p.eventsChan != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					p.logger.Debug("Channel already closed during shutdown")
				}
			}()
			// Only close if not already closed
			select {
			case event, ok := <-p.eventsChan:
				if !ok {
					// Channel already closed
					return
				}
				// Put the value back if we read one
				select {
				case p.eventsChan <- event:
				default:
					// Channel full, just close it
				}
				close(p.eventsChan)
			default:
				// No data available, safe to close
				close(p.eventsChan)
			}
		}()
	}

	// Step 5: Close publisher with graceful shutdown
	if p.publisher != nil {
		publisherDone := make(chan bool, 1)
		go func() {
			p.publisher.Close()
			publisherDone <- true
		}()

		select {
		case <-publisherDone:
			p.logger.Info("Publisher closed gracefully")
		case <-time.After(5 * time.Second):
			p.logger.Warn("Timeout closing publisher")
		}
	}

	// Reset cancel to prevent multiple shutdowns
	p.cancel = nil

	p.logger.Info("Event pipeline stopped")
	return nil
}

// consumeCollector reads events from a collector and forwards to processing
func (p *EventPipeline) consumeCollector(name string, collector collectors.Collector) {
	defer p.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("Panic in collector consumer",
				zap.String("collector", name),
				zap.Any("panic", r),
			)
		}
	}()

	p.logger.Debug("Starting collector consumer", zap.String("collector", name))

	for {
		select {
		case <-p.ctx.Done():
			p.logger.Info("Collector consumer stopping due to context cancellation",
				zap.String("collector", name))
			return
		case event, ok := <-collector.Events():
			if !ok {
				p.logger.Info("Collector channel closed", zap.String("collector", name))
				return
			}

			// Add collector name to metadata
			if event.Metadata == nil {
				event.Metadata = make(map[string]string)
			}
			event.Metadata["collector_name"] = name

			// Try to send event with safe channel writing
			// Use a non-blocking select to avoid race conditions during shutdown
			select {
			case <-p.ctx.Done():
				// Pipeline is shutting down, drop event
				p.logger.Debug("Dropping event due to shutdown",
					zap.String("collector", name),
					zap.String("event_type", event.Type))
				return
			case p.eventsChan <- &event:
				// Successfully sent event
			default:
				// Channel full or closed, use timeout with graceful handling
				select {
				case <-p.ctx.Done():
					return
				case p.eventsChan <- &event:
					// Successfully sent after retry
				case <-time.After(100 * time.Millisecond):
					// Drop event if channel is blocked
					p.logger.Debug("Dropping event due to channel backpressure",
						zap.String("collector", name),
						zap.String("event_type", event.Type))
				}
			}
		}
	}
}

// worker processes events
func (p *EventPipeline) worker() {
	defer p.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("Panic in pipeline worker",
				zap.Any("panic", r),
			)
		}
	}()

	for {
		select {
		case event, ok := <-p.eventsChan:
			if !ok {
				return
			}
			func() {
				// Wrap individual event processing in recovery
				defer func() {
					if r := recover(); r != nil {
						p.logger.Error("Panic processing event",
							zap.Any("panic", r),
							zap.String("event_type", event.Type),
						)
					}
				}()

				// Enrich event with K8s context while keeping raw event structure
				enriched := p.enrichEvent(event)

				// Publish raw event directly to NATS with retry logic
				// This is the key change: we publish the raw event instead of unified event
				if p.publisher != nil {
					retries := 3
					for i := 0; i < retries; i++ {
						err := p.publisher.Publish(*enriched.Raw)
						if err == nil {
							break
						}

						if i == retries-1 {
							// Final retry failed - use raw event fields for logging
							eventID := fmt.Sprintf("%s-%d", enriched.Raw.Type, enriched.Raw.Timestamp.UnixNano())
							if enriched.Raw.TraceID != "" {
								eventID = enriched.Raw.TraceID
							}
							p.logger.Error("Failed to publish raw event after retries",
								zap.Error(err),
								zap.String("event_id", eventID),
								zap.String("event_type", enriched.Raw.Type),
								zap.String("trace_id", enriched.Raw.TraceID),
								zap.Int("retries", retries),
							)
						} else {
							// Wait before retry
							time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
						}
					}
				}
			}()
		case <-p.ctx.Done():
			return
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

		// Check if collector implements structured health interface
		if healthReporter, ok := collector.(interface {
			Health() HealthDetails
		}); ok {
			details := healthReporter.Health()
			health.Healthy = details.Healthy
			health.Error = details.Error
			health.LastEvent = details.LastEvent
		}

		status[name] = health
	}

	return status
}
