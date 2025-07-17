// +build !linux

package stub

import (
	"context"
	"fmt"
	"runtime"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

// Implementation provides a stub for non-Linux platforms
type Implementation struct {
	eventChan chan core.RawEvent
}

// New creates a new stub implementation
func New() *Implementation {
	return &Implementation{
		eventChan: make(chan core.RawEvent),
	}
}

// Init initializes the stub
func (impl *Implementation) Init(config core.Config) error {
	// Allow initialization to succeed, but fail on Start
	return nil
}

// Start returns an error on non-Linux platforms
func (impl *Implementation) Start(ctx context.Context) error {
	return fmt.Errorf("eBPF is not supported on %s", runtime.GOOS)
}

// Stop is a no-op on non-Linux platforms
func (impl *Implementation) Stop() error {
	close(impl.eventChan)
	return nil
}

// Events returns an empty channel
func (impl *Implementation) Events() <-chan core.RawEvent {
	return impl.eventChan
}

// ProgramsLoaded always returns 0
func (impl *Implementation) ProgramsLoaded() int {
	return 0
}

// MapsCreated always returns 0
func (impl *Implementation) MapsCreated() int {
	return 0
}