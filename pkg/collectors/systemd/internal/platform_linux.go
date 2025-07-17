//go:build linux
// +build linux

package internal

import (
	"context"

	"github.com/yairfalse/tapio/pkg/collectors/systemd/core"
	"github.com/yairfalse/tapio/pkg/collectors/systemd/linux"
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

func (l *linuxImpl) isConnected() bool {
	return l.impl.IsConnected()
}

func (l *linuxImpl) systemdVersion() string {
	return l.impl.SystemdVersion()
}

func (l *linuxImpl) servicesMonitored() int {
	return l.impl.ServicesMonitored()
}

func (l *linuxImpl) activeServices() int {
	return l.impl.ActiveServices()
}

func (l *linuxImpl) failedServices() int {
	return l.impl.FailedServices()
}