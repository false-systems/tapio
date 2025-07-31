//go:build !linux
// +build !linux

package internal

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

// newPlatformImpl creates a stub implementation for non-Linux platforms
func newPlatformImpl() (platformImpl, error) {
	return &stubImpl{}, nil
}

// stubImpl provides minimal stub for non-Linux platforms
type stubImpl struct{}

func (s *stubImpl) init(config core.Config) error {
	return fmt.Errorf("eBPF not supported on non-Linux platforms")
}

func (s *stubImpl) start(ctx context.Context) error {
	return fmt.Errorf("eBPF not supported on non-Linux platforms")
}

func (s *stubImpl) stop() error {
	return nil
}

func (s *stubImpl) events() <-chan core.RawEvent {
	ch := make(chan core.RawEvent)
	close(ch)
	return ch
}

func (s *stubImpl) programsLoaded() int {
	return 0
}

func (s *stubImpl) mapsCreated() int {
	return 0
}
