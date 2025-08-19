//go:build !linux
// +build !linux

package bpf

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Stub types for non-Linux platforms

type systemdMonitorObjects struct {
	systemdMonitorPrograms
	systemdMonitorMaps
}

func (o *systemdMonitorObjects) Close() error {
	return nil
}

type systemdMonitorPrograms struct {
	TraceExec *ebpf.Program
	TraceExit *ebpf.Program
}

func (p *systemdMonitorPrograms) Close() error {
	return nil
}

type systemdMonitorMaps struct {
	Events      *ebpf.Map
	SystemdPids *ebpf.Map
}

func (m *systemdMonitorMaps) Close() error {
	return nil
}

// Stub functions for non-Linux platforms

func loadSystemdMonitor() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("eBPF not supported on this platform")
}

func loadSystemdMonitorObjects(obj interface{}, opts *ebpf.CollectionOptions) error {
	return fmt.Errorf("eBPF not supported on this platform")
}
