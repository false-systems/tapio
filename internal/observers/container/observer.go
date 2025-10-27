//go:build linux
// +build linux

package container

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Observer monitors container lifecycle events
type Observer struct {
	name          string
	oomProcessor  *OOMProcessor
	exitProcessor *ExitProcessor
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
