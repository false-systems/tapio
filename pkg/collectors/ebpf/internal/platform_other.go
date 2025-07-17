// +build !linux

package internal

import (
	"context"
	
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/stub"
)

// newPlatformImpl creates a stub implementation for non-Linux platforms
func newPlatformImpl() (platformImpl, error) {
	return &stubImpl{
		impl: stub.New(),
	}, nil
}

// stubImpl wraps the stub implementation to match the internal interface
type stubImpl struct {
	impl *stub.Implementation
}

func (s *stubImpl) init(config core.Config) error {
	return s.impl.Init(config)
}

func (s *stubImpl) start(ctx context.Context) error {
	return s.impl.Start(ctx)
}

func (s *stubImpl) stop() error {
	return s.impl.Stop()
}

func (s *stubImpl) events() <-chan core.RawEvent {
	return s.impl.Events()
}

func (s *stubImpl) programsLoaded() int {
	return s.impl.ProgramsLoaded()
}

func (s *stubImpl) mapsCreated() int {
	return s.impl.MapsCreated()
}