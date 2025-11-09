package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ObserverRuntime is the unified infrastructure for all observers.
type ObserverRuntime struct {
	config      Config
	processor   EventProcessor
	emitters    []Emitter
	sampler     *Sampler
	queue       *BoundedQueue
	mu          sync.RWMutex
	running     bool
	retryMu     sync.Mutex
	retryCounts map[string]int // Event ID -> retry count (prevents memory leaks)
}

// NewObserverRuntime creates a new runtime with the given processor and options.
func NewObserverRuntime(processor EventProcessor, opts ...Option) (*ObserverRuntime, error) {
	if processor == nil {
		return nil, fmt.Errorf("processor is required")
	}

	// Start with default config
	config := DefaultConfig(processor.Name())

	runtime := &ObserverRuntime{
		config:      config,
		processor:   processor,
		retryCounts: make(map[string]int),
	}

	// Apply options
	for _, opt := range opts {
		opt(runtime)
	}

	// Validate final config
	if err := runtime.config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create sampler and queue AFTER options (so they get final config)
	runtime.sampler = NewSampler(runtime.config.Sampling)
	runtime.queue = NewBoundedQueue(runtime.config.Backpressure)

	return runtime, nil
}

// Run starts the observer runtime and blocks until context is cancelled.
func (r *ObserverRuntime) Run(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("runtime already running")
	}
	r.running = true
	r.mu.Unlock()

	// Setup with config
	if err := r.processor.Setup(ctx, r.config); err != nil {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
		return err
	}

	// Start queue drainer
	go r.drainQueue(ctx)

	// Wait for cancellation
	<-ctx.Done()

	// Teardown
	r.mu.Lock()
	r.running = false
	r.mu.Unlock()

	return r.processor.Teardown(context.Background())
}

// ProcessEvent processes a raw event through the processor and emits to emitters
func (r *ObserverRuntime) ProcessEvent(ctx context.Context, rawEvent []byte) error {
	r.mu.RLock()
	if !r.running {
		r.mu.RUnlock()
		return fmt.Errorf("runtime not running")
	}
	r.mu.RUnlock()

	// Process
	event, err := r.processor.Process(ctx, rawEvent)
	if err != nil {
		return err
	}

	// Processor can return nil (ignore event)
	if event == nil {
		return nil
	}

	// Apply sampling if enabled
	if r.config.Sampling.Enabled {
		if !r.sampler.ShouldSample(event) {
			// Event sampled out
			return nil
		}
	}

	// Enqueue event (applies backpressure)
	if !r.queue.Enqueue(event) {
		// Queue full - drop event
		return fmt.Errorf("event dropped: queue full (capacity: %d)", r.queue.Cap())
	}

	return nil
}

// IsHealthy returns true if runtime is running
func (r *ObserverRuntime) IsHealthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

// Option is a functional option for configuring ObserverRuntime
type Option func(*ObserverRuntime)

// WithEmitters configures the emitters to use
func WithEmitters(emitters ...Emitter) Option {
	return func(r *ObserverRuntime) {
		r.emitters = emitters
	}
}

// WithSamplingDisabled disables event sampling (useful for tests)
func WithSamplingDisabled() Option {
	return func(r *ObserverRuntime) {
		r.config.Sampling.Enabled = false
	}
}

// drainQueue continuously drains events from queue and emits to emitters
func (r *ObserverRuntime) drainQueue(ctx context.Context) {
	ticker := time.NewTicker(r.config.Backpressure.DrainInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Drain all available events
			for {
				event := r.queue.Dequeue()
				if event == nil {
					break // Queue empty
				}

				// Emit to all emitters with criticality handling
				var criticalErr error
				for _, emitter := range r.emitters {
					if err := emitter.Emit(ctx, event); err != nil {
						if emitter.IsCritical() {
							criticalErr = err
							log.Error().
								Str("emitter", emitter.Name()).
								Err(err).
								Str("observer", r.processor.Name()).
								Msg("critical emitter failed")
							break
						}
						// Non-critical emitter failed - log but continue
						log.Warn().
							Str("emitter", emitter.Name()).
							Err(err).
							Str("observer", r.processor.Name()).
							Msg("non-critical emitter failed")
					}
				}

				if criticalErr != nil {
					// Check retry count using event ID (prevents memory leaks)
					r.retryMu.Lock()
					retryCount := r.retryCounts[event.ID]
					r.retryMu.Unlock()

					if retryCount < r.config.Backpressure.MaxRetries {
						// Increment retry count and re-enqueue
						r.retryMu.Lock()
						r.retryCounts[event.ID] = retryCount + 1
						r.retryMu.Unlock()

						// Re-enqueue for retry
						if !r.queue.Enqueue(event) {
							// Queue full - drop event and clean up retry tracking
							r.retryMu.Lock()
							delete(r.retryCounts, event.ID)
							r.retryMu.Unlock()
							log.Warn().
								Str("observer", r.processor.Name()).
								Str("event_id", event.ID).
								Int("retry_count", retryCount).
								Msg("dropped event: queue full during retry")
						}
					} else {
						// Max retries exceeded - drop event and clean up
						r.retryMu.Lock()
						delete(r.retryCounts, event.ID)
						r.retryMu.Unlock()
						log.Error().
							Str("observer", r.processor.Name()).
							Str("event_id", event.ID).
							Int("max_retries", r.config.Backpressure.MaxRetries).
							Msg("dropped event: max retries exceeded")
					}
				} else {
					// Success - clean up retry tracking
					r.retryMu.Lock()
					delete(r.retryCounts, event.ID)
					r.retryMu.Unlock()
				}
			}
		}
	}
}
