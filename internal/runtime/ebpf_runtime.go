//go:build linux

package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cilium/ebpf"
)

// EBPFManager is the interface for managing eBPF programs
// Implemented by base.EBPFManager
type EBPFManager interface {
	AttachKprobe(progName, symbol string) error
	AttachKretprobe(progName, symbol string) error
	AttachTracepoint(progName, group, name string) error
	AttachTracepointWithProgram(prog *ebpf.Program, group, name string) error
	OpenRingBuffer(mapName string) error
	ReadEvents(ctx context.Context, handler func([]byte) error) error
	GetMap(name string) (*ebpf.Map, error)
	Close() error
	WaitForReady(timeout time.Duration) error
}

// EBPFRuntime manages eBPF program lifecycle
type EBPFRuntime struct {
	manager EBPFManager
	mu      sync.RWMutex
	running bool
	stopCh  chan struct{}
}

// NewEBPFRuntime creates a new eBPF runtime with the given manager
func NewEBPFRuntime(manager EBPFManager) *EBPFRuntime {
	return &EBPFRuntime{
		manager: manager,
		stopCh:  make(chan struct{}),
	}
}

// Start begins reading events from the eBPF ring buffer
func (r *EBPFRuntime) Start(ctx context.Context, handler func([]byte) error) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("eBPF runtime already running")
	}
	r.running = true
	r.mu.Unlock()

	// Start reading events (blocks until context cancelled)
	err := r.manager.ReadEvents(ctx, handler)

	// Mark as stopped when ReadEvents returns
	r.mu.Lock()
	r.running = false
	r.mu.Unlock()

	return err
}

// Stop gracefully stops the eBPF runtime
func (r *EBPFRuntime) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Idempotent - if not running, nothing to do
	if !r.running {
		return nil
	}

	// Close manager resources
	return r.manager.Close()
}

// IsHealthy returns true if the runtime is running
func (r *EBPFRuntime) IsHealthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}
