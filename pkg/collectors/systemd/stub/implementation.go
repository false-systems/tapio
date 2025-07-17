//go:build !linux
// +build !linux

package stub

import (
	"context"
	"fmt"
	"runtime"

	"github.com/yairfalse/tapio/pkg/collectors/systemd/core"
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
	return fmt.Errorf("systemd collector is not supported on %s", runtime.GOOS)
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

// IsConnected always returns false
func (impl *Implementation) IsConnected() bool {
	return false
}

// SystemdVersion returns empty string
func (impl *Implementation) SystemdVersion() string {
	return ""
}

// ServicesMonitored always returns 0
func (impl *Implementation) ServicesMonitored() int {
	return 0
}

// ActiveServices always returns 0
func (impl *Implementation) ActiveServices() int {
	return 0
}

// FailedServices always returns 0
func (impl *Implementation) FailedServices() int {
	return 0
}