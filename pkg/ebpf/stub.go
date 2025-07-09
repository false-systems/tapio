//go:build !linux || !ebpf
// +build !linux,!ebpf

package ebpf

import (
	"context"
	"fmt"
)

// stubMonitor provides a no-op implementation for non-Linux systems
type stubMonitor struct{}

func (s *stubMonitor) Start(ctx context.Context) error {
	return fmt.Errorf("eBPF monitoring not available on this system")
}

func (s *stubMonitor) Stop() error {
	return nil
}

func (s *stubMonitor) IsAvailable() bool {
	return false
}

func (s *stubMonitor) GetMemoryStats() ([]ProcessMemoryStats, error) {
	return nil, fmt.Errorf("eBPF monitoring not available on this system")
}