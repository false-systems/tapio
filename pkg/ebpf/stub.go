//go:build !linux || !ebpf
// +build !linux !ebpf

package ebpf

import (
	"context"
	"runtime"
)

// stubMonitor provides a no-op implementation for non-Linux systems
type stubMonitor struct {
	lastError error
}

// CollectEvents is a no-op for stub monitor
func (s *stubMonitor) CollectEvents() {
	// No-op
}

func (s *stubMonitor) Start(ctx context.Context) error {
	return ErrNotSupported
}

func (s *stubMonitor) Stop() error {
	return nil
}

func (s *stubMonitor) GetMemoryStats() ([]ProcessMemoryStats, error) {
	return nil, ErrNotSupported
}

func (s *stubMonitor) GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error) {
	return nil, ErrNotSupported
}

func (s *stubMonitor) IsAvailable() bool {
	return false
}

func (s *stubMonitor) GetLastError() error {
	if runtime.GOOS != "linux" {
		return ErrNotSupported
	}
	return s.lastError
}
