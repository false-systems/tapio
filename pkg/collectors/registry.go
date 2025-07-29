package collectors

import (
	"context"
	"fmt"
	"sync"
)

// Registry manages multiple collectors and provides centralized control
type Registry struct {
	collectors map[string]Collector
	mu         sync.RWMutex

	// Event aggregation
	events chan RawEvent
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// State
	started bool
}

// NewRegistry creates a new collector registry
func NewRegistry() *Registry {
	return &Registry{
		collectors: make(map[string]Collector),
		events:     make(chan RawEvent, 10000), // Large buffer for aggregated events
	}
}

// Register adds a collector to the registry
func (r *Registry) Register(name string, collector Collector) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return fmt.Errorf("cannot register collectors after registry is started")
	}

	if _, exists := r.collectors[name]; exists {
		return fmt.Errorf("collector '%s' already registered", name)
	}

	r.collectors[name] = collector
	return nil
}

// Unregister removes a collector from the registry
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return fmt.Errorf("cannot unregister collectors after registry is started")
	}

	if _, exists := r.collectors[name]; !exists {
		return fmt.Errorf("collector '%s' not found", name)
	}

	delete(r.collectors, name)
	return nil
}

// Start starts all registered collectors
func (r *Registry) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return fmt.Errorf("registry already started")
	}

	if len(r.collectors) == 0 {
		return fmt.Errorf("no collectors registered")
	}

	r.ctx, r.cancel = context.WithCancel(ctx)

	// Start all collectors
	for name, collector := range r.collectors {
		if err := collector.Start(r.ctx); err != nil {
			// Stop already started collectors
			r.stopCollectors()
			return fmt.Errorf("failed to start collector '%s': %w", name, err)
		}

		// Start event forwarder for this collector
		r.wg.Add(1)
		go r.forwardEvents(name, collector)
	}

	r.started = true
	return nil
}

// Stop stops all collectors
func (r *Registry) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return nil
	}

	// Cancel context to stop forwarders
	r.cancel()

	// Stop all collectors
	r.stopCollectors()

	// Wait for forwarders to finish
	r.wg.Wait()

	// Close aggregated events channel
	close(r.events)

	r.started = false
	return nil
}

// Events returns the aggregated event channel from all collectors
func (r *Registry) Events() <-chan RawEvent {
	return r.events
}

// List returns the names of all registered collectors
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.collectors))
	for name := range r.collectors {
		names = append(names, name)
	}
	return names
}

// Get returns a specific collector by name
func (r *Registry) Get(name string) (Collector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	collector, exists := r.collectors[name]
	return collector, exists
}

// Health returns the health status of all collectors
func (r *Registry) Health() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	health := make(map[string]bool)
	for name, collector := range r.collectors {
		health[name] = collector.IsHealthy()
	}
	return health
}

// IsHealthy returns true if all collectors are healthy
func (r *Registry) IsHealthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, collector := range r.collectors {
		if !collector.IsHealthy() {
			return false
		}
	}
	return true
}

// stopCollectors stops all collectors (must be called with lock held)
func (r *Registry) stopCollectors() {
	var wg sync.WaitGroup

	for name, collector := range r.collectors {
		wg.Add(1)
		go func(n string, c Collector) {
			defer wg.Done()
			if err := c.Stop(); err != nil {
				// Log error but continue
				// In production, this would use proper logging
			}
		}(name, collector)
	}

	wg.Wait()
}

// forwardEvents forwards events from a collector to the aggregated channel
func (r *Registry) forwardEvents(name string, collector Collector) {
	defer r.wg.Done()

	for {
		select {
		case <-r.ctx.Done():
			return
		case event, ok := <-collector.Events():
			if !ok {
				return
			}

			// Add collector name to metadata
			if event.Metadata == nil {
				event.Metadata = make(map[string]string)
			}
			event.Metadata["collector"] = name

			select {
			case r.events <- event:
			case <-r.ctx.Done():
				return
			}
		}
	}
}

