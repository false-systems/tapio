//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"context"
	"fmt"
	"sync"
)

// internalCollectorManager manages eBPF-specific collectors without external dependencies
type internalCollectorManager struct {
	collectors map[string]interface{} // Using interface{} to avoid import cycles
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
}

// newInternalCollectorManager creates a new internal collector manager
func newInternalCollectorManager() *internalCollectorManager {
	return &internalCollectorManager{
		collectors: make(map[string]interface{}),
	}
}

// RegisterCollector registers a collector (interface{} to avoid import cycles)
func (cm *internalCollectorManager) RegisterCollector(name string, collector interface{}) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.collectors[name]; exists {
		return fmt.Errorf("collector %s already registered", name)
	}

	cm.collectors[name] = collector
	return nil
}

// Start starts the manager (collectors will be started externally)
func (cm *internalCollectorManager) Start(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.ctx, cm.cancel = context.WithCancel(ctx)
	return nil
}

// Stop stops the manager
func (cm *internalCollectorManager) Stop() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.cancel != nil {
		cm.cancel()
	}
	return nil
}

// GetCollector returns a collector by name
func (cm *internalCollectorManager) GetCollector(name string) (interface{}, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	collector, exists := cm.collectors[name]
	return collector, exists
}
