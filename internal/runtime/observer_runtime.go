package runtime

import (
	"context"
	"fmt"
	"sync"
)

// ObserverRuntime is the unified infrastructure for all observers.
type ObserverRuntime struct {
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

	runtime := &ObserverRuntime{
		processor: processor,
	}

	// Apply options
	for _, opt := range opts {
		opt(runtime)
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

	// Setup
	if err := r.processor.Setup(ctx); err != nil {
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

	// Emit to all emitters
	for _, emitter := range r.emitters {
		if err := emitter.Emit(ctx, event); err != nil {
			// Best-effort - log but continue
			fmt.Printf("emitter %s failed: %v\n", emitter.Name(), err)
		}
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
