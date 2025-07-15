#!/bin/bash

# Script to fix the import cycle in Tapio project

echo "🔧 Fixing import cycle in Tapio project..."

# Step 1: Remove the problematic CollectorManager files from pkg/ebpf
echo "📁 Removing collector_manager files from pkg/ebpf..."
rm -f pkg/ebpf/collector_manager.go
rm -f pkg/ebpf/collector_manager_stub.go

# Step 2: Create a simple internal ebpf collector manager that doesn't import pkg/collectors
echo "📝 Creating internal eBPF collector manager..."

cat > pkg/ebpf/internal_manager.go << 'EOF'
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
EOF

# Create stub version for non-Linux platforms
cat > pkg/ebpf/internal_manager_stub.go << 'EOF'
//go:build !linux || !ebpf
// +build !linux !ebpf

package ebpf

import (
	"context"
)

// internalCollectorManager stub for non-Linux platforms
type internalCollectorManager struct{}

func newInternalCollectorManager() *internalCollectorManager {
	return &internalCollectorManager{}
}

func (cm *internalCollectorManager) RegisterCollector(name string, collector interface{}) error {
	return nil
}

func (cm *internalCollectorManager) Start(ctx context.Context) error {
	return nil
}

func (cm *internalCollectorManager) Stop() error {
	return nil
}

func (cm *internalCollectorManager) GetCollector(name string) (interface{}, bool) {
	return nil, false
}
EOF

echo "✅ Import cycle fix script completed!"
echo ""
echo "📋 Next steps:"
echo "1. Update monitor_linux.go to use internalCollectorManager"
echo "2. Move any external collector management to pkg/collectors"
echo "3. Test the build"