//go:build !linux
// +build !linux

package internal

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
)

// EBPFMonitor is a stub for non-Linux systems
type EBPFMonitor struct {
	*FallbackEBPFMonitor
}

// NewEBPFMonitor creates a fallback monitor on non-Linux systems
func NewEBPFMonitor(config core.Config) (*EBPFMonitor, error) {
	fallback, err := NewFallbackEBPFMonitor(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create fallback monitor: %w", err)
	}
	return &EBPFMonitor{FallbackEBPFMonitor: fallback}, nil
}

// FallbackEBPFMonitor is used when eBPF is not available
type FallbackEBPFMonitor struct {
	*ProcessMonitor
}

// NewFallbackEBPFMonitor creates a fallback monitor when eBPF is not available
func NewFallbackEBPFMonitor(config core.Config) (*FallbackEBPFMonitor, error) {
	processMonitor, err := NewProcessMonitor(config)
	if err != nil {
		return nil, err
	}
	return &FallbackEBPFMonitor{ProcessMonitor: processMonitor}, nil
}

func (m *FallbackEBPFMonitor) MonitorType() string {
	return "ebpf-fallback"
}
