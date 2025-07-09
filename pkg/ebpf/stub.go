//go:build !linux || !ebpf
// +build !linux !ebpf

package ebpf

import (
	"context"
	"runtime"
)

// StubMonitor is a no-op implementation for platforms without eBPF
type StubMonitor struct {
	lastError error
}

// NewMonitor creates a new eBPF monitor (stub on non-Linux)
func NewMonitor(config *Config) Monitor {
	return &StubMonitor{
		lastError: ErrNotSupported,
	}
}

func (m *StubMonitor) Start(ctx context.Context) error {
	return ErrNotSupported
}

func (m *StubMonitor) Stop() error {
	return nil
}

func (m *StubMonitor) GetMemoryStats() (map[uint32]*ProcessMemoryStats, error) {
	return nil, ErrNotSupported
}

func (m *StubMonitor) GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error) {
	return nil, ErrNotSupported
}

func (m *StubMonitor) IsAvailable() bool {
	return false
}

func (m *StubMonitor) GetLastError() error {
	if runtime.GOOS != "linux" {
		return ErrNotSupported
	}
	return m.lastError
}
