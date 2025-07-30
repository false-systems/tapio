//go:build !linux
// +build !linux

package systemd

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// ebpfEnhancement stub for non-Linux platforms
type ebpfEnhancement struct{}

// Stub methods for non-Linux
func (s *stubImpl) initEBPF() error {
	return fmt.Errorf("eBPF not supported on this platform")
}

func (s *stubImpl) collectEBPFEvents(ctx context.Context, events chan<- collectors.RawEvent) {
	// No-op on non-Linux
}

func (s *stubImpl) cleanupEBPF() {
	// No-op on non-Linux
}
