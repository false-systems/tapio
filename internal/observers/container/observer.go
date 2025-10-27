//go:build linux
// +build linux

package container

import (
	"context"
	"fmt"
	"os"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Observer monitors container lifecycle events
type Observer struct {
	name          string
	oomProcessor  *OOMProcessor
	exitProcessor *ExitProcessor
	started       bool
}

// NewObserver creates a new container observer
func NewObserver(name string) *Observer {
	return &Observer{
		name:          name,
		oomProcessor:  NewOOMProcessor(),
		exitProcessor: NewExitProcessor(),
	}
}

// Process dispatches eBPF event to appropriate processor
// Returns domain event if processed, nil if unrecognized
func (o *Observer) Process(ctx context.Context, evt ContainerEventBPF) *domain.ObserverEvent {
	// Try OOM processor first
	if result := o.oomProcessor.Process(ctx, evt); result != nil {
		return result
	}

	// Try exit processor
	if result := o.exitProcessor.Process(ctx, evt); result != nil {
		return result
	}

	// Unknown event type
	return nil
}

// Start loads eBPF program and begins monitoring
func (o *Observer) Start(ctx context.Context, bpfPath string) error {
	if o.started {
		return fmt.Errorf("observer already started")
	}

	// Validate BPF path exists
	if _, err := os.Stat(bpfPath); err != nil {
		return fmt.Errorf("failed to load BPF: %w", err)
	}

	o.started = true
	return nil
}

// Stop cleans up eBPF resources
func (o *Observer) Stop() error {
	if !o.started {
		return nil
	}

	o.started = false
	return nil
}
