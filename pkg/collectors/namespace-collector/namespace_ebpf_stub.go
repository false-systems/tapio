//go:build !linux
// +build !linux

package namespace_collector

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Stub types for non-Linux platforms

type namespaceMonitorSpecs struct {
	namespaceMonitorProgramSpecs
	namespaceMonitorMapSpecs
}

type namespaceMonitorProgramSpecs struct{}

type namespaceMonitorMapSpecs struct{}

type namespaceMonitorObjects struct {
	namespaceMonitorPrograms
	namespaceMonitorMaps
}

func (o *namespaceMonitorObjects) Close() error {
	return nil
}

type namespaceMonitorMaps struct{}

func (m *namespaceMonitorMaps) Close() error {
	return nil
}

type namespaceMonitorPrograms struct{}

func (p *namespaceMonitorPrograms) Close() error {
	return nil
}

// Stub functions for non-Linux platforms

func loadNamespaceMonitor() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("eBPF not supported on this platform")
}

func loadNamespaceMonitorObjects(obj interface{}, opts *ebpf.CollectionOptions) error {
	return fmt.Errorf("eBPF not supported on this platform")
}

// Collector method stubs for non-Linux platforms

// startEBPF stub for non-Linux platforms
func (c *Collector) startEBPF() error {
	c.logger.Warn("eBPF not supported on this platform")
	return nil
}

// stopEBPF stub for non-Linux platforms
func (c *Collector) stopEBPF() {
	// No-op on non-Linux platforms
}

// readEBPFEvents stub for non-Linux platforms
func (c *Collector) readEBPFEvents() {
	// No-op on non-Linux platforms
	if c.ctx != nil {
		<-c.ctx.Done()
	}
}
