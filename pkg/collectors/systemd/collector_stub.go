//go:build !linux
// +build !linux

package systemd

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// stubImpl implements systemdImpl for non-Linux platforms
type stubImpl struct{}

// newPlatformImpl creates a stub implementation
func newPlatformImpl() (systemdImpl, error) {
	return &stubImpl{}, nil
}

func (s *stubImpl) init() error {
	return fmt.Errorf("systemd collector is only supported on Linux")
}

func (s *stubImpl) connect() error {
	return fmt.Errorf("systemd collector is only supported on Linux")
}

func (s *stubImpl) disconnect() error {
	return nil
}

func (s *stubImpl) collectEvents(ctx context.Context, events chan<- collectors.RawEvent) error {
	return fmt.Errorf("systemd collector is only supported on Linux")
}

func (s *stubImpl) isHealthy() bool {
	return false
}
