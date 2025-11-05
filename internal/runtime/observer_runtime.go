package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
)

// ObserverRuntime is the unified infrastructure for all observers.
type ObserverRuntime struct {
	config    Config
	processor EventProcessor
	emitters  []Emitter
	mu        sync.RWMutex
	running   bool
}

// NewObserverRuntime creates a new runtime with the given processor and options.
func NewObserverRuntime(processor EventProcessor, opts ...Option) (*ObserverRuntime, error) {
	if processor == nil {
		return nil, fmt.Errorf("processor is required")
	}

	// Start with default config
	config := DefaultConfig(processor.Name())

	runtime := &ObserverRuntime{
		config:    config,
		processor: processor,
	}

	// Apply options
	for _, opt := range opts {
		opt(runtime)
	}

	// Validate final config
	if err := runtime.config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

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

	// Emit to all emitters with criticality handling
	var criticalErr error
	for _, emitter := range r.emitters {
		if err := emitter.Emit(ctx, event); err != nil {
			if emitter.IsCritical() {
				// Critical emitter failed - fail entire emission
				criticalErr = fmt.Errorf("critical emitter %s failed: %w", emitter.Name(), err)
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

	return criticalErr
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
