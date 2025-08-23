//go:build !linux
// +build !linux

package bpf

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Stub types for non-Linux platforms

type namespaceMonitorObjects struct{}

func (o *namespaceMonitorObjects) Close() error {
	return nil
}

type namespaceMonitorPrograms struct{}

func (p *namespaceMonitorPrograms) Close() error {
	return nil
}

type namespaceMonitorMaps struct{}

func (m *namespaceMonitorMaps) Close() error {
	return nil
}

// Stub functions for non-Linux platforms

func loadNamespaceMonitor() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("eBPF not supported on this platform")
}

func loadNamespaceMonitorObjects(obj interface{}, opts *ebpf.CollectionOptions) error {
	return fmt.Errorf("eBPF not supported on this platform")
}
