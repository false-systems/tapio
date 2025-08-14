//go:build !linux
// +build !linux

package bpf

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Stub types for non-Linux platforms

type cniMonitorObjects struct{}

func (o *cniMonitorObjects) Close() error {
	return nil
}

type cniMonitorPrograms struct{}

func (p *cniMonitorPrograms) Close() error {
	return nil
}

type cniMonitorMaps struct{}

func (m *cniMonitorMaps) Close() error {
	return nil
}

// Stub functions for non-Linux platforms

func loadCniMonitor() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("eBPF not supported on this platform")
}

func loadCniMonitorObjects(obj interface{}, opts *ebpf.CollectionOptions) error {
	return fmt.Errorf("eBPF not supported on this platform")
}