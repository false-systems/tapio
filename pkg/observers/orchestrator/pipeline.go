package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/observers"
	"go.uber.org/zap"
)

// New creates a new observer orchestrator
func New(logger *zap.Logger, config Config) (*ObserverOrchestrator, error) {
	// Validate and set defaults for worker count
	if config.Workers <= 0 {
		logger.Warn("Invalid worker count, using default", zap.Int("provided", config.Workers), zap.Int("default", 4))
		config.Workers = DefaultConfig().Workers
	} else if config.Workers > 64 {
		logger.Warn("Worker count too high, capping at maximum", zap.Int("provided", config.Workers), zap.Int("maximum", 64))
		config.Workers = 64
	}

	// Validate and set defaults for buffer size
	if config.BufferSize <= 0 {
		logger.Warn("Invalid buffer size, using default", zap.Int("provided", config.BufferSize), zap.Int("default", 10000))
		config.BufferSize = DefaultConfig().BufferSize
	} else if config.BufferSize < 100 {
		logger.Warn("Buffer size too small, using minimum", zap.Int("provided", config.BufferSize), zap.Int("minimum", 100))
		config.BufferSize = 100
	} else if config.BufferSize > 100000 {
		logger.Warn("Buffer size too large, capping at maximum", zap.Int("provided", config.BufferSize), zap.Int("maximum", 100000))
		config.BufferSize = 100000
	}

	logger.Info("Creating observer orchestrator",
		zap.Int("workers", config.Workers),
		zap.Int("buffer_size", config.BufferSize),
		zap.String("nats_url", config.NATSConfig.URL))

	// Create enhanced NATS publisher (production-ready with backpressure)
	publisher, err := NewEnhancedNATSPublisher(logger, config.NATSConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create NATS publisher: %w", err)
	}

	return &ObserverOrchestrator{
		observers:  make(map[string]observers.Observer),
		publisher:  publisher,
		logger:     logger,
		eventsChan: make(chan *domain.CollectorEvent, config.BufferSize),
		workers:    config.Workers,
	}, nil
}

// RegisterObserver adds an observer to the orchestrator
func (p *ObserverOrchestrator) RegisterObserver(name string, observer observers.Observer) error {
	if p.ctx != nil {
		return fmt.Errorf("cannot register observer after orchestrator started")
	}
	if _, exists := p.observers[name]; exists {
		return fmt.Errorf("observer %s already registered", name)
	}
	p.observers[name] = observer
	return nil
}

// Start begins processing events
func (p *ObserverOrchestrator) Start(ctx context.Context) error {
	if p.ctx != nil {
		return fmt.Errorf("orchestrator already started")
	}

	p.ctx, p.cancel = context.WithCancel(ctx)

	// Start all observers
	for name, observer := range p.observers {
		if err := observer.Start(p.ctx); err != nil {
			p.Stop()
			return fmt.Errorf("failed to start observer %s: %w", name, err)
		}
	}

	// Use WaitGroup to track all goroutines
	p.wg = &sync.WaitGroup{}

	// Start observer consumers
	for name, observer := range p.observers {
		p.wg.Add(1)
		go p.consumeObserver(name, observer)
	}

	// Start workers
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}

	return nil
}

// Stop gracefully shuts down the orchestrator
func (p *ObserverOrchestrator) Stop() error {
	p.logger.Info("Stopping observer orchestrator")

	// Prevent multiple shutdown attempts
	if p.cancel == nil {
		p.logger.Warn("Orchestrator already stopped or not started")
		return nil
	}

	// Step 1: Signal shutdown to all components first
	p.cancel()

	// Step 2: Stop all observers to prevent new events
	// This must be done BEFORE waiting for goroutines to prevent new events
	for name, observer := range p.observers {
		if err := observer.Stop(); err != nil {
			// Log but continue stopping others
			p.logger.Warn("Error stopping observer",
				zap.String("observer", name),
				zap.Error(err))
		}
	}

	// Step 3: Wait for all goroutines to finish with timeout
	// This ensures all observer consumers and workers have stopped before closing channels
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

	p.logger.Info("Observer orchestrator stopped")
	return nil
}

// consumeObserver reads events from an observer and forwards to processing
func (p *ObserverOrchestrator) consumeObserver(name string, observer observers.Observer) {
	defer p.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("Panic in observer consumer",
				zap.String("observer", name),
				zap.Any("panic", r),
			)
		}
	}()

	p.logger.Debug("Starting observer consumer", zap.String("observer", name))

	for {
		select {
		case <-p.ctx.Done():
			p.logger.Info("Observer consumer stopping due to context cancellation",
				zap.String("observer", name))
			return
		case event, ok := <-observer.Events():
			if !ok {
				p.logger.Info("Observer channel closed", zap.String("observer", name))
				return
			}

			// Add observer name to metadata attributes
			if event.Metadata.Attributes == nil {
				event.Metadata.Attributes = make(map[string]string)
			}
			event.Metadata.Attributes["observer_name"] = name

			// Try to send event with safe channel writing
			// Use a non-blocking select to avoid race conditions during shutdown
			select {
			case <-p.ctx.Done():
				// Orchestrator is shutting down, drop event
				p.logger.Debug("Dropping event due to shutdown",
					zap.String("observer", name),
					zap.String("event_type", string(event.Type)))
				return
			case p.eventsChan <- event:
				// Successfully sent event
			default:
				// Channel full or closed, use timeout with graceful handling
				select {
				case <-p.ctx.Done():
					return
				case p.eventsChan <- event:
					// Successfully sent after retry
				case <-time.After(100 * time.Millisecond):
					// Drop event if channel is blocked
					p.logger.Debug("Dropping event due to channel backpressure",
						zap.String("observer", name),
						zap.String("event_type", string(event.Type)))
				}
			}
		}
	}
}

// worker processes events
func (p *ObserverOrchestrator) worker() {
	defer p.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("Panic in orchestrator worker",
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
							zap.String("event_type", string(event.Type)),
						)
					}
				}()

				// Publish raw event directly to NATS with retry logic
				if p.publisher != nil {
					retries := 3
					for i := 0; i < retries; i++ {
						err := p.publisher.Publish(event)
						if err == nil {
							break
						}

						if i == retries-1 {
							// Final retry failed - use ObserverEvent fields for logging
							eventID := fmt.Sprintf("%s-%d", string(event.Type), event.Timestamp.UnixNano())
							if event.Metadata.TraceID != "" {
								eventID = event.Metadata.TraceID
							}
							p.logger.Error("Failed to publish event after retries",
								zap.Error(err),
								zap.String("event_id", eventID),
								zap.String("event_type", string(event.Type)),
								zap.String("trace_id", event.Metadata.TraceID),
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

// GetHealthStatus returns the health status of all observers
func (p *ObserverOrchestrator) GetHealthStatus() map[string]ObserverHealthStatus {
	status := make(map[string]ObserverHealthStatus)

	for name, observer := range p.observers {
		health := ObserverHealthStatus{
			Healthy: observer.IsHealthy(),
		}

		// Check if observer implements structured health interface
		if healthReporter, ok := observer.(interface {
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
