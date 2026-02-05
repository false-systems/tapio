package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
)

// ObserverRuntime is the unified infrastructure for all observers.
type ObserverRuntime struct {
	config      Config
	processor   EventProcessor
	emitters    []domain.EventEmitter
	sampler     *Sampler
	queue       *BoundedQueue
	causality   *base.CausalityTracker // Tracks causality chains across all events
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
		causality:   base.NewCausalityTracker(), // Create causality tracker
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

	// AUTO-TRACK: Extract entity ID and record in causality tracker
	entityID := extractEntityID(event)
	if entityID != "" && event.SpanID != "" {
		r.causality.RecordEvent(event, entityID)
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

// WithEmitters configures the event emitters to use for event emission.
// All observers emit through domain.EventEmitter (the universal gateway).
func WithEmitters(emitters ...domain.EventEmitter) Option {
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

// CausalityTracker returns the causality tracker for this runtime.
// Observers can use this to query parent spans and build causality chains.
func (r *ObserverRuntime) CausalityTracker() *base.CausalityTracker {
	return r.causality
}

// GetParentSpanForEntity returns the parent span ID for a given entity.
// Entity ID format: "namespace/name" for K8s resources, "ip:port" for network endpoints.
// Returns empty string if no parent span is tracked for this entity.
func (r *ObserverRuntime) GetParentSpanForEntity(entityID string) string {
	return r.causality.GetParentSpanForEntity(entityID)
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

// extractEntityID extracts a unique entity identifier from an event for causality tracking.
// Entity ID format:
//   - K8s resources: "namespace/name" (e.g., "default/nginx-pod")
//   - Network endpoints: "ip:port" or just "ip" (e.g., "10.0.0.1:8080")
//   - Empty string if no entity can be identified
//
// Priority:
//  1. K8s resource (if K8sData present with namespace and name)
//  2. Network source endpoint (if NetworkData present with IP)
//  3. Empty (no trackable entity)
func extractEntityID(event *domain.ObserverEvent) string {
	if event == nil {
		return ""
	}

	// Priority 1: K8s resource (most specific)
	if event.K8sData != nil {
		if event.K8sData.ResourceNamespace != "" && event.K8sData.ResourceName != "" {
			return fmt.Sprintf("%s/%s", event.K8sData.ResourceNamespace, event.K8sData.ResourceName)
		}
	}

	// Priority 2: Network source endpoint
	if event.NetworkData != nil {
		if event.NetworkData.SrcIP != "" {
			if event.NetworkData.SrcPort > 0 {
				return fmt.Sprintf("%s:%d", event.NetworkData.SrcIP, event.NetworkData.SrcPort)
			}
			return event.NetworkData.SrcIP
		}
	}

	// No trackable entity
	return ""
}
