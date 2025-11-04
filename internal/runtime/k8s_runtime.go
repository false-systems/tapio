package runtime

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/client-go/tools/cache"
)

// K8sRuntime manages Kubernetes informer lifecycle
type K8sRuntime struct {
	informers []cache.SharedIndexInformer
	stopChs   []chan struct{}
	mu        sync.RWMutex
	started   bool
}

// NewK8sRuntime creates a new K8s runtime
func NewK8sRuntime() *K8sRuntime {
	return &K8sRuntime{
		informers: []cache.SharedIndexInformer{},
		stopChs:   []chan struct{}{},
	}
}

// AddInformer registers an informer with event handlers
func (r *K8sRuntime) AddInformer(informer cache.SharedIndexInformer, handlers cache.ResourceEventHandlerFuncs) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return fmt.Errorf("cannot add informer: runtime already started")
	}

	// Register handlers
	if _, err := informer.AddEventHandler(handlers); err != nil {
		return fmt.Errorf("failed to add event handler: %w", err)
	}

	r.informers = append(r.informers, informer)
	return nil
}

// Start starts all registered informers
func (r *K8sRuntime) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return fmt.Errorf("runtime already started")
	}
	r.started = true

	// Create stop channels for each informer
	for range r.informers {
		r.stopChs = append(r.stopChs, make(chan struct{}))
	}
	r.mu.Unlock()

	// Start all informers
	for i, informer := range r.informers {
		stopCh := r.stopChs[i]
		go informer.Run(stopCh)
	}

	// Wait for context cancellation
	<-ctx.Done()

	return nil
}

// WaitForCacheSync waits for all informers to sync
func (r *K8sRuntime) WaitForCacheSync(ctx context.Context) error {
	r.mu.RLock()
	if !r.started {
		r.mu.RUnlock()
		return fmt.Errorf("runtime not started")
	}
	informers := r.informers
	r.mu.RUnlock()

	// Build slice of HasSynced functions for cache.WaitForCacheSync
	syncFuncs := make([]cache.InformerSynced, 0, len(informers))
	for _, inf := range informers {
		syncFuncs = append(syncFuncs, inf.HasSynced)
	}

	// Wait for all to sync
	if !cache.WaitForCacheSync(ctx.Done(), syncFuncs...) {
		return fmt.Errorf("timeout waiting for cache sync")
	}

	return nil
}

// Stop stops all informers
func (r *K8sRuntime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, stopCh := range r.stopChs {
		close(stopCh)
	}
}
