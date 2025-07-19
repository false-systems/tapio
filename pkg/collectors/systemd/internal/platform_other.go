//go:build !linux
// +build !linux

package internal

import (
	"context"

	"github.com/yairfalse/tapio/pkg/collectors/systemd/core"
	"github.com/yairfalse/tapio/pkg/collectors/systemd/stub"
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

func (s *stubImpl) isConnected() bool {
	return s.impl.IsConnected()
}

func (s *stubImpl) systemdVersion() string {
	return s.impl.SystemdVersion()
}

func (s *stubImpl) servicesMonitored() int {
	return s.impl.ServicesMonitored()
}

func (s *stubImpl) activeServices() int {
	return s.impl.ActiveServices()
}

func (s *stubImpl) failedServices() int {
	return s.impl.FailedServices()
}
