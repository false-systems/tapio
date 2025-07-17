// +build linux

package internal

import (
	"context"
	
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/linux"
)

// newPlatformImpl creates a Linux-specific implementation
func newPlatformImpl() (platformImpl, error) {
	return &linuxImpl{
		impl: linux.New(),
	}, nil
}

// linuxImpl wraps the Linux implementation to match the internal interface
type linuxImpl struct {
	impl *linux.Implementation
}

func (l *linuxImpl) init(config core.Config) error {
	return l.impl.Init(config)
}

func (l *linuxImpl) start(ctx context.Context) error {
	return l.impl.Start(ctx)
}

func (l *linuxImpl) stop() error {
	return l.impl.Stop()
}

func (l *linuxImpl) events() <-chan core.RawEvent {
	return l.impl.Events()
}

func (l *linuxImpl) programsLoaded() int {
	return l.impl.ProgramsLoaded()
}

func (l *linuxImpl) mapsCreated() int {
	return l.impl.MapsCreated()
}