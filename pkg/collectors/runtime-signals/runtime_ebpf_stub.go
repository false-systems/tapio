//go:build !linux
// +build !linux

package runtimesignals

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// Stub types for non-Linux platforms

type runtimeMonitorSpecs struct {
	runtimeMonitorProgramSpecs
	runtimeMonitorMapSpecs
}

type runtimeMonitorProgramSpecs struct{}

type runtimeMonitorMapSpecs struct{}

type runtimeMonitorObjects struct {
	runtimeMonitorPrograms
	runtimeMonitorMaps
}

func (o *runtimeMonitorObjects) Close() error {
	return nil
}

type runtimeMonitorMaps struct {
	Events *ebpf.Map
}

func (m *runtimeMonitorMaps) Close() error {
	return nil
}

type runtimeMonitorPrograms struct {
	TraceProcessExec    *ebpf.Program
	TraceProcessExit    *ebpf.Program
	TraceSignalGenerate *ebpf.Program
	TraceSignalDeliver  *ebpf.Program
	TraceOomKill        *ebpf.Program
}

func (p *runtimeMonitorPrograms) Close() error {
	return nil
}

// Stub functions for non-Linux platforms

func loadRuntimeMonitor() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("eBPF not supported on this platform")
}

func loadRuntimeMonitorObjects(obj interface{}, opts *ebpf.CollectionOptions) error {
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
