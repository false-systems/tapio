//go:build !linux
// +build !linux

package runtimesignals

import (
	"fmt"

	"go.uber.org/zap"
)

// ebpfState is a stub for non-Linux platforms
type ebpfState struct {
	// Empty on non-Linux platforms
}

// IsLoaded returns false on non-Linux platforms
func (s *ebpfState) IsLoaded() bool {
	return false
}

// LinkCount returns 0 on non-Linux platforms
func (s *ebpfState) LinkCount() int {
	return 0
}

// startEBPF returns an error on non-Linux platforms
func (c *Collector) startEBPF() error {
	c.logger.Warn("Runtime signals collector requires Linux with eBPF support",
		zap.String("collector", c.Name()))

	// Set ebpfState to indicate no eBPF support
	c.ebpfState = &ebpfState{}

	// Don't return error - just run without eBPF
	return nil
}

// stopEBPF is a no-op on non-Linux platforms
func (c *Collector) stopEBPF() {
	c.logger.Debug("No eBPF to stop on non-Linux platform")
}

// readEBPFEvents is not needed on non-Linux platforms
func (c *Collector) readEBPFEvents() {
	// No-op on non-Linux
}

// processEBPFEvent is not needed on non-Linux platforms
func (c *Collector) processEBPFEvent(data []byte) {
	// No-op on non-Linux
}

// Stub types for non-Linux platforms
type runtimeMonitorObjects struct {
	runtimeMonitorPrograms
	runtimeMonitorMaps
}

func (o *runtimeMonitorObjects) Close() error {
	return nil
}

type runtimeMonitorMaps struct {
	Events interface{} // Stub for events map
}

func (m *runtimeMonitorMaps) Close() error {
	return nil
}

type runtimeMonitorPrograms struct {
	TraceProcessExec    interface{}
	TraceProcessExit    interface{}
	TraceSignalGenerate interface{}
	TraceSignalDeliver  interface{}
	TraceOomKill        interface{}
}

func (p *runtimeMonitorPrograms) Close() error {
	return nil
}

func loadRuntimeMonitorObjects(obj interface{}, opts interface{}) error {
	return fmt.Errorf("eBPF not supported on this platform")
}

// GetPlatformSupport returns platform support status
func GetPlatformSupport() string {
	return "Runtime signals collector requires Linux with eBPF support. Running in fallback mode."
}
